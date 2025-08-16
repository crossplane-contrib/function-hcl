package evaluator

import (
	"fmt"

	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
	"google.golang.org/protobuf/types/known/structpb"
)

// analyzer provides facilities for HCL analysis.
type analyzer struct {
	finder          sourceFinder
	resourceNames   map[string]bool
	collectionNames map[string]bool
}

func newAnalyzer(finder sourceFinder) *analyzer {
	return &analyzer{
		finder:          finder,
		resourceNames:   make(map[string]bool),
		collectionNames: make(map[string]bool),
	}
}

func (a *analyzer) addResource(name string, r hcl.Range) hcl.Diagnostics {
	if a.resourceNames[name] {
		return toErrorDiag("resource defined more than once", name, r)
	}
	a.resourceNames[name] = true
	return nil
}

func (a *analyzer) addCollection(name string, r hcl.Range) hcl.Diagnostics {
	if a.collectionNames[name] {
		return toErrorDiag("resource collection defined more than once", name, r)
	}
	a.collectionNames[name] = true
	return nil
}

func (a *analyzer) checkReferences(ctx *hcl.EvalContext, tables map[string]DynamicObject, expr hcl.Traversal) hcl.Diagnostics {
	var ret hcl.Diagnostics
	sr := expr.SourceRange()
	expr = normalizeTraversal(expr)
	getText := func() string {
		return a.finder.sourceCode(sr)
	}
	switch expr.RootName() {
	case reservedReq, reservedSelf:
		if len(expr) < 2 {
			return nil
		}
		root := tables[expr.RootName()]
		second, ok := expr[1].(hcl.TraverseAttr)
		if !ok {
			ret = ret.Extend(toErrorDiag("invalid index expression", getText(), sr))
			break
		}
		if _, ok := root[second.Name]; !ok {
			ret = ret.Extend(toErrorDiag(fmt.Sprintf("no such attribute %q", second.Name), getText(), sr))
			break
		}

		// get the third step in the traversal if one exists
		thirdStep := ""
		if len(expr) > 2 {
			third, ok := expr[2].(hcl.TraverseAttr)
			if ok {
				thirdStep = third.Name
			}
		}
		if thirdStep == "" {
			break
		}

		switch {
		case expr.RootName() == reservedReq && second.Name == "resource":
			if !a.resourceNames[thirdStep] {
				ret = ret.Extend(toErrorDiag("invalid resource name reference", thirdStep, sr))
			}
		case expr.RootName() == reservedReq && second.Name == "resources":
			if !a.collectionNames[thirdStep] {
				ret = ret.Extend(toErrorDiag("invalid resource collection name reference", thirdStep, sr))
			}
		case expr.RootName() == reservedSelf && second.Name == "each":
			if thirdStep != "key" && thirdStep != "value" {
				ret = ret.Extend(toErrorDiag("invalid each reference, must be one of 'key' or 'value'", thirdStep, sr))
			}
		}

	case iteratorName:
		if len(expr) < 2 {
			return nil
		}
		second, ok := expr[1].(hcl.TraverseAttr)
		if !ok {
			ret = ret.Extend(toErrorDiag("invalid index expression", getText(), sr))
			break
		}
		if second.Name != "key" && second.Name != "value" {
			ret = ret.Extend(toErrorDiag("invalid each reference, must be one of 'key' or 'value'", second.Name, sr))
			break
		}
		fallthrough // since each is a local variable added on demand, add the local variable ref checks as well

	default: // local variable reference
		reference := expr.RootName()
		if !hasVariable(ctx, reference) {
			r := expr[0].SourceRange()
			ret = ret.Extend(toErrorDiag("invalid local variable reference", reference, r))
		}
	}
	return ret
}

func (a *analyzer) processLocals(ctx *hcl.EvalContext, content *hcl.BodyContent) (*hcl.EvalContext, map[string]hcl.Expression, hcl.Diagnostics) {
	lp := newLocalsProcessor(a.finder)
	childCtx, diags := lp.process(ctx, content)
	if diags.HasErrors() {
		return nil, nil, diags
	}
	exprs, diags := lp.getLocalExpressions(content)
	if diags.HasErrors() {
		return nil, nil, diags
	}
	return childCtx, exprs, diags
}

func (a *analyzer) analyzeContent(ctx *hcl.EvalContext, parent *hcl.Block, content *hcl.BodyContent) hcl.Diagnostics {
	// if in a resources block add the expected self vars
	if parent.Type == blockResources {
		ctx = createSelfChildContext(ctx, DynamicObject{
			selfBaseName:            cty.StringVal("dummy"),
			selfObservedResources:   cty.DynamicVal,
			selfObservedConnections: cty.DynamicVal,
		})
	}

	if parent.Type == blockResource || parent.Type == blockTemplate {
		ctx = createSelfChildContext(ctx, map[string]cty.Value{
			selfName:               cty.StringVal("dummy"),
			selfObservedResource:   cty.DynamicVal,
			selfObservedConnection: cty.DynamicVal,
		})
	}

	// evaluate locals, checking for bad refs
	ctx, localExpressions, diags := a.processLocals(ctx, content)
	if diags.HasErrors() {
		return diags
	}

	// now ensure that all expressions including ones in local and attributes refer to
	// locals, resources, and collections that exist.
	tables := makeTables(ctx)

	var ret hcl.Diagnostics

	// first locals
	for _, expr := range localExpressions {
		vars := expr.Variables()
		for _, v := range vars {
			ret = ret.Extend(a.checkReferences(ctx, tables, v))
		}
	}

	// then attributes
	for _, attr := range content.Attributes {
		// unlike any other attribute, the name attribute for the `resources` block is special because
		// it has access to the iterator.
		if attr.Name == "name" && parent.Type == blockResources {
			continue
		}
		vars := attr.Expr.Variables()
		for _, v := range vars {
			ret = ret.Extend(a.checkReferences(ctx, tables, v))
		}
	}

	// if it is a resources block add the iterator context at this point
	if parent.Type == blockResources {
		ctx = ctx.NewChild()
		ctx.Variables = DynamicObject{
			iteratorName: cty.ObjectVal(DynamicObject{
				attrKey:   cty.DynamicVal,
				attrValue: cty.DynamicVal,
			}),
		}
		// check the name attribute if one exists
		if nameAttr, ok := content.Attributes[attrName]; ok {
			vars := nameAttr.Expr.Variables()
			for _, v := range vars {
				ret = ret.Extend(a.checkReferences(ctx, tables, v))
			}
		}
	}

	// process child blocks
	for _, block := range content.Blocks {
		if block.Type == blockLocals {
			continue
		}
		childContent, d := block.Body.Content(schemasByBlockType[block.Type])
		if d.HasErrors() { // should never happen if structure has already been checked
			return d
		}
		ret = ret.Extend(a.analyzeContent(ctx, block, childContent))
	}
	return ret
}

func (a *analyzer) analyze(ctx *hcl.EvalContext, content *hcl.BodyContent) hcl.Diagnostics {
	return a.analyzeContent(ctx, &hcl.Block{}, content)
}

func (a *analyzer) checkStructure(body hcl.Body, s *hcl.BodySchema) hcl.Diagnostics {
	if s == nil {
		_, diags := body.JustAttributes()
		if diags.HasErrors() {
			return diags
		}
		return nil
	}
	content, diags := body.Content(s)
	if diags.HasErrors() {
		return diags
	}
	for _, block := range content.Blocks {
		switch block.Type {
		case blockResource:
			diags = diags.Extend(a.addResource(block.Labels[0], block.LabelRanges[0]))
		case blockResources:
			diags = diags.Extend(a.addCollection(block.Labels[0], block.LabelRanges[0]))
		}
		diags = diags.Extend(a.checkStructure(block.Body, schemasByBlockType[block.Type]))
	}
	return diags
}

func (e *Evaluator) doAnalyze(files ...File) (finalErr hcl.Diagnostics) {
	// note: when returning something using diags from this function, we sort by severity first
	// this is in order to have at least one error show up in formatted errors.
	defer func() {
		finalErr = sortDiagsBySeverity(finalErr)
	}()

	a := newAnalyzer(e)

	// parse all files
	bodies, diags := e.toBodies(files)
	if diags.HasErrors() {
		return diags
	}

	for _, body := range bodies {
		diags = diags.Extend(a.checkStructure(body, topLevelSchema()))
	}
	if diags.HasErrors() {
		return diags
	}

	content, ds := e.makeContent(bodies)
	diags = diags.Extend(ds)
	if diags.HasErrors() {
		return diags
	}

	ctx, ds := e.processFunctions(content)
	diags = diags.Extend(ds)
	if diags.HasErrors() {
		return diags
	}

	req := &fnv1.RunFunctionRequest{
		Observed: &fnv1.State{
			Composite: &fnv1.Resource{
				Resource:          &structpb.Struct{},
				ConnectionDetails: map[string][]byte{},
			},
			Resources: map[string]*fnv1.Resource{},
		},
		Context:        &structpb.Struct{},
		ExtraResources: map[string]*fnv1.Resources{},
		Credentials:    map[string]*fnv1.Credentials{},
	}

	ctx, err := e.makeVars(ctx, req)
	if err != nil {
		return []*hcl.Diagnostic{{Severity: hcl.DiagError, Summary: "internal error: setup dummy vars", Detail: err.Error()}}
	}

	return a.analyze(ctx, content)
}
