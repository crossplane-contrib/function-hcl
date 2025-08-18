package functions

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

type DynamicObject = map[string]cty.Value

// Arg represents an argument for a user-defined function.
type Arg struct {
	Name        string    // argument name
	Description string    // optional description
	HasDefault  bool      // true if it has a default value
	Default     cty.Value // the default value
}

// UserFunction represents a user-defined function.
type UserFunction struct {
	Name         string           // user function name
	Description  string           // optional description
	Args         map[string]*Arg  // named arguments
	body         hcl.Expression   // result expression
	blockContent *hcl.BodyContent // function block in which to find locals blocks
}

// Processor loads user functions and provides mechanisms to provide a root context.
// capable of invoking these functions.
type Processor struct {
	Functions map[string]*UserFunction
	invoker   *invoker
}

// NewProcessor creates a processor.
func NewProcessor() *Processor {
	return &Processor{
		Functions: map[string]*UserFunction{},
		invoker:   newInvoker(nil),
	}
}

// Process processes the supplied body for function definitions.
func (e *Processor) Process(content *hcl.BodyContent) hcl.Diagnostics {
	return e.processFunctions(content)
}

// RootContext provides a root context with the supplied variables and
// functions that have the standard functions as well as the special `invoke`
// function to invoke user-defined functions.
func (e *Processor) RootContext(values DynamicObject) *hcl.EvalContext {
	if values == nil {
		values = DynamicObject{}
	}
	return e.invoker.rootContext(values)
}

func (e *Processor) CheckUserFunctionRefs(expr hclsyntax.Node) hcl.Diagnostics {
	return e.invoker.checkUserFunctionRefs(expr)
}
