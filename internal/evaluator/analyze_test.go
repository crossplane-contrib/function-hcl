package evaluator

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyzerFailures(t *testing.T) {
	tests := []struct {
		name   string
		hcl    string
		errMsg string
	}{
		{
			name:   "bad syntax",
			hcl:    `resource foo {`,
			errMsg: "test.hcl:1,14-15: Unclosed configuration block; There is no closing brace for this block",
		},
		{
			name:   "bad resource block",
			hcl:    `resource foo bar {}`,
			errMsg: "test.hcl:1,14-17: Extraneous label for resource",
		},
		{
			name: "bad resources block",
			hcl: `
resources foo  {
	body = {
	}
}
`,
			errMsg: `test.hcl:2,16-16: Missing required argument; The argument "for_each" is required`,
		},
		{
			name: "bad composite block",
			hcl: `
resource foo  {
	body = {
	}
	composite {}
}
`,
			errMsg: "test.hcl:5,12-13: Missing object for composite; All composite blocks must have 1 labels",
		},
		{
			name: "bad ready block",
			hcl: `
resource foo  {
	body = {
	}
	ready = true 
}
`,
			errMsg: `test.hcl:5,2-7: Unsupported argument; An argument named "ready" is not expected here`,
		},
		{
			name:   "bad group block",
			hcl:    `group foo bar {}`,
			errMsg: `Extraneous label for group`,
		},
		{
			name: "bad locals block",
			hcl: `
locals {
	foo = "bar"
	more {
		bar = "10"
	}
}
`,
			errMsg: `test.hcl:4,2-6: Unexpected "more" block; Blocks are not allowed here`,
		},
		{
			name: "bad local ref",
			hcl: `
locals {
	foo = bar
}
`,
			errMsg: `test.hcl:3,8-11: reference to non-existent variable; bar`,
		},
		{
			name: "resource name clash",
			hcl: `
resource foo {
	body = {}
}

resource foo {
	body = {}
}
`,
			errMsg: `test.hcl:6,10-13: resource defined more than once; foo`,
		},
		{
			name: "collection name clash",
			hcl: `
resources foo {
	for_each = range(10)
	template {
		body = {}
	}
}

resources foo {
	for_each = range(10)
	template {
		body = {}
	}
}
`,
			errMsg: `test.hcl:9,11-14: resource collection defined more than once; foo`,
		},
		{
			name: "body expression ref to nonexistent local",
			hcl: `
resource foo {
	body = {
		bar = foo
	}
}
`,
			errMsg: `test.hcl:4,9-12: invalid local variable reference; foo`,
		},
		{
			name: "condition expression ref to nonexistent local",
			hcl: `
resource foo {
	condition = try(foo, false)
	body = {
		bar = "10"
	}
}
`,
			errMsg: `test.hcl:3,18-21: invalid local variable reference; foo`,
		},
		{
			name: "bad req index",
			hcl: `
resource foo {
	body = {
		bar = req[0]
	}
}
`,
			errMsg: `test.hcl:4,9-15: invalid index expression; req[0]`,
		},
		{
			name: "bad req second",
			hcl: `
resource foo {
	body = {
		bar = req.resources0.foo
	}
}
`,
			errMsg: `test.hcl:4,9-27: no such attribute "resources0"; req.resources0.foo`,
		},
		{
			name: "bad resource ref",
			hcl: `
locals {
	foo = req.resource.foo
}
`,
			errMsg: `test.hcl:3,8-24: invalid resource name reference; foo`,
		},
		{
			name: "bad resources ref",
			hcl: `
locals {
	foo = req.resources.foo
}
`,
			errMsg: `test.hcl:3,8-25: invalid resource collection name reference; foo`,
		},
		{
			name: "bad each ref",
			hcl: `
resources foo {
	for_each = range(10)
	template {
		body = {
			bar = each.foobar	
		}
	}
}
`,
			errMsg: `test.hcl:6,10-21: invalid each reference, must be one of 'key' or 'value'; foobar`,
		},
		{
			name: "bad for_each expr",
			hcl: `
resources foo {
	for_each = each.key
	template {
		body = {
			bar = "10"	
		}
	}
}
`,
			errMsg: `test.hcl:3,13-17: invalid local variable reference; each`,
		},
		{
			name: "bad name attribute",
			hcl: `
resources foo {
	for_each = range(10)
	name = "foo-${each.key}-${bar}" // note that this fails on bar but not on each.key
	template {
		body = {
			bar = "10"	
		}
	}
}
`,
			errMsg: `test.hcl:4,28-31: invalid local variable reference; bar`,
		},
		{
			name: "bad self name reference",
			hcl: `
resources foo {
	condition = self.basename == "foo" && self.name == "foo"
	for_each = range(10)
	template {
		body = {
			bar = "10"	
		}
	}
}
`,
			errMsg: `test.hcl:3,40-49: no such attribute "name"; self.name`,
		},
		{
			name: "duplicate requirements",
			hcl: `
	requirement valid {
		select {
			apiVersion = "v1"
			kind = "ConfigMap"
			matchName = "foo-bar"
		}
	}
	requirement valid {
		select {
			apiVersion = "v1"
			kind = "ConfigMap"
			matchName = "foo-bar"
		}
	}
`,
			errMsg: `test.hcl:9,14-19: requirement defined more than once; valid`,
		},
		{
			name: "bad requirement",
			hcl: `
	requirement foo {}
`,
			errMsg: `test.hcl:2,2-17: no select block in requirement; foo`,
		},
		{
			name: "multiple failures",
			hcl: `
resources foo {
	condition = self.basename == "foo" && self.name == "foo"
	for_each = range(count)
	template {
		body = {
			bar = bar
		}
	}
}
`,
			errMsg: `test.hcl:3,40-49: no such attribute "name"; self.name`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e, err := New(Options{})
			require.NoError(t, err)
			diags := e.Analyze(File{Name: "test.hcl", Content: test.hcl})
			require.True(t, diags.HasErrors())

			var errorMessages []string
			for _, diag := range diags {
				errorMessages = append(errorMessages, diag.Error())
			}
			assert.Contains(t, strings.Join(errorMessages, ", "), test.errMsg)
		})
	}
}
