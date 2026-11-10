package evaluator

import (
	"strings"

	"github.com/crossplane-contrib/function-hcl/function/internal/evaluator/hclutils"
	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

// processValidations evaluates the validation blocks at the top level and in every group whose
// condition is true, before any other block. Other blocks stop the evaluation at their first error,
// so a resource that reads the invalid input would otherwise fail with its own expression error and
// hide the message of the rule that the input breaks. Groups without validation blocks are skipped
// before their locals are evaluated, so a composition without them evaluates its locals only once.
// Errors in locals and group conditions are left to processGroup, which reports them and records
// the discarded groups.
func (e *Evaluator) processValidations(ctx *hcl.EvalContext, content *hcl.BodyContent) hcl.Diagnostics {
	if !hasValidation(content) {
		return nil
	}
	ctx, ds := e.processLocals(ctx, content)
	if ds.HasErrors() {
		return nil
	}
	if condAttr, ok := content.Attributes[attrCondition]; ok {
		v, ds := condAttr.Expr.Value(ctx)
		if ds.HasErrors() || !v.IsWhollyKnown() || v.IsNull() || v.Type() != cty.Bool || v.False() {
			return nil
		}
	}

	var diags hcl.Diagnostics
	for _, b := range content.Blocks {
		var curDiags hcl.Diagnostics
		switch b.Type {
		case blockValidation:
			curDiags = e.processValidation(ctx, b)
		case blockGroup:
			if groupContent, ds := b.Body.Content(groupSchema()); !ds.HasErrors() {
				curDiags = e.processValidations(ctx, groupContent)
			}
		}
		diags = diags.Extend(curDiags)
		if curDiags.HasErrors() {
			return diags
		}
	}
	return diags
}

// hasValidation reports whether content or one of its groups has a validation block.
func hasValidation(content *hcl.BodyContent) bool {
	for _, b := range content.Blocks {
		switch b.Type {
		case blockValidation:
			return true
		case blockGroup:
			if groupContent, ds := b.Body.Content(groupSchema()); !ds.HasErrors() && hasValidation(groupContent) {
				return true
			}
		}
	}
	return false
}

// processValidation fails the evaluation with the block's error message when its condition is
// known to be false. A condition that can't be evaluated yet is skipped with a warning, like any
// other incomplete value, because the same kind of error also means a dependency isn't ready yet.
// A condition or message whose type is known to be wrong fails the evaluation whatever the
// condition's value, so a broken block is reported before the input it checks is invalid. The
// message may read values that exist only when the input is invalid, so a message that can't be
// evaluated is an error only when the condition is false.
func (e *Evaluator) processValidation(ctx *hcl.EvalContext, block *hcl.Block) hcl.Diagnostics {
	e.validated[block.DefRange] = true
	content, diags := block.Body.Content(validationSchema())
	if diags.HasErrors() {
		return diags
	}

	condExpr := content.Attributes[attrCondition].Expr
	msgExpr := content.Attributes[attrErrMessage].Expr
	cond, condDiags := condExpr.Value(ctx)
	msg, msgDiags := msgExpr.Value(ctx)
	switch {
	case hasWrongType(cond, cty.Bool):
		return diags.Append(validationError("validation condition must be a bool", "got "+cond.Type().FriendlyName(), condExpr))
	case hasWrongType(msg, cty.String):
		return diags.Append(validationError("validation error_message must be a string", "got "+msg.Type().FriendlyName(), msgExpr))
	case condDiags.HasErrors() || !cond.IsWhollyKnown():
		context := e.messagesFromDiags(condDiags)
		if len(context) == 0 {
			context = []string{"the condition is not known yet"}
		}
		e.discard(DiscardItem{
			Type:        discardTypeValidation,
			Reason:      discardReasonIncomplete,
			Name:        attrCondition,
			SourceRange: condExpr.Range().String(),
			Context:     context,
		})
		return diags.Extend(hclutils.DowngradeDiags(condDiags))
	case cond.IsNull():
		return diags.Append(validationError("validation condition must be a bool", "got null", condExpr))
	case cond.True():
		return diags
	}

	// the input is known to be invalid, so the evaluation fails as a validation failure even when
	// the message itself can't be built
	detail := "error_message is not known yet"
	switch {
	case msgDiags.HasErrors():
		detail = "error_message could not be evaluated: " + strings.Join(e.messagesFromDiags(msgDiags), "; ")
	case msg.IsWhollyKnown() && msg.IsNull():
		detail = "error_message is null"
	case msg.IsWhollyKnown():
		detail = msg.AsString()
	}
	return diags.Append(validationError("validation failed", detail, condExpr))
}

func validationError(summary, detail string, expr hcl.Expression) *hcl.Diagnostic {
	return &hcl.Diagnostic{Severity: hcl.DiagError, Summary: summary, Detail: detail, Subject: ptr(expr.Range())}
}

// hasWrongType reports whether v's type is known and is not want, even when v is not known or its
// evaluation had errors. A value whose type is not known yet can still be of the right type.
func hasWrongType(v cty.Value, want cty.Type) bool {
	t := v.Type()
	return t != want && t != cty.DynamicPseudoType && t != cty.NilType
}
