package evaluator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crossplane-contrib/function-hcl/internal/evaluator/hclutils"
	"github.com/crossplane-contrib/function-hcl/internal/evaluator/locals"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

func (e *Evaluator) processLocals(ctx *hcl.EvalContext, content *hcl.BodyContent) (*hcl.EvalContext, hcl.Diagnostics) {
	return locals.NewProcessor().Process(ctx, content)
}

// processGroup processes all blocks at the top-level or at the level of a single group.
func (e *Evaluator) processGroup(ctx *hcl.EvalContext, content *hcl.BodyContent) hcl.Diagnostics {
	ctx, diags := e.processLocals(ctx, content)
	if diags.HasErrors() {
		return diags
	}

	cond, ds := e.evaluateCondition(ctx, content, discardTypeGroup, "")
	diags = diags.Extend(ds)
	if ds.HasErrors() {
		return diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "unable to evaluate condition",
		})
	}
	if !cond {
		return nil
	}
	for _, b := range content.Blocks {
		var curDiags hcl.Diagnostics
		switch b.Type {
		case blockGroup:
			content, ds := b.Body.Content(groupSchema())
			if ds.HasErrors() {
				return diags.Extend(ds)
			}
			curDiags = ds.Extend(e.processGroup(ctx, content))
		case blockResource:
			curDiags = e.processResource(ctx, b)
		case blockResources:
			curDiags = e.processResources(ctx, b)
		case blockContext:
			curDiags = e.processContext(ctx, b)
		case blockComposite:
			curDiags = e.processComposite(ctx, b)
		case blockRequirement:
			curDiags = e.processRequirement(ctx, b)
		case blockLocals:
			// already processed
		case blockFunction:
			// ditto
		default:
			curDiags = curDiags.Append(&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  fmt.Sprintf("unsupported block type %s", b.Type),
				Subject:  ptr(b.DefRange),
			})
		}
		diags = diags.Extend(curDiags)
		if curDiags.HasErrors() {
			return diags
		}
	}
	return diags
}

func (e *Evaluator) processResource(ctx *hcl.EvalContext, block *hcl.Block) hcl.Diagnostics {
	resourceName := block.Labels[0]

	content, diags := block.Body.Content(resourceSchema())
	if diags.HasErrors() {
		return diags
	}

	// add the resource to our stash
	ds := e.addResource(ctx, resourceName, content, nil)
	return diags.Extend(ds)
}

func (e *Evaluator) processResources(ctx *hcl.EvalContext, block *hcl.Block) hcl.Diagnostics {
	baseName := block.Labels[0]

	// parse with strict schema
	content, diags := block.Body.Content(resourcesSchema())
	if diags.HasErrors() {
		return diags
	}

	var templateBlock *hcl.Block
	for _, b := range content.Blocks {
		if b.Type == blockTemplate {
			if templateBlock != nil {
				return diags.Append(&hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  fmt.Sprintf("multiple template blocks for resource collection %s", baseName),
					Subject:  ptr(b.DefRange),
				})
			}
			templateBlock = b
		}
	}
	if templateBlock == nil {
		return diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  fmt.Sprintf("no template block for resource collection %s", baseName),
			Subject:  ptr(block.DefRange),
		})
	}

	templateContent, ds := templateBlock.Body.Content(templateSchema())
	diags = diags.Extend(ds)
	if ds.HasErrors() {
		return diags
	}

	var err error
	// create a context for the resources block to include the self.basename set to base name
	ctx = createSelfChildContext(ctx, DynamicObject{
		selfBaseName:            cty.StringVal(baseName),
		selfObservedResources:   e.getObservedCollectionResources(baseName),
		selfObservedConnections: e.getObservedCollectionConnections(baseName),
	})

	// add a locals child context
	ctx, ds = e.processLocals(ctx, content)
	diags = diags.Extend(ds)
	if ds.HasErrors() {
		return diags
	}

	cond, ds := e.evaluateCondition(ctx, content, discardTypeResourceList, baseName)
	diags = diags.Extend(ds)
	if ds.HasErrors() {
		return diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  fmt.Sprintf("unable to evaluate condition for resource collection %s", baseName),
			Subject:  ptr(block.DefRange),
		})
	}
	if !cond {
		return diags
	}

	// get the iterations from the for_each expression
	forEachExpr := content.Attributes[attrForEach].Expr
	forEachVal, ds := forEachExpr.Value(ctx)
	diags = diags.Extend(ds)
	if ds.HasErrors() {
		return diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  fmt.Sprintf("unable to evaluate for_each for resource collection %s", baseName),
			Subject:  ptr(forEachExpr.Range()),
		})
	}

	iters, err := extractIterations(forEachVal)
	if err != nil {
		return diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  fmt.Sprintf("unable to extract iterations for resource collection %s", baseName),
			Subject:  ptr(forEachExpr.Range()),
		})
	}

	// get the name as an expression.
	var nameExpr hcl.Expression
	if npAttr, ok := content.Attributes[attrName]; ok {
		nameExpr = npAttr.Expr
	} else {
		nameExpr, ds = hclsyntax.ParseTemplate([]byte(`${self.basename}-${each.key}`), "default-name.hcl", hcl.Pos{Line: 1, Column: 1})
		diags = diags.Extend(ds)
		if ds.HasErrors() {
			return diags.Append(&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  fmt.Sprintf("unable to evaluate default name expression for resource collection %s", baseName),
				Subject:  ptr(nameExpr.Range()),
			})
		}
	}

	// actually process resources
	for i, iter := range iters {
		iterContext := ctx.NewChild()
		iterContext.Variables = DynamicObject{
			iteratorName: cty.ObjectVal(DynamicObject{
				attrKey:   iter.key,
				attrValue: iter.value,
			}),
		}

		resourceExpr, ds := nameExpr.Value(iterContext)
		diags = diags.Extend(ds)
		if ds.HasErrors() {
			return diags.Append(&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  fmt.Sprintf("unable to evaluate name expression for resource collection %s", baseName),
				Subject:  ptr(nameExpr.Range()),
			})
		}
		if resourceExpr.Type() != cty.String {
			return diags.Append(&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  fmt.Sprintf("name produced from evaluating the name expression for collection %s was not a string", baseName),
				Subject:  ptr(nameExpr.Range()),
			})
		}
		name := resourceExpr.AsString()
		annotations := map[string]string{
			annotationBaseName: baseName,
			annotationIndex:    fmt.Sprintf("s%06d", i),
		}
		ds = e.addResource(iterContext, name, templateContent, annotations)
		diags = diags.Extend(ds)
		if ds.HasErrors() {
			return diags
		}
	}

	// process any composite and context blocks
	for _, b := range content.Blocks {
		var currentDiags hcl.Diagnostics
		if b.Type == blockComposite {
			currentDiags = e.processComposite(ctx, b)
		}
		if b.Type == blockContext {
			currentDiags = e.processContext(ctx, b)
		}
		diags = diags.Extend(currentDiags)
		if currentDiags.HasErrors() {
			return diags
		}
	}
	return diags
}

func (e *Evaluator) addResource(ctx *hcl.EvalContext, resourceName string, content *hcl.BodyContent, annotations map[string]string) hcl.Diagnostics {
	// dup check
	if e.desiredResources[resourceName] != nil {
		return hcl.Diagnostics{&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  fmt.Sprintf("duplicate resource %q", resourceName),
		}}
	}
	// create resource-specific context with magic variables
	ctx = createSelfChildContext(ctx, DynamicObject{
		selfName:               cty.StringVal(resourceName),
		selfObservedResource:   e.getObservedResource(resourceName),
		selfObservedConnection: e.getObservedConnection(resourceName),
	})

	ctx, diags := e.processLocals(ctx, content)
	if diags.HasErrors() {
		return diags
	}

	body, ok := content.Attributes[attrBody]
	if !ok {
		return hcl.Diagnostics{&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  fmt.Sprintf("internal error: no body in content block for %q", resourceName),
		}}
	}

	cond, ds := e.evaluateCondition(ctx, content, discardTypeResource, resourceName)
	diags = diags.Extend(ds)
	if ds.HasErrors() {
		return diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  fmt.Sprintf("unable to evaluate condition for resource %s", resourceName),
		})
	}
	if !cond {
		return nil
	}

	// process the body
	out, ds := body.Expr.Value(ctx)

	// if we have errors in processing or couldn't fully eval the body, make it a hard error if there is already an observed
	// resource with this name. This implies that the user has made a bad change to one of the
	// expressions in the body, and we should halt instead of silently removing the resource
	// from the desired output, thereby having crossplane delete it.
	if ds.HasErrors() || !out.IsWhollyKnown() {
		context := e.messagesFromDiags(ds)

		var incompleteVars []string
		for _, t := range body.Expr.Variables() {
			v, tdiag := t.TraverseAbs(ctx)
			ds = append(ds, tdiag...)

			sourceName := e.sourceCode(t.SourceRange())

			// try to find the path to the actual unknown values to assist with debugging
			unknownPaths, err := findUnknownPaths(v)
			if err != nil {
				// unexpected error while finding unknown paths, add to context instead of failing
				ds = append(ds, &hcl.Diagnostic{
					Severity: hcl.DiagWarning,
					Subject:  ptr(t.SourceRange()),
					Summary:  fmt.Sprintf("unexpected error while finding unknown paths for %s: %s", resourceName, err),
				})
			}
			for _, path := range unknownPaths {
				incompleteVars = append(incompleteVars, sourceName+path)
			}

			// if we didn't find any unknown paths, add the source name only
			if len(unknownPaths) == 0 && !v.IsWhollyKnown() {
				incompleteVars = append(incompleteVars, sourceName)
			}
		}
		unknown := strings.Join(incompleteVars, ", ")
		if _, have := e.existingResourceMap[resourceName]; have {
			return diags.Extend(ds).Append(&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Subject:  ptr(body.Expr.Range()),
				Summary:  fmt.Sprintf("existing resource %s could not be evaluated, abort (unknown values: %s)", resourceName, unknown),
			})
		}

		e.discard(DiscardItem{
			Type:        discardTypeResource,
			Reason:      discardReasonIncomplete,
			Name:        resourceName,
			SourceRange: body.Expr.Range().String(),
			Context:     append(context, fmt.Sprintf("unknown values: %s", unknown)),
		})
		// map unknown resource value errors to warnings as we'll handle them later
		return diags.Extend(hclutils.DowngradeDiags(ds))
	}
	diags = diags.Extend(ds)

	// convert body to a protobuf struct and add to desired state
	bodyStruct, err := valueToStructWithAnnotations(out, annotations)
	if err != nil {
		return diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  fmt.Sprintf("unable to convert resource body to struct: %s", resourceName),
			Subject:  ptr(body.Expr.Range()),
		})
	}
	e.desiredResources[resourceName] = bodyStruct

	for _, b := range content.Blocks {
		var currentDiags hcl.Diagnostics
		if b.Type == blockComposite {
			currentDiags = e.processComposite(ctx, b)
		}
		if b.Type == blockReady {
			currentDiags = e.processReady(ctx, resourceName, b)
		}
		if b.Type == blockContext {
			currentDiags = e.processContext(ctx, b)
		}
		diags = diags.Extend(currentDiags)
		if currentDiags.HasErrors() {
			return diags
		}
	}

	return diags
}

var validReadyValues string

func init() {
	var keys []string
	for k := range fnv1.Ready_value {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	validReadyValues = strings.Join(keys, ", ")
}

func (e *Evaluator) processReady(ctx *hcl.EvalContext, resourceName string, block *hcl.Block) hcl.Diagnostics {
	content, diags := block.Body.Content(readySchema())
	if diags.HasErrors() {
		return diags
	}
	ctx, ds := e.processLocals(ctx, content)
	diags = diags.Extend(ds)
	if ds.HasErrors() {
		return diags
	}
	attr, ok := content.Attributes[attrValue]
	if !ok {
		return diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  fmt.Sprintf("attribute %q not found in ready block for %s", attrValue, resourceName),
			Subject:  ptr(block.DefRange),
		})
	}

	value, ds := attr.Expr.Value(ctx)
	if ds.HasErrors() || !value.IsWhollyKnown() {
		e.discard(DiscardItem{
			Type:        discardTypeReady,
			Reason:      discardReasonIncomplete,
			Name:        resourceName,
			SourceRange: attr.Expr.Range().String(),
			Context:     e.messagesFromDiags(diags),
		})
		// map unknown ready value errors to warnings as we'll handle them later
		return diags.Extend(hclutils.DowngradeDiags(ds))
	}
	diags = diags.Extend(ds)
	if value.Type() != cty.String {
		return diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  fmt.Sprintf("attribute %q not a string in ready block for %s", attrValue, resourceName),
			Subject:  ptr(attr.Expr.Range()),
		})
	}
	s := value.AsString()
	v, ok := fnv1.Ready_value[s]
	if !ok {
		return diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  fmt.Sprintf("attribute %q does not have a valid value in ready block for %s, must be one of %q", attrValue, resourceName, validReadyValues),
			Subject:  ptr(attr.Expr.Range()),
		})
	}
	e.ready[resourceName] = v
	return diags
}
