package evaluator

import (
	"fmt"

	"github.com/crossplane-contrib/function-hcl/internal/evaluator/hclutils"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

type selection struct {
	sourceRange hcl.Range
	apiVersion  hcl.Expression
	kind        hcl.Expression
	hasName     bool
	matchName   hcl.Expression
	matchLabels hcl.Expression
}

func (e *Evaluator) checkRequirementBlock(block *hcl.Block, content *hcl.BodyContent) (*selection, hcl.Diagnostics) {
	name := block.Labels[0]

	var curDiags hcl.Diagnostics
	// extract a single select block
	var selBlock *hcl.Block
	for _, b := range content.Blocks {
		if b.Type == blockSelect {
			if selBlock != nil {
				return nil, hclutils.ToErrorDiag("multiple select blocks in requirement", name, b.DefRange)
			}
			selBlock = b
		}
	}
	if selBlock == nil {
		return nil, hclutils.ToErrorDiag("no select block in requirement", name, block.DefRange)
	}

	// verify basic structure of selection
	sel, diags := e.selectBlockToSelection(name, selBlock)
	curDiags = curDiags.Extend(diags)
	if diags.HasErrors() {
		return nil, diags
	}
	return sel, curDiags
}

func (e *Evaluator) processRequirement(ctx *hcl.EvalContext, block *hcl.Block) hcl.Diagnostics {
	var curDiags hcl.Diagnostics

	// get name, check duplicates
	name := block.Labels[0]
	if _, ok := e.requirements[name]; ok {
		return hclutils.ToErrorDiag("multiple requirements with name", name, block.DefRange)
	}

	// verify schema
	content, diags := block.Body.Content(requirementSchema())
	curDiags = curDiags.Extend(diags)
	if diags.HasErrors() {
		return diags
	}

	// check the block for structural and other errors
	sel, diags := e.checkRequirementBlock(block, content)
	curDiags = curDiags.Extend(diags)
	if diags.HasErrors() {
		return diags
	}

	// process locals so that selection can be evaluated
	ctx, diags = e.processLocals(ctx, content)
	curDiags = curDiags.Extend(diags)
	if diags.HasErrors() {
		return diags
	}

	// check any conditional setting
	cond, diags := e.evaluateCondition(ctx, content, discardTypeRequirement, name)
	curDiags = curDiags.Extend(diags)
	if diags.HasErrors() {
		return diags
	}
	if !cond {
		return curDiags
	}

	// evaluate the selector
	selector, diags := e.selectionToSelector(name, ctx, sel)
	curDiags = curDiags.Extend(diags)
	if diags.HasErrors() {
		return diags
	}

	// the selector can be nil if it is itself incomplete and waiting on other values
	if sel != nil {
		e.requirements[name] = selector
	}
	return curDiags
}

// selectBlockToSelection checks for overall correctness of the supplied select block without regard to actual values.
func (e *Evaluator) selectBlockToSelection(requirementName string, block *hcl.Block) (*selection, hcl.Diagnostics) {
	var curDiags hcl.Diagnostics
	content, diags := block.Body.Content(selectSchema())
	curDiags = curDiags.Extend(diags)
	if diags.HasErrors() {
		return nil, diags
	}

	_, hasName := content.Attributes[attrMatchName]
	_, hasLabels := content.Attributes[attrMatchLabels]

	switch {
	case hasName && hasLabels:
		return nil, hclutils.ToErrorDiag("requirement selector has both matchName and matchLabels", requirementName, block.DefRange)
	case !(hasName || hasLabels):
		return nil, hclutils.ToErrorDiag("requirement selector has neither matchName nor matchLabels", requirementName, block.DefRange)
	}

	sel := &selection{
		sourceRange: block.DefRange,
		apiVersion:  content.Attributes[attrAPIVersion].Expr,
		kind:        content.Attributes[attrKind].Expr,
		hasName:     hasName,
	}
	if hasName {
		sel.matchName = content.Attributes[attrMatchName].Expr
	} else {
		sel.matchLabels = content.Attributes[attrMatchLabels].Expr
	}

	// use an empty context to evaluate expressions and check their types when hardcoded values are present
	ctx := &hcl.EvalContext{Variables: map[string]cty.Value{}}

	checkStringAttr := func(name string, expr hcl.Expression) {
		v, _ := expr.Value(ctx)
		if v.IsWhollyKnown() && v.Type() != cty.String {
			curDiags = curDiags.Extend(hclutils.ToErrorDiag(fmt.Sprintf("%s in requirement selector was not a string", name), requirementName, expr.Range()))
		}
	}
	checkStringAttr("api version", sel.apiVersion)
	checkStringAttr("kind", sel.kind)
	if sel.hasName {
		checkStringAttr("matchName", sel.matchName)
	} else {
		labelsVal, _ := sel.matchLabels.Value(ctx)
		if labelsVal.IsWhollyKnown() {
			if !labelsVal.Type().IsObjectType() {
				curDiags = curDiags.Extend(hclutils.ToErrorDiag("matchLabels in requirement selector was not an object", requirementName, sel.matchLabels.Range()))
			} else {
				val := labelsVal.AsValueMap()
				for k, v := range val {
					if v.Type() != cty.String {
						curDiags = curDiags.Extend(hclutils.ToErrorDiag(fmt.Sprintf("match label %q in requirement selector was not an string", k), requirementName, sel.matchLabels.Range()))
					}
				}
			}
		}
	}
	return sel, curDiags
}

// selectionToSelector returns a resource selector for the supplied selection evaluated in the supplied context.
// It returns a nil selector in case certain values in the selection are unknown. It returns an error if a value
// is known and is malformed in some way.
func (e *Evaluator) selectionToSelector(requirementName string, ctx *hcl.EvalContext, s *selection) (out *fnv1.ResourceSelector, outDiags hcl.Diagnostics) {
	defer func() {
		if out == nil && !outDiags.HasErrors() {
			e.discard(DiscardItem{
				Type:        discardTypeRequirement,
				Reason:      discardReasonIncomplete,
				Name:        requirementName,
				SourceRange: s.sourceRange.String(),
			})
		}
	}()

	apiVersion, diags := s.apiVersion.Value(ctx)
	if !apiVersion.IsWhollyKnown() {
		return nil, hclutils.DowngradeDiags(diags)
	}
	if apiVersion.Type() != cty.String {
		return nil, hclutils.ToErrorDiag("api version in requirement selector was not a string", requirementName, s.apiVersion.Range())
	}

	kind, diags := s.kind.Value(ctx)
	if !kind.IsWhollyKnown() {
		return nil, hclutils.DowngradeDiags(diags)
	}
	if kind.Type() != cty.String {
		return nil, hclutils.ToErrorDiag("kind in requirement selector was not a string", requirementName, s.kind.Range())
	}

	if s.hasName {
		name, diags := s.matchName.Value(ctx)
		if !name.IsWhollyKnown() {
			return nil, hclutils.DowngradeDiags(diags)
		}
		if name.Type() != cty.String {
			return nil, hclutils.ToErrorDiag("matchName in requirement selector was not a string", requirementName, s.matchName.Range())
		}
		return &fnv1.ResourceSelector{
			ApiVersion: apiVersion.AsString(),
			Kind:       kind.AsString(),
			Match: &fnv1.ResourceSelector_MatchName{
				MatchName: name.AsString(),
			},
		}, nil
	}

	labelsVal, diags := s.matchLabels.Value(ctx)
	if !labelsVal.IsWhollyKnown() {
		return nil, hclutils.DowngradeDiags(diags)
	}

	if !labelsVal.Type().IsObjectType() {
		return nil, hclutils.ToErrorDiag("matchLabels in requirement selector was not an object", requirementName, s.matchLabels.Range())
	}
	labels := map[string]string{}
	val := labelsVal.AsValueMap()
	for k, v := range val {
		if v.Type() != cty.String {
			return nil, hclutils.ToErrorDiag(fmt.Sprintf("match label %q in requirement selector was not an string", k), requirementName, s.matchLabels.Range())
		}
		labels[k] = v.AsString()
	}
	return &fnv1.ResourceSelector{
		ApiVersion: apiVersion.AsString(),
		Kind:       kind.AsString(),
		Match: &fnv1.ResourceSelector_MatchLabels{
			MatchLabels: &fnv1.MatchLabels{Labels: labels},
		},
	}, nil
}
