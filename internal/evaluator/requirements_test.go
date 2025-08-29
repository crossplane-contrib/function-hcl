package evaluator

import (
	"testing"

	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReqBasicMatchLabels(t *testing.T) {
	e := createTestEvaluator(t)
	ctx := createTestEvalContext()
	hclContent := `
requirement cm {
	condition = true
	locals {
		region = req.composite.spec.region
	}
	select {
		apiVersion = "v1"
		kind = "ConfigMap"
		matchLabels = {
			region: region
		}
	}
}
`
	content := parseHCL(t, e, hclContent, "test.hcl")
	diags := e.processGroup(ctx, content)
	require.False(t, diags.HasErrors())
	require.Equal(t, 1, len(e.requirements))
	require.NotNil(t, e.requirements["cm"])
	assert.Equal(t, "v1", e.requirements["cm"].ApiVersion)
	assert.Equal(t, "ConfigMap", e.requirements["cm"].Kind)
	ml, ok := e.requirements["cm"].Match.(*fnv1.ResourceSelector_MatchLabels)
	require.True(t, ok)
	assert.Equal(t, 1, len(ml.MatchLabels.Labels))
	assert.Equal(t, "us-west-2", ml.MatchLabels.Labels["region"])
}

func TestReqBasicMatchName(t *testing.T) {
	e := createTestEvaluator(t)
	ctx := createTestEvalContext()
	hclContent := `
// test this in a group and ensure local vars are evaluated correctly
group {
	locals {
		cmPrefix = "foo"
	}

	requirement cm {
		locals {
			cmName = "${cmPrefix}-bar"
		}

		select {
			apiVersion = "v1"
			kind = "ConfigMap"
			matchName = cmName
		}
	}
}
`
	content := parseHCL(t, e, hclContent, "test.hcl")
	diags := e.processGroup(ctx, content)
	require.False(t, diags.HasErrors())
	require.Equal(t, 1, len(e.requirements))
	require.NotNil(t, e.requirements["cm"])
	assert.Equal(t, "v1", e.requirements["cm"].ApiVersion)
	assert.Equal(t, "ConfigMap", e.requirements["cm"].Kind)
	mn, ok := e.requirements["cm"].Match.(*fnv1.ResourceSelector_MatchName)
	require.True(t, ok)
	assert.Equal(t, "foo-bar", mn.MatchName)
}

func TestReqBasicSkipCondition(t *testing.T) {
	e := createTestEvaluator(t)
	ctx := createTestEvalContext()
	hclContent := `
requirement cm {
	condition = false
	select {
		apiVersion = "v1"
		kind = "ConfigMap"
		matchName = "foo-bar"
	}
}
`
	content := parseHCL(t, e, hclContent, "test.hcl")
	diags := e.processGroup(ctx, content)
	require.False(t, diags.HasErrors())
	require.Equal(t, 0, len(e.requirements))
	require.Equal(t, 1, len(e.discards))
	assert.Equal(t, discardReasonUserCondition, e.discards[0].Reason)
}

func TestReqDiscardsIncomplete(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		msg  string
	}{
		{
			name: "api version",
			hcl: `
requirement cm {
	select {
		apiVersion = req.composite.metadata.foo
		kind = "ConfigMap"
		matchName = "foo-bar"
	}
}
`,
		},
		{
			name: "kind",
			hcl: `
requirement cm {
	select {
		apiVersion = "v1"
		kind = req.composite.metadata.foo
		matchName = "foo-bar"
	}
}
`,
		},
		{
			name: "match name",
			hcl: `
requirement cm {
	select {
		apiVersion = "v1"
		kind = "ConfigMap"
		matchName = req.composite.metadata.foo
	}
}
`,
		},
		{
			name: "match labels",
			hcl: `
requirement cm {
	select {
		apiVersion = "v1"
		kind = "ConfigMap"
		matchLabels =  { foo: req.composite.metadata.foo }
	}
}
`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := createTestEvaluator(t)
			ctx := createTestEvalContext()
			content := parseHCL(t, e, test.hcl, "test.hcl")
			diags := e.processGroup(ctx, content)
			require.False(t, diags.HasErrors())
			assert.Equal(t, 1, len(e.discards))
			assert.Equal(t, discardReasonIncomplete, e.discards[0].Reason)
		})
	}
}

func TestReqNegative(t *testing.T) {
	tests := []struct {
		name string
		hcl  string
		msg  string
	}{
		{
			name: "duplicate",
			hcl: `
requirement cm {
	select {
		apiVersion = "v1"
		kind = "ConfigMap"
		matchName = "foo-bar"
	}
}
requirement cm {
	select {
		apiVersion = "v1"
		kind = "ConfigMap"
		matchName = "foo-bar"
	}
}
`,
			msg: `test.hcl:9,1-15: multiple requirements with name; cm`,
		},
		{
			name: "bad req schema",
			hcl: `
requirement cm {
	select2 {
		apiVersion = "v1"
		kind = "ConfigMap"
		matchName = "foo-bar"
	}
}
`,
			msg: `test.hcl:3,2-9: Unsupported block type; Blocks of type "select2" are not expected here`,
		},
		{
			name: "no select block",
			hcl: `
requirement cm {
}
`,
			msg: `test.hcl:2,1-15: no select block in requirement; cm`,
		},
		{
			name: "multiple select blocks",
			hcl: `
requirement cm {
	select {
		apiVersion = "v1"
		kind = "ConfigMap"
		matchName = "foo-bar"
	}
	select {
		apiVersion = "v1"
		kind = "ConfigMap"
		matchName = "foo-bar"
	}
}
`,
			msg: `test.hcl:8,2-8: multiple select blocks in requirement; cm`,
		},
		{
			name: "select block no match",
			hcl: `
requirement cm {
	select {
		apiVersion = "v1"
		kind = "ConfigMap"
	}
}
`,
			msg: `test.hcl:3,2-8: requirement selector has neither matchName nor matchLabels; cm`,
		},
		{
			name: "select block multi match",
			hcl: `
requirement cm {
	select {
		apiVersion = "v1"
		kind = "ConfigMap"
		matchName = "foo-bar"
		matchLabels = { "foo": "bar" }
	}
}
`,
			msg: `test.hcl:3,2-8: requirement selector has both matchName and matchLabels; cm`,
		},
		{
			name: "bad select",
			hcl: `
requirement cm {
	select {
	}
}
`,
			msg: `test.hcl:3,9-9: Missing required argument; The argument "apiVersion" is required, but no definition was found`,
		},
		{
			name: "bad locals",
			hcl: `
requirement cm {
	locals {
		val = foo
	}
	select {
		apiVersion = "v1"
		kind = "ConfigMap"
		matchLabels = { "foo": val }
	}
}
`,
			msg: `test.hcl:4,9-12: reference to non-existent variable; foo`,
		},
		{
			name: "bad condition",
			hcl: `
requirement cm {
	condition = req.foo
	select {
		apiVersion = "v1"
		kind = "ConfigMap"
		matchLabels = { "foo": "bar" }
	}
}
`,
			msg: `test.hcl:3,17-21: Unsupported attribute; This object does not have an attribute named "foo"`,
		},
		{
			name: "bad type apiVersion",
			hcl: `
requirement cm {
	select {
		apiVersion = 10
		kind = "ConfigMap"
		matchLabels = { "foo": "bar" }
	}
}
`,
			msg: `test.hcl:4,16-18: api version in requirement selector was not a string; cm`,
		},
		{
			name: "bad type kind",
			hcl: `
requirement cm {
	select {
		apiVersion = "v1"
		kind = ["ConfigMap"]
		matchLabels = { "foo": "bar" }
	}
}
`,
			msg: `test.hcl:5,10-23: kind in requirement selector was not a string; cm`,
		},
		{
			name: "bad type matchName",
			hcl: `
requirement cm {
	select {
		apiVersion = "v1"
		kind = "ConfigMap"
		matchName = 10
	}
}
`,
			msg: `test.hcl:6,15-17: matchName in requirement selector was not a string; cm`,
		},
		{
			name: "bad type matchLabels",
			hcl: `
requirement cm {
	select {
		apiVersion = "v1"
		kind = "ConfigMap"
		matchLabels = "foo-bar"
	}
}
`,
			msg: `test.hcl:6,17-26: matchLabels in requirement selector was not an object; cm`,
		},

		{
			name: "bad type matchLabels key",
			hcl: `
requirement cm {
	select {
		apiVersion = "v1"
		kind = "ConfigMap"
		matchLabels = { foo: 10 }
	}
}
`,
			msg: `test.hcl:6,17-28: match label "foo" in requirement selector was not an string; cm`,
		},
		{
			name: "bad lazy type apiVersion",
			hcl: `
requirement cm {
	locals {
		val = 10
	}
	select {
		apiVersion = val
		kind = "ConfigMap"
		matchLabels = { "foo": "bar" }
	}
}
`,
			msg: `test.hcl:7,16-19: api version in requirement selector was not a string; cm`,
		},
		{
			name: "bad lazy type kind",
			hcl: `
requirement cm {
	locals {
		val = ["ConfigMap"]
	}
	select {
		apiVersion = "v1"
		kind = val
		matchLabels = { "foo": "bar" }
	}
}
`,
			msg: `test.hcl:8,10-13: kind in requirement selector was not a string; cm`,
		},
		{
			name: "bad lazy type matchName",
			hcl: `
requirement cm {
	locals {
		val = 10
	}
	select {
		apiVersion = "v1"
		kind = "ConfigMap"
		matchName = val
	}
}
`,
			msg: `test.hcl:9,15-18: matchName in requirement selector was not a string; cm`,
		},
		{
			name: "bad lazy type matchLabels",
			hcl: `
	locals {
		val = ["ConfigMap"]
	}
requirement cm {
	select {
		apiVersion = "v1"
		kind = "ConfigMap"
		matchLabels = val
	}
}
`,
			msg: `test.hcl:9,17-20: matchLabels in requirement selector was not an object; cm`,
		},
		{
			name: "bad lazy type matchLabels key",
			hcl: `
	locals {
		val = { foo: 10 }
	}
requirement cm {
	select {
		apiVersion = "v1"
		kind = "ConfigMap"
		matchLabels = val
	}
}
`,
			msg: `test.hcl:9,17-20: match label "foo" in requirement selector was not an string; cm`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := createTestEvaluator(t)
			ctx := createTestEvalContext()
			content := parseHCL(t, e, test.hcl, "test.hcl")
			diags := e.processGroup(ctx, content)
			require.True(t, diags.HasErrors())
			assert.Contains(t, diags.Error(), test.msg)
		})
	}
}
