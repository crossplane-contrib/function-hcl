package functions_test

import (
	"strings"
	"testing"

	"github.com/crossplane-contrib/function-hcl/internal/evaluator/functions"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

// parseFunctionsHCL parses HCL content containing function declarations and returns body content.
func parseFunctionsHCL(t *testing.T, content string) *hcl.BodyContent {
	t.Helper()
	parser := hclparse.NewParser()
	file, diags := parser.ParseHCL([]byte(content), "test.hcl")
	require.False(t, diags.HasErrors(), "Failed to parse HCL: %s", diags)

	schema := &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "function", LabelNames: []string{"name"}},
			{Type: "locals"},
		},
	}

	contentBody, diags := file.Body.Content(schema)
	require.False(t, diags.HasErrors(), "Failed to get content: %s", diags)
	return contentBody
}

func parseExpression(t *testing.T, str string) hclsyntax.Expression {
	if strings.HasPrefix(str, "${") {
		expr, err := hclsyntax.ParseTemplate([]byte(str), "template.hcl", hcl.Pos{Line: 1, Column: 1})
		require.False(t, err.HasErrors())
		return expr
	}
	expr, err := hclsyntax.ParseExpression([]byte(str), "expr.hcl", hcl.Pos{Line: 1, Column: 1})
	require.False(t, err.HasErrors())
	return expr
}

func TestBasicFunctions(t *testing.T) {
	defs := parseFunctionsHCL(t, `
locals {}
function mX {
	description = "scales a number by m"
	arg n {
		description = "input"
	}
	arg m {
		default = 2
		description = "multiplier"
	}
	body = m * n
}

function plusK {
	arg n {
		description = "input"
	}
	arg k {
		default = 1
		description = "addend"
	}
	body = n + k
}

function twoXPlus1 {
	arg n {
		description = "input"
	}
	locals {
		mult = invoke("mX", { n: n})
		add = invoke("plusK", { n: mult})
	}
	body = add
}
`)
	p := functions.NewProcessor()
	diags := p.Process(defs)
	require.False(t, diags.HasErrors())

	expr := parseExpression(t, `invoke("twoXPlus1", {n: 100})`)
	ctx := p.RootContext(nil)
	v, diags := expr.Value(ctx)
	require.False(t, diags.HasErrors())
	require.Equal(t, v.Type(), cty.Number)
	out, _ := v.AsBigFloat().Int64()
	assert.EqualValues(t, 201, out)
}

func TestRecursiveFunction(t *testing.T) {
	defs := parseFunctionsHCL(t, `
function factorial {
	arg n {}
	body = n < 1 ? 1 : n * invoke("factorial", { n: n - 1 })
}
`)

	p := functions.NewProcessor()
	diags := p.Process(defs)
	require.False(t, diags.HasErrors())
	expr := parseExpression(t, `invoke("factorial", { n: 5 })`)
	ctx := p.RootContext(nil)
	v, diags := expr.Value(ctx)
	require.False(t, diags.HasErrors())
	require.Equal(t, v.Type(), cty.Number)
	out, _ := v.AsBigFloat().Int64()
	assert.EqualValues(t, 120, out)

	// check for max depth
	expr = parseExpression(t, `invoke("factorial", { n: 101 })`)
	_, diags = expr.Value(ctx)
	require.True(t, diags.HasErrors())
	assert.Contains(t, diags.Error(), "user function calls: max depth 100 exceeded")
}

func TestFunctionCallsNegative(t *testing.T) {
	defs := parseFunctionsHCL(t, `
function mX {
	arg n {
		description = "input"
	}
	arg m {
		default = 2
		description = "multiplier"
	}
	body = m * n
}
`)

	p := functions.NewProcessor()
	diags := p.Process(defs)
	require.False(t, diags.HasErrors())
	ctx := p.RootContext(nil)

	tests := []struct {
		name string
		expr string
		msg  string
	}{
		{
			name: "missing function arg",
			expr: `invoke("mX", {})`,
			msg:  `function: mX, argument "n" expected but not supplied`,
		},
		{
			name: "extra function arg",
			expr: `invoke("mX", { n: 10, m2: 4})`,
			msg:  `function: mX, invalid argument "m2"`,
		},
		{
			name: "bad call syntax 1",
			expr: `invoke("mX")`,
			msg:  `Function "invoke" expects 2 argument(s)`,
		},
		{
			name: "bad call syntax 2",
			expr: `invoke("mX", 10)`,
			msg:  `arguments to user function 'mX' is not an object, found cty.Number`,
		},
		{
			name: "bad function name",
			expr: `invoke("nX", { n: 10})`,
			msg:  `user function 'nX' not found`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := parseExpression(t, test.expr)
			_, diags := e.Value(ctx)
			require.True(t, diags.HasErrors())
			assert.Contains(t, diags.Error(), test.msg)
		})
	}
}

func TestProcessFunctionsNegative(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		msg  string
	}{
		{
			name: "function does not match schema",
			msg:  `test.hcl:3,2-6: Unsupported block type; Blocks of type "arg2" are not expected here`,
			hcl: `
function x { 
	arg2 y {} 
	body = y
}
			`,
		},
		{
			name: "function name not identifier",
			msg:  `test.hcl:2,10-20: function "x plus y" : name must be an identifier`,
			hcl: `
function "x plus y" { 
	arg y {} 
	body = y
}
			`,
		},
		{
			name: "arg name not identifier",
			msg:  `test.hcl:3,6-14: function "x", arg "plus y" : name must be an identifier`,
			hcl: `
function x { 
	arg "plus y" {} 
	body = "x"
}
			`,
		},
		{
			name: "arg does not match schema",
			msg:  `test.hcl:3,10-18: Unsupported argument; An argument named "default2" is not expected here`,
			hcl: `
function x { 
	arg y { default2 = 10 } 
	body = y
}
			`,
		},
		{
			name: "duplicate function declaration",
			msg:  `test.hcl:6,1-11: duplicate function declaration; x`,
			hcl: `
function x { 
	arg y {} 
	body = y
}
function x { 
	arg z {} 
	body = z
}
			`,
		},
		{
			name: "duplicate arg declaration",
			msg:  `test.hcl:4,2-7: function x: duplicate definition of argument; y`,
			hcl: `
function x { 
	arg y {} 
	arg y {}
	body = y
}
			`,
		},
		{
			name: "bad function description",
			msg:  `test.hcl:3,2-19: function x : description is not a constant string`,
			hcl: `
function x { 
	description = 100
	arg y {}
	body = y
}
			`,
		},
		{
			name: "bad arg description",
			msg:  `test.hcl:4,10-27: function "x", arg "y" : description is not a constant string`,
			hcl: `
function x { 
	description = "f(x)"
	arg y { description = 100 }
	body = y
}
			`,
		},
		{
			name: "non constant default",
			msg:  `test.hcl:7,10-21: function "x", args "y": default is not a constant`,
			hcl: `
locals {
	z = 100
}
function x { 
	description = "f(x)"
	arg y { default = z }
	body = y
}
			`,
		},
		{
			name: "bad locals",
			msg:  `cycle found`,
			hcl: `
function x { 
	description = "f(x)"
	arg y { default = 100 }
	locals {
		a = b
		b = a
	}
	body = y
}
			`,
		},
		{
			name: "bad refs",
			msg:  `reference to non-existent variable; z, and 1 other diagnostic(s)`,
			hcl: `
function x { 
	arg y {}
	body = z
}

function y { 
	arg p {}
	body = z
}
			`,
		},
		{
			name: "bad function call 1",
			msg:  `test.hcl:4,16-17: user function invocation is not via a static string;`,
			hcl: `
function x { 
	arg y {}
	body = invoke(y, {})
}
			`,
		},
		{
			name: "bad function call 2",
			msg:  `test.hcl:4,9-17: user function invocation has incorrect number of arguments; want 2, got 0`,
			hcl: `
function x { 
	arg y {}
	body = invoke()
}
			`,
		},
		{
			name: "bad function call 3",
			msg:  `test.hcl:4,15-44: user function invocation has incorrect number of arguments; want 2, got 3`,
			hcl: `
function x { 
	arg y {}
	body = upper(invoke("y", {a: 10}, {b: 20}))
}
			`,
		},
		{
			name: "bad function call 4",
			msg:  `test.hcl:4,16-19: invoke called on unknown function: "y"`,
			hcl: `
function x { 
	arg y {}
	body = invoke("y", {a: y})
}
			`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defs := parseFunctionsHCL(t, test.hcl)
			p := functions.NewProcessor()
			diags := p.Process(defs)
			require.True(t, diags.HasErrors())
			assert.Contains(t, diags.Error(), test.msg)
		})
	}
}

func TestProcessorCheckRefs(t *testing.T) {
	p := functions.NewProcessor()
	diags := p.Process(parseFunctionsHCL(t, `
function plus10 {
	arg n {}
	body = n + 10
}
`))
	assert.False(t, diags.HasErrors())
	diags = p.CheckUserFunctionRefs(parseExpression(t, `invoke("plus10",{n : 10})`))
	assert.False(t, diags.HasErrors())

	diags = p.CheckUserFunctionRefs(parseExpression(t, `invoke("plus20",{n : 10})`))
	assert.True(t, diags.HasErrors())
	assert.Contains(t, diags.Error(), `expr.hcl:1,8-16: invoke called on unknown function: "plus20"`)
}
