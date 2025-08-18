package functions

import (
	"fmt"

	"github.com/crossplane-contrib/function-hcl/internal/evaluator/functions/internal/funcs"
	"github.com/crossplane-contrib/function-hcl/internal/evaluator/hclutils"
	"github.com/crossplane-contrib/function-hcl/internal/evaluator/locals"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

const (
	InvokeFunctionName = "invoke"
	maxDepth           = 100
)

func (f *UserFunction) checkRefs(i *invoker) hcl.Diagnostics {
	values := DynamicObject{}
	for name, arg := range f.Args {
		values[name] = arg.Default // does not matter if there is no default
	}
	ctx := i.rootContext(values)
	lp := locals.NewProcessor()
	ctx, diags := lp.Process(ctx, f.blockContent)
	if diags.HasErrors() {
		return diags
	}
	vars := f.body.Variables()
	for _, v := range vars {
		ref := v.RootName()
		if !hclutils.HasVariable(ctx, ref) {
			diags = diags.Extend(hclutils.ToErrorDiag(fmt.Sprintf("function %s: reference to non-existent variable", f.Name), ref, v.SourceRange()))
		}
	}
	n, ok := f.body.(hclsyntax.Node)
	if ok {
		diags = diags.Extend(i.checkUserFunctionRefs(n))
	}
	return diags
}

func (f *UserFunction) invoke(i *invoker, params DynamicObject) (cty.Value, error) {
	for pName := range params {
		if _, ok := f.Args[pName]; !ok {
			return cty.NilVal, fmt.Errorf("function: %s, invalid argument %q", f.Name, pName)
		}
	}
	values := DynamicObject{}
	for name, arg := range f.Args {
		v, ok := params[name]
		if !ok {
			if !arg.HasDefault {
				return cty.NilVal, fmt.Errorf("function: %s, argument %q expected but not supplied", f.Name, name)
			}
			v = arg.Default
		}
		values[name] = v
	}
	ctx := i.rootContext(values)
	lp := locals.NewProcessor()
	ctx, diags := lp.Process(ctx, f.blockContent)
	if diags.HasErrors() {
		return cty.NilVal, diags
	}
	ret, diags := f.body.Value(ctx)
	if diags.HasErrors() {
		return cty.NilVal, diags
	}
	return ret, nil
}

type invoker struct {
	fns     map[string]*UserFunction
	depth   int
	funcMap map[string]function.Function
}

func newInvoker(fns map[string]*UserFunction) *invoker {
	if fns == nil {
		fns = map[string]*UserFunction{}
	}
	ret := &invoker{
		fns: fns,
	}
	all := funcs.All()
	f := function.New(&function.Spec{
		Description: "invokes user functions defined in the HCL source",
		Params: []function.Parameter{
			{
				Name:        "name",
				Description: "name of the user function to invoke",
				Type:        cty.String,
			},
			{
				Name:        "args",
				Description: "an object containing the arguments to the function",
				Type:        cty.DynamicPseudoType,
			},
		},
		Type: func([]cty.Value) (cty.Type, error) {
			return cty.DynamicPseudoType, nil
		},
		Impl: ret.invoke,
	})
	all[InvokeFunctionName] = f
	ret.funcMap = all
	return ret
}

func (i *invoker) rootContext(values DynamicObject) *hcl.EvalContext {
	return &hcl.EvalContext{
		Variables: values,
		Functions: i.funcMap,
	}
}

func (i *invoker) invoke(args []cty.Value, _ cty.Type) (cty.Value, error) {
	i.depth++
	if i.depth >= maxDepth {
		return cty.NilVal, fmt.Errorf("user function calls: max depth %d exceeded", maxDepth)
	}
	defer func() {
		i.depth--
	}()

	name := args[0].AsString()
	fn, ok := i.fns[name]
	if !ok {
		return cty.NilVal, fmt.Errorf("user function '%s' not found", name)
	}
	argType := args[1].Type()
	if !argType.IsObjectType() {
		return cty.NilVal, fmt.Errorf("arguments to user function '%s' is not an object, found %s", name, argType.GoString())
	}
	params := args[1].AsValueMap()
	return fn.invoke(i, params)
}

func (i *invoker) checkUserFunctionRefs(expr hclsyntax.Node) hcl.Diagnostics {
	var diags hcl.Diagnostics
	_ = hclsyntax.VisitAll(expr, func(node hclsyntax.Node) hcl.Diagnostics {
		fnCall, ok := node.(*hclsyntax.FunctionCallExpr)
		if !ok {
			return nil
		}
		if fnCall.Name != InvokeFunctionName {
			return nil
		}
		if len(fnCall.Args) != 2 {
			diags = diags.Extend(hclutils.ToErrorDiag("user function invocation has incorrect number of arguments", fmt.Sprintf("want 2, got %d", len(fnCall.Args)), fnCall.Range()))
			return nil
		}
		fnName := fnCall.Args[0]
		v, _ := fnName.Value(&hcl.EvalContext{})
		if !(v.IsWhollyKnown() && v.Type() == cty.String) {
			diags = diags.Extend(hclutils.ToErrorDiag("user function invocation is not via a static string", "", fnCall.Args[0].Range()))
			return nil
		}
		if _, ok := i.fns[v.AsString()]; !ok {
			diags = diags.Extend(hclutils.ToErrorDiag(fmt.Sprintf("invoke called on unknown function: %q", v.AsString()), "", fnCall.Args[0].Range()))
			return nil
		}
		return nil
	})
	return diags
}
