package evaluator

import (
	"fmt"

	"github.com/crossplane-contrib/function-hcl/internal/evaluator/functions"
	"github.com/crossplane-contrib/function-hcl/internal/evaluator/hclutils"
	"github.com/crossplane-contrib/function-hcl/internal/evaluator/locals"
	"github.com/hashicorp/hcl/v2/hclsyntax"

	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
	"google.golang.org/protobuf/types/known/structpb"
)

// analyzer provides facilities for HCL analysis.
type analyzer struct {
	e               *Evaluator
	p               *functions.Processor
	resourceNames   map[string]bool
	collectionNames map[string]bool
}

func newAnalyzer(e *Evaluator) *analyzer {
	return &analyzer{
		e:               e,
		resourceNames:   map[string]bool{},
		collectionNames: map[string]bool{},
	}
}

func (a *analyzer) addResource(name string, r hcl.Range) hcl.Diagnostics {
	if a.resourceNames[name] {
		return hclutils.ToErrorDiag("resource defined more than once", name, r)
	}
	a.resourceNames[name] = true
	return nil
}

func (a *analyzer) addCollection(name string, r hcl.Range) hcl.Diagnostics {
	if a.collectionNames[name] {
		return hclutils.ToErrorDiag("resource collection defined more than once", name, r)
	}
	a.collectionNames[name] = true
	return nil
}

func (a *analyzer) checkReferences(ctx *hcl.EvalContext, tables map[string]DynamicObject, expr hcl.Traversal) hcl.Diagnostics {
	var ret hcl.Diagnostics
	sr := expr.SourceRange()
	expr = hclutils.NormalizeTraversal(expr)
	getText := func() string {
		return a.e.sourceCode(sr)
	}
	switch expr.RootName() {
	case reservedReq, reservedSelf:
		if len(expr) < 2 {
			return nil
		}
		root := tables[expr.RootName()]
		second, ok := expr[1].(hcl.TraverseAttr)
		if !ok {
			ret = ret.Extend(hclutils.ToErrorDiag("invalid index expression", getText(), sr))
			break
		}
		if _, ok := root[second.Name]; !ok {
			ret = ret.Extend(hclutils.ToErrorDiag(fmt.Sprintf("no such attribute %q", second.Name), getText(), sr))
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
				ret = ret.Extend(hclutils.ToErrorDiag("invalid resource name reference", thirdStep, sr))
			}
		case expr.RootName() == reservedReq && second.Name == "resources":
			if !a.collectionNames[thirdStep] {
				ret = ret.Extend(hclutils.ToErrorDiag("invalid resource collection name reference", thirdStep, sr))
			}
		case expr.RootName() == reservedSelf && second.Name == "each":
			if thirdStep != "key" && thirdStep != "value" {
				ret = ret.Extend(hclutils.ToErrorDiag("invalid each reference, must be one of 'key' or 'value'", thirdStep, sr))
			}
		}

	case iteratorName:
		if len(expr) < 2 {
			return nil
		}
		second, ok := expr[1].(hcl.TraverseAttr)
		if !ok {
			ret = ret.Extend(hclutils.ToErrorDiag("invalid index expression", getText(), sr))
			break
		}
		if second.Name != "key" && second.Name != "value" {
			ret = ret.Extend(hclutils.ToErrorDiag("invalid each reference, must be one of 'key' or 'value'", second.Name, sr))
			break
		}
		fallthrough // since each is a local variable added on demand, add the local variable ref checks as well

	default: // local variable reference
		reference := expr.RootName()
		if !hasVariable(ctx, reference) {
			r := expr[0].SourceRange()
			ret = ret.Extend(hclutils.ToErrorDiag("invalid local variable reference", reference, r))
		}
	}
	return ret
}

func (a *analyzer) processLocals(ctx *hcl.EvalContext, content *hcl.BodyContent) (*hcl.EvalContext, map[string]hcl.Expression, hcl.Diagnostics) {
	lp := locals.NewProcessor()
	childCtx, diags := lp.Process(ctx, content)
	if diags.HasErrors() {
		return nil, nil, diags
	}
	exprs, diags := lp.Expressions(content)
	if diags.HasErrors() {
		return nil, nil, diags
	}
	return childCtx, exprs, diags
}

// analyzeContent analyzes the content in the supplied block after setting up an eval context for it.
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

	checkFunctionRefs := func(x hcl.Expression) {
		n, ok := x.(hclsyntax.Node)
		if ok {
			ret = ret.Extend(a.p.CheckUserFunctionRefs(n))
		}
	}

	// first locals
	for _, expr := range localExpressions {
		vars := expr.Variables()
		for _, v := range vars {
			ret = ret.Extend(a.checkReferences(ctx, tables, v))
		}
		checkFunctionRefs(expr)
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
		checkFunctionRefs(attr.Expr)
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
		// function blocks have already been statically analyzed at load for bad references.
		if block.Type == blockLocals || block.Type == blockFunction {
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

func (a *analyzer) analyze(files ...File) hcl.Diagnostics {
	// parse all files
	bodies, diags := a.e.toBodies(files)
	if diags.HasErrors() {
		return diags
	}

	for _, body := range bodies {
		diags = diags.Extend(a.checkStructure(body, topLevelSchema()))
	}
	if diags.HasErrors() {
		return diags
	}

	content, ds := a.e.makeContent(bodies)
	diags = diags.Extend(ds)
	if diags.HasErrors() {
		return diags
	}

	p := functions.NewProcessor()
	ds = p.Process(content)
	diags = diags.Extend(ds)
	if diags.HasErrors() {
		return diags
	}

	a.p = p
	ctx := p.RootContext(nil)

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

	ctx, err := a.e.makeVars(ctx, req)
	if err != nil {
		return []*hcl.Diagnostic{{Severity: hcl.DiagError, Summary: "internal error: setup dummy vars", Detail: err.Error()}}
	}

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
	return a.analyze(files...)
}
