package evaluator

import (
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

// processValidations skips content without validation blocks before evaluating its locals, so a
// composition without them evaluates its locals only once.
func TestHasValidation(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		want bool
	}{
		{
			name: "none",
			hcl: `
locals {
  a = 1
}
group {
  locals {
    b = 2
  }
}
`,
		},
		{
			name: "top level",
			hcl: `
validation {
  condition     = true
  error_message = "m"
}
`,
			want: true,
		},
		{
			name: "nested group",
			hcl: `
group {
  group {
    validation {
      condition     = true
      error_message = "m"
    }
  }
}
`,
			want: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file, diags := hclsyntax.ParseConfig([]byte(test.hcl), "main.hcl", hcl.InitialPos)
			require.False(t, diags.HasErrors(), diags.Error())
			content, diags := file.Body.Content(topLevelSchema())
			require.False(t, diags.HasErrors(), diags.Error())
			assert.Equal(t, test.want, hasValidation(content))
		})
	}
}

// A group condition that calls timestamp() or uuid() can be false when processValidations
// evaluates it and true when processGroup does. The group's validations must still run before
// its resources render.
func TestValidationGroupConditionChanges(t *testing.T) {
	const src = `
group {
  condition = flip()

  validation {
    condition     = false
    error_message = "the group input is invalid"
  }
}
`
	file, diags := hclsyntax.ParseConfig([]byte(src), "main.hcl", hcl.InitialPos)
	require.False(t, diags.HasErrors(), diags.Error())
	content, diags := file.Body.Content(topLevelSchema())
	require.False(t, diags.HasErrors(), diags.Error())

	calls := 0
	ctx := &hcl.EvalContext{Functions: map[string]function.Function{
		"flip": function.New(&function.Spec{
			Type: function.StaticReturnType(cty.Bool),
			Impl: func([]cty.Value, cty.Type) (cty.Value, error) {
				calls++
				return cty.BoolVal(calls > 1), nil
			},
		}),
	}}
	e, err := New(Options{})
	require.NoError(t, err)

	require.False(t, e.processValidations(ctx, content).HasErrors())
	diags = e.processGroup(ctx, content)
	require.True(t, diags.HasErrors())
	assert.Contains(t, diags.Error(), "validation failed; the group input is invalid")
	assert.Equal(t, 2, calls)
}
