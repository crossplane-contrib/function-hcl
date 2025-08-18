package locals

import (
	"fmt"
	"strings"

	"github.com/crossplane-contrib/function-hcl/internal/evaluator/hclutils"
	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

const BlockLocals = "locals"

type dynamicObject = map[string]cty.Value

// Processor processes local declarations in a block. This is the workhorse of the evaluator.
// It computes dependencies across local variables and evaluates them in dependency order checking for circularity.
// Given an eval context, it checks whether there is any expression that relies on an unknown local or on
// other unknown values in the eval context and produces an error if that is the case.
// At the end of processing, it returns a child context with the locals having computed values.
// Note that "computed" does not mean "complete" - locals may have incomplete values if they refer to resource
// properties that are not yet known.
type Processor struct{}

// NewProcessor returns a locals processor.
func NewProcessor() *Processor {
	return &Processor{}
}

// exprDeps tracks the dependencies in an HCL expression.
type exprDeps struct {
	expr hcl.Expression
	deps []string
}

// Process processes all local blocks found in the supplied body contents as a single unit and returns a child
// context which has values for all locals.
func (l *Processor) Process(ctx *hcl.EvalContext, content *hcl.BodyContent) (*hcl.EvalContext, hcl.Diagnostics) {
	var diags hcl.Diagnostics
	var attrsList []hcl.Attributes
	for _, block := range content.Blocks {
		if block.Type == BlockLocals {
			attrs, ds := block.Body.JustAttributes()
			diags = diags.Extend(ds)
			if ds.HasErrors() {
				return nil, diags
			}
			attrsList = append(attrsList, attrs)
		}
	}
	childCtx, ds := l.evaluate(ctx, attrsList)
	return childCtx, diags.Extend(ds)
}

// evaluate returns a child context that has the values for all supplied locals evaluated in dependency order.
func (l *Processor) evaluate(ctx *hcl.EvalContext, attrsList []hcl.Attributes) (*hcl.EvalContext, hcl.Diagnostics) {
	locals := map[string]*exprDeps{}
	for _, attrs := range attrsList {
		for name, attr := range attrs {
			if _, ok := locals[name]; ok {
				return nil, hclutils.ToErrorDiag(fmt.Sprintf("local %q: duplicate local declaration", name), "", attr.Range)
			}
			if hclutils.HasVariable(ctx, name) {
				return nil, hclutils.ToErrorDiag("attempt to shadow variable", name, attr.Range)
			}
			locals[name] = &exprDeps{expr: attr.Expr}
		}
	}

	// if no locals defined, there is nothing to do.
	if len(locals) == 0 {
		return ctx, nil
	}

	diags := l.computeDeps(ctx, locals)
	if diags.HasErrors() {
		return nil, diags
	}
	childCtx := ctx.NewChild()
	childCtx.Variables = dynamicObject{}
	return childCtx, diags.Extend(l.eval(childCtx, locals))
}

// Expressions returns expressions keyed by local name under the supplied content.
func (l *Processor) Expressions(content *hcl.BodyContent) (map[string]hcl.Expression, hcl.Diagnostics) {
	var attrsList []hcl.Attributes
	for _, block := range content.Blocks {
		if block.Type == BlockLocals {
			attrs, diags := block.Body.JustAttributes()
			if diags.HasErrors() {
				return nil, diags
			}
			attrsList = append(attrsList, attrs)
		}
	}
	ret := map[string]hcl.Expression{}
	for _, attrs := range attrsList {
		for _, attr := range attrs {
			ret[attr.Name] = attr.Expr
		}
	}
	return ret, nil
}

// computeDeps computes the dependency maps between locals, also checking for typos in expressions in the process.
// Note that it does *not* check for circularity at this stage.
func (l *Processor) computeDeps(ctx *hcl.EvalContext, locals map[string]*exprDeps) hcl.Diagnostics {
	for name, info := range locals {
		deps := info.expr.Variables()
		for _, dep := range deps {
			reference := dep.RootName()
			if _, ok := locals[reference]; ok {
				locals[name].deps = append(locals[name].deps, reference)
			} else if !hclutils.HasVariable(ctx, reference) {
				return hclutils.ToErrorDiag("reference to non-existent variable", reference, dep.SourceRange())
			}
		}
	}
	return nil
}

type evalPath struct {
	path []string
}

func (e *evalPath) push(name string) error {
	for index, v := range e.path {
		if v == name {
			//nolint:gocritic
			subPath := append(e.path[index:], name)
			return fmt.Errorf("cycle found: %s", strings.Join(subPath, " \u2192 "))
		}
	}
	e.path = append(e.path, name)
	return nil
}

func (e *evalPath) pop() {
	e.path = e.path[:len(e.path)-1]
}

type localContext struct {
	ctx       *hcl.EvalContext
	locals    map[string]*exprDeps
	seen      *evalPath
	remaining map[string]bool
}

// eval evaluates all locals in dependency order.
func (l *Processor) eval(ctx *hcl.EvalContext, locals map[string]*exprDeps) hcl.Diagnostics {
	var diags hcl.Diagnostics

	remaining := map[string]bool{}
	for name := range locals {
		remaining[name] = true
	}
	for name := range locals {
		diags = diags.Extend(l.evalLocal(&localContext{
			ctx:       ctx,
			locals:    locals,
			seen:      &evalPath{},
			remaining: remaining,
		}, name))
	}
	return diags
}

// evalLocal evals a single local, ensuring that its dependencies are evaluated first.
func (l *Processor) evalLocal(c *localContext, name string) hcl.Diagnostics {
	var diags hcl.Diagnostics
	if !c.remaining[name] { // already processed
		return nil
	}
	// check cycles
	if err := c.seen.push(name); err != nil {
		return hclutils.ToErrorDiag(err.Error(), "", c.locals[name].expr.Range())
	}
	defer func() {
		c.seen.pop()
		delete(c.remaining, name)
	}()

	// ensure dependencies are evaluated first
	info := c.locals[name]
	for _, dep := range info.deps {
		if c.remaining[dep] {
			diags = diags.Extend(l.evalLocal(c, dep))
		}
	}
	if diags.HasErrors() {
		return diags
	}

	// evaluate local
	// val will be an unknown value if it cannot be eval-ed
	// we ignore errors due to incomplete values.
	val, ds := info.expr.Value(c.ctx)
	// rewrite the severity of errors due to incomplete values to warnings as we'll handle them later
	diags = diags.Extend(hclutils.DowngradeDiags(ds))

	// having evaluated it, update the context with the new kv.
	c.ctx.Variables[name] = val
	return diags
}
