package functions

import (
	"fmt"

	"github.com/crossplane-contrib/function-hcl/internal/evaluator/hclutils"
	"github.com/crossplane-contrib/function-hcl/internal/evaluator/locals"
	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

const (
	BlockFunction   = "function"
	BlockArg        = "arg"
	attrDescription = "description"
	attrDefault     = "default"
	attrBody        = "body"
	blockLocals     = locals.BlockLocals
)

// processFunctions processes all function blocks at the top-level and returns error
// diagnostics in case of function definition issues.
func (e *Processor) processFunctions(content *hcl.BodyContent) hcl.Diagnostics {
	var curDiags, emptyDiags hcl.Diagnostics
	funcs := map[string]*UserFunction{}
	for _, b := range content.Blocks {
		if b.Type != BlockFunction {
			continue
		}
		fn, diags := e.processFunction(b)
		curDiags = curDiags.Extend(diags)
		if diags.HasErrors() {
			return diags
		}
		if _, ok := funcs[fn.Name]; ok {
			return emptyDiags.Extend(hclutils.ToErrorDiag("duplicate function declaration", fn.Name, b.DefRange))
		}
		funcs[fn.Name] = fn
	}
	e.Functions = funcs
	e.invoker = newInvoker(funcs)
	for _, f := range funcs {
		curDiags = curDiags.Extend(f.checkRefs(e.invoker))
	}
	return curDiags
}

// processFunction processes a single function block and returns an equivalent UserFunction.
func (e *Processor) processFunction(block *hcl.Block) (*UserFunction, hcl.Diagnostics) {
	var curDiags, emptyDiags hcl.Diagnostics
	content, diags := block.Body.Content(FunctionSchema())
	if diags.HasErrors() {
		return nil, diags
	}
	curDiags = curDiags.Extend(diags)
	fnName := block.Labels[0]

	if !hclutils.IsIdentifier(fnName) {
		return nil, emptyDiags.Extend(hclutils.ToErrorDiag(fmt.Sprintf("function %q : name must be an identifier", fnName), "", block.LabelRanges[0]))
	}

	desc := ""
	descAttr := content.Attributes[attrDescription]
	if descAttr != nil {
		v, d := descAttr.Expr.Value(&hcl.EvalContext{})
		curDiags = curDiags.Extend(d)
		if !(v.IsWhollyKnown() && v.Type() == cty.String) {
			return nil, emptyDiags.Extend(hclutils.ToErrorDiag(fmt.Sprintf("function %s : description is not a constant string", fnName), "", descAttr.Range))
		}
		desc = v.AsString()
	}

	args := map[string]*Arg{}
	for _, b := range content.Blocks {
		if b.Type == BlockArg {
			arg, diags := e.processArg(fnName, b)
			if diags.HasErrors() {
				return nil, diags
			}
			if _, ok := args[arg.Name]; ok {
				return nil, emptyDiags.Extend(hclutils.ToErrorDiag(fmt.Sprintf("function %s: duplicate definition of argument", fnName), arg.Name, b.DefRange))
			}
			args[arg.Name] = arg
		}
	}
	vals := map[string]cty.Value{}
	for _, a := range args {
		vals[a.Name] = a.Default // doesn't matter if there is no default
	}
	ctx := newInvoker(nil).rootContext(vals)
	lp := locals.NewProcessor()
	_, diags = lp.Process(ctx, content)
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
func (e *Processor) processArg(fn string, block *hcl.Block) (*Arg, hcl.Diagnostics) {
	var curDiags, emptyDiags hcl.Diagnostics
	a, diags := block.Body.Content(ArgSchema())
	curDiags = curDiags.Extend(diags)
	if diags.HasErrors() {
		return nil, diags
	}

	argName := block.Labels[0]
	if !hclutils.IsIdentifier(argName) {
		return nil, emptyDiags.Extend(hclutils.ToErrorDiag(fmt.Sprintf("function %q, arg %q : name must be an identifier", fn, argName), "", block.LabelRanges[0]))
	}

	desc := ""
	descAttr := a.Attributes[attrDescription]
	if descAttr != nil {
		v, d := descAttr.Expr.Value(&hcl.EvalContext{})
		curDiags = curDiags.Extend(d)
		if !(v.IsWhollyKnown() && v.Type() == cty.String) {
			return nil, emptyDiags.Extend(hclutils.ToErrorDiag(fmt.Sprintf("function %q, arg %q : description is not a constant string", fn, argName), "", descAttr.Range))
		}
		desc = v.AsString()
	}

	defAttr := a.Attributes[attrDefault]
	var v cty.Value
	if defAttr != nil {
		v, diags = defAttr.Expr.Value(&hcl.EvalContext{})
		curDiags = curDiags.Extend(diags)
		if !v.IsWhollyKnown() {
			return nil, emptyDiags.Extend(hclutils.ToErrorDiag(fmt.Sprintf("function %q, args %q: default is not a constant", fn, argName), "", defAttr.Range))
		}
	}
	return &Arg{
		Name:        argName,
		Description: desc,
		HasDefault:  defAttr != nil,
		Default:     v,
	}, curDiags
}

// FunctionSchema is the schema for function blocks.
func FunctionSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: BlockArg, LabelNames: []string{"name"}},
			{Type: blockLocals},
		},
		Attributes: []hcl.AttributeSchema{
			{Name: attrDescription},
			{Name: attrBody, Required: true},
		},
	}
}

// ArgSchema is the schema for argument blocks.
func ArgSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: attrDescription},
			{Name: attrDefault},
		},
	}
}
