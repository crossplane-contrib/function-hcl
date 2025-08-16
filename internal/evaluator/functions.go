package evaluator

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

// processFunctions processes all function blocks at the top-level and returns an evaluation
// context that includes all supported functions with an `invoke` function in addition.
func (e *Evaluator) processFunctions(content *hcl.BodyContent) (*hcl.EvalContext, hcl.Diagnostics) {
	var curDiags, emptyDiags hcl.Diagnostics
	funcs := map[string]*UserFunction{}
	for _, b := range content.Blocks {
		if b.Type != blockFunction {
			continue
		}
		fn, diags := e.processFunction(b)
		if diags.HasErrors() {
			return nil, diags
		}
		curDiags = curDiags.Extend(diags)
		if _, ok := funcs[fn.Name]; ok {
			return nil, emptyDiags.Append(err2DiagWithRange(fmt.Errorf("function %s: duplicate definition", fn.Name), b.DefRange))
		}
		funcs[fn.Name] = fn
	}
	return newInvoker(funcs, e).rootContext(DynamicObject{}), nil
}

// processFunction processes a single function block and returns an equivalent UserFunction.
func (e *Evaluator) processFunction(block *hcl.Block) (*UserFunction, hcl.Diagnostics) {
	var curDiags, emptyDiags hcl.Diagnostics
	content, diags := block.Body.Content(functionSchema())
	if diags.HasErrors() {
		return nil, diags
	}
	curDiags = curDiags.Extend(diags)
	fnName := block.Labels[0]

	desc := ""
	descAttr := content.Attributes[attrDescription]
	if descAttr != nil {
		v, d := descAttr.Expr.Value(&hcl.EvalContext{})
		curDiags = curDiags.Extend(d)
		if !(v.IsWhollyKnown() && v.Type() == cty.String) {
			return nil, emptyDiags.Append(err2DiagWithRange(fmt.Errorf("function %q: description is not a constant string", fnName), descAttr.Range))
		}
		desc = v.AsString()
	}

	args := map[string]*Arg{}
	for _, b := range content.Blocks {
		if b.Type == blockArg {
			arg, diags := e.processArg(fnName, b)
			if diags.HasErrors() {
				return nil, diags
			}
			if _, ok := args[arg.Name]; ok {
				return nil, emptyDiags.Append(err2DiagWithRange(fmt.Errorf("function %s: duplicate definition of argument %s", fnName, arg.Name), b.DefRange))
			}
			args[arg.Name] = arg
		}
	}
	vals := map[string]cty.Value{}
	for _, a := range args {
		vals[a.Name] = a.Default // doesn't matter if there is no default
	}
	ctx := newInvoker(nil, e).rootContext(vals)
	lp := newLocalsProcessor(e)
	_, diags = lp.process(ctx, content)
	if diags.HasErrors() {
		return nil, diags
	}
	curDiags = curDiags.Extend(diags)
	bodyAttr := content.Attributes[attrBody]
	return &UserFunction{
		Name:         fnName,
		Description:  desc,
		Args:         args,
		body:         bodyAttr.Expr,
		blockContent: content,
	}, curDiags
}

// processArg processes a single arg block and returns an Arg.
func (e *Evaluator) processArg(fn string, block *hcl.Block) (*Arg, hcl.Diagnostics) {
	var curDiags, emptyDiags hcl.Diagnostics
	a, diags := block.Body.Content(argSchema())
	if diags.HasErrors() {
		return nil, diags
	}
	argName := block.Labels[0]
	curDiags = curDiags.Extend(diags)

	desc := ""
	descAttr := a.Attributes[attrDescription]
	if descAttr != nil {
		v, d := descAttr.Expr.Value(&hcl.EvalContext{})
		curDiags = curDiags.Extend(d)
		if !(v.IsWhollyKnown() && v.Type() == cty.String) {
			return nil, emptyDiags.Append(err2DiagWithRange(fmt.Errorf("function %q argument %q: description is not a constant string", fn, argName), descAttr.Range))
		}
		desc = v.AsString()
	}

	defAttr := a.Attributes[attrDefault]
	var v cty.Value
	if defAttr != nil {
		v, diags = defAttr.Expr.Value(&hcl.EvalContext{})
		curDiags = curDiags.Extend(diags)
		if !v.IsWhollyKnown() {
			return nil, emptyDiags.Append(err2DiagWithRange(fmt.Errorf("function %q argument %q: default is not a constant", fn, argName), defAttr.Range))
		}
	}
	return &Arg{
		Name:        argName,
		Description: desc,
		HasDefault:  defAttr != nil,
		Default:     v,
	}, curDiags
}
