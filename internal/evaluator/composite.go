package evaluator

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/crossplane-contrib/function-hcl/internal/evaluator/hclutils"
	"github.com/hashicorp/hcl/v2"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

func (e *Evaluator) processComposite(ctx *hcl.EvalContext, block *hcl.Block) hcl.Diagnostics {
	content, diags := block.Body.Content(compositeSchema())
	if diags.HasErrors() {
		return diags
	}

	ctx, ds := e.processLocals(ctx, content)
	diags = diags.Extend(ds)
	if ds.HasErrors() {
		return ds
	}

	values, ds := e.bodyExpression(content, block.DefRange)
	diags = diags.Extend(ds)
	if ds.HasErrors() {
		return ds
	}

	what := block.Labels[0]
	switch what {
	case blockLabelStatus:
		diags = diags.Extend(e.addStatus(ctx, values))
	case blockLabelConnection:
		diags = diags.Extend(e.addConnectionDetails(ctx, values))
	default:
		diags = diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  fmt.Sprintf("invalid composite label: %s", what),
		})
	}
	return diags
}

func (e *Evaluator) addStatus(ctx *hcl.EvalContext, attrs hcl.Expression) hcl.Diagnostics {
	values, diags := e.attributesToValueMap(ctx, attrs, discardTypeStatus)
	if values == nil {
		return diags
	}
	e.compositeStatuses = append(e.compositeStatuses, values)
	return diags
}

func (e *Evaluator) addConnectionDetails(ctx *hcl.EvalContext, attrs hcl.Expression) hcl.Diagnostics {
	out, diags := e.attributesToValueMap(ctx, attrs, discardTypeConnection)
	if out == nil {
		return diags
	}

	values := map[string][]byte{}
	hasDiscards := false
	for name, v := range out {
		val, ok := v.(string)
		if !ok {
			diags = diags.Append(&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  fmt.Sprintf("connection key %q was not a string, got %T", name, v),
			})
			// continue processing to collect additional warnings and errors
			continue
		}
		// make sure that the value can be decoded to bytes
		b, err := base64.StdEncoding.DecodeString(val)
		if err != nil { // do not print the value, it could be a secret in plain text
			e.discard(DiscardItem{
				Type:        discardTypeConnection,
				Reason:      discardReasonBadSecret,
				Name:        name,
				SourceRange: attrs.Range().String(),
				Context:     []string{fmt.Sprintf("connection secret key %q not in base64 format", name)},
			})
			// do not error out for this.
			diags = diags.Append(&hcl.Diagnostic{
				Severity: hcl.DiagWarning,
				Summary:  fmt.Sprintf("connection secret key %q not in base64 format", name),
			})
			// mark that we have discards but continue processing to collect additional warnings and errors
			hasDiscards = true
		} else {
			values[name] = b
		}
	}
	if hasDiscards || diags.HasErrors() {
		return diags
	}
	e.compositeConnections = append(e.compositeConnections, values)
	return diags
}

func (e *Evaluator) attributesToValueMap(ctx *hcl.EvalContext, expr hcl.Expression, eType DiscardType) (Object, hcl.Diagnostics) {
	value, diags := expr.Value(ctx)
	if diags.HasErrors() || !value.IsWhollyKnown() {
		// discard the object
		e.discard(DiscardItem{
			Type:        eType,
			Reason:      discardReasonIncomplete,
			SourceRange: expr.Range().String(),
			Context:     e.messagesFromDiags(diags),
		})
		// remap errors to warnings as we'll handle discarded objects later
		return nil, hclutils.DowngradeDiags(diags)
	}
	b, err := ctyjson.Marshal(value, value.Type())
	if err != nil {
		return nil, diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  fmt.Sprintf("error marshaling cty value: %s", err.Error()),
			Subject:  ptr(expr.Range()),
		})
	}
	var ret Object
	err = json.Unmarshal(b, &ret)
	if err != nil {
		return nil, diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  fmt.Sprintf("error unmarshaling cty value: %s", err.Error()),
			Subject:  ptr(expr.Range()),
		})
	}
	return ret, nil
}
