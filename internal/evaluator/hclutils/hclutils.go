package hclutils

import (
	"fmt"
	"regexp"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

// reIdent is a regular expression that can test for HCL identifiers that are allowed to contain dashes.
var reIdent = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*$`)

// IsIdentifier returns true if the supplied string can be interpreted as an HCL identifier.
func IsIdentifier(s string) bool {
	return reIdent.MatchString(s)
}

// NormalizeTraversal normalizes an index traversal to an attribute traversal for known cases.
// (i.e. x["foo"] is effectively turned to x.foo).
func NormalizeTraversal(t hcl.Traversal) hcl.Traversal {
	var ret hcl.Traversal
loop:
	for _, item := range t {
		switch item := item.(type) {
		case hcl.TraverseRoot:
			ret = append(ret, item)
		case hcl.TraverseAttr:
			ret = append(ret, item)
		case hcl.TraverseIndex:
			k := item.Key
			if k.Type() == cty.String && IsIdentifier(k.AsString()) {
				ret = append(ret, hcl.TraverseAttr{
					Name:     k.AsString(),
					SrcRange: item.SrcRange,
				})
				continue loop
			}
			ret = append(ret, item)
		default:
			panic(fmt.Errorf("unexpected traversal type: %T", item))
		}
	}
	return ret
}

// DowngradeDiags downgrades all errors in the supplied diags to warnings and returns it.
// This is a destructive operation, clone the diags before calling this function if you need the original.
func DowngradeDiags(diags hcl.Diagnostics) hcl.Diagnostics {
	for i := range diags {
		if diags[i].Severity == hcl.DiagError {
			diags[i].Severity = hcl.DiagWarning
		}
	}
	return diags
}

// Err2Diag converts an error to a hcl.Diagnostic.
func Err2Diag(err error) *hcl.Diagnostic {
	return &hcl.Diagnostic{
		Severity: hcl.DiagError,
		Summary:  err.Error(),
	}
}

// ToErrorDiag create diagnostics with the supplied summary, details, and range.
func ToErrorDiag(summary string, details string, r hcl.Range) hcl.Diagnostics {
	ret := &hcl.Diagnostic{
		Severity: hcl.DiagError,
		Summary:  summary,
		Detail:   details,
		Subject:  &r,
		Context:  &r,
	}
	return []*hcl.Diagnostic{ret}
}

// HasVariable returns true if the supplied name is defined in the current or any ancestor context.
func HasVariable(ctx *hcl.EvalContext, name string) bool {
	c := ctx
	for c != nil {
		if _, ok := c.Variables[name]; ok {
			return true
		}
		c = c.Parent()
	}
	return false
}
