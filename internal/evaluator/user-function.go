package evaluator

import (
	"fmt"

	"github.com/crossplane-contrib/function-hcl/internal/funcs"
	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

type Arg struct {
	Name        string
	Description string
	HasDefault  bool
	Default     cty.Value
}

type UserFunction struct {
	Name         string
	Description  string
	Args         map[string]*Arg
	body         hcl.Expression
	blockContent *hcl.BodyContent
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
	lp := newLocalsProcessor(i.finder)
	ctx, diags := lp.process(ctx, f.blockContent)
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
	finder   sourceFinder
	fns      map[string]*UserFunction
	depth    int
	maxDepth int
	funcMap  map[string]function.Function
}

func newInvoker(fns map[string]*UserFunction, finder sourceFinder) *invoker {
	if fns == nil {
		fns = map[string]*UserFunction{}
	}
	ret := &invoker{
		finder:   finder,
		fns:      fns,
		maxDepth: 100,
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
	all[userFunctionInvokerName] = f
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
	if i.depth >= i.maxDepth {
		return cty.NilVal, fmt.Errorf("user function calls: max depth exceeded")
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
