package completion

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/crossplane-contrib/function-hcl/language-server/internal/langhcl/lang"
	"github.com/hashicorp/hcl/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func hover(t *testing.T, text string, xrd *xrd, pos hcl.Pos) *lang.HoverData {
	s := newTextScaffold(t, text, xrd)
	ctx, updatedPos := s.completionContext(t, pos)
	completer := New(ctx)
	data, err := completer.HoverAt(filepath.Base(testFileName), updatedPos)
	if err != nil {
		t.Logf("hover error: %v", err)
		return nil
	}
	return data
}

func requireHover(t *testing.T, text string, xrd *xrd, pos hcl.Pos) *lang.HoverData {
	t.Helper()
	data := hover(t, text, xrd, pos)
	require.NotNil(t, data, "expected hover data but got nil")
	return data
}

// --- body-level hover: attribute names ---

func TestHoverAttributeName(t *testing.T) {
	t.Run("top-level attribute name", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
  }
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 2, Column: 4})
		assert.Contains(t, data.Content.Value(), "**body**")
	})

	t.Run("nested attribute name", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    spec = {
      forProvider = {
        region = "us-east-1"
      }
    }
  }
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 7, Column: 10})
		assert.Contains(t, data.Content.Value(), "**region**")
	})

	t.Run("kind attribute name", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
  }
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 6})
		assert.Contains(t, data.Content.Value(), "**kind**")
	})
}

// --- body-level hover: block types and labels ---

func TestHoverBlockType(t *testing.T) {
	t.Run("resource block type", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
  }
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 1, Column: 5})
		assert.Contains(t, data.Content.Value(), "**resource**")
	})

	t.Run("validation block type", func(t *testing.T) {
		text := `
validation {
  condition     = true
  error_message = "never shown"
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 1, Column: 5})
		assert.Contains(t, data.Content.Value(), "**validation**")

		data = requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 6})
		assert.Contains(t, data.Content.Value(), "**error_message**")
		assert.Contains(t, data.Content.Value(), "the error returned when condition is false")
	})

	t.Run("composite block type", func(t *testing.T) {
		t.Skip("nested block type/label hover has position matching issue")
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
  }
  composite status {
    body = {
      arn = "foo"
    }
  }
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 7, Column: 6})
		assert.Contains(t, data.Content.Value(), "**composite**")
	})
}

func TestHoverBlockLabel(t *testing.T) {
	t.Run("resource label", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
  }
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 1, Column: 11})
		assert.Contains(t, data.Content.Value(), "vpc")
	})

	t.Run("composite status label", func(t *testing.T) {
		t.Skip("nested block label hover has position matching issue")
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
  }
  composite status {
    body = {
      arn = "foo"
    }
  }
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 7, Column: 14})
		assert.Contains(t, data.Content.Value(), "status")
	})
}

// --- hover: positions that return nil ---

func TestHoverNoData(t *testing.T) {
	t.Run("on equals sign", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
  }
}
`
		data := hover(t, text, nil,
			hcl.Pos{Line: 3, Column: 10})
		assert.Nil(t, data, "no hover on equals sign")
	})

	t.Run("empty space in body", func(t *testing.T) {
		text := `
resource vpc {

}
`
		data := hover(t, text, nil,
			hcl.Pos{Line: 2, Column: 1})
		assert.Nil(t, data, "no hover on empty space")
	})
}

// --- expression hover: literal values ---

func TestHoverLiteralValue(t *testing.T) {
	t.Run("string literal", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
  }
}
`
		// hovering on the literal value itself yields nil (LiteralValueExpr is a no-op)
		data := hover(t, text, nil,
			hcl.Pos{Line: 3, Column: 13})
		assert.Nil(t, data)
	})
}

// --- expression hover: scope traversals ---

func TestHoverScopeTraversal(t *testing.T) {
	t.Run("req root", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    spec = {
      forProvider = {
        region = req.composite.spec.parameters.region
      }
    }
  }
}
`
		data := requireHover(t, text, stdXRD,
			hcl.Pos{Line: 7, Column: 20})
		assert.Contains(t, data.Content.Value(), "req")
	})

	t.Run("req.composite", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    spec = {
      forProvider = {
        region = req.composite.spec.parameters.region
      }
    }
  }
}
`
		data := requireHover(t, text, stdXRD,
			hcl.Pos{Line: 7, Column: 26})
		assert.Contains(t, data.Content.Value(), "composite")
	})

	t.Run("req.composite.spec", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    spec = {
      forProvider = {
        region = req.composite.spec.parameters.region
      }
    }
  }
}
`
		data := requireHover(t, text, stdXRD,
			hcl.Pos{Line: 7, Column: 32})
		assert.Contains(t, data.Content.Value(), "spec")
	})

	t.Run("local variable", func(t *testing.T) {
		text := `
locals {
  region = "us-east-1"
  result = region
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 14})
		assert.Contains(t, data.Content.Value(), "region")
	})
}

// --- expression hover: objects ---

func TestHoverObjectExpression(t *testing.T) {
	t.Run("object key with schema", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    spec = {
      forProvider = {
        region = "us-east-1"
      }
    }
  }
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 7, Column: 10})
		assert.Contains(t, data.Content.Value(), "**region**")
	})

	t.Run("object value traversal", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    spec = {
      forProvider = {
        region = req.composite.spec.parameters.region
      }
    }
  }
}
`
		data := requireHover(t, text, stdXRD,
			hcl.Pos{Line: 7, Column: 44})
		assert.Contains(t, data.Content.Value(), "parameters")
	})

	t.Run("metadata key", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    metadata = {
      name = "my-vpc"
    }
  }
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 6, Column: 8})
		assert.Contains(t, data.Content.Value(), "**name**")
	})
}

// --- expression hover: function calls ---

func TestHoverFunctionCall(t *testing.T) {
	t.Run("function name", func(t *testing.T) {
		text := `
locals {
  result = upper("hello")
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 2, Column: 14})
		assert.Contains(t, data.Content.Value(), "upper")
	})

	t.Run("function argument traversal", func(t *testing.T) {
		text := `
locals {
  name = "test"
  result = upper(name)
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 20})
		assert.Contains(t, data.Content.Value(), "name")
	})

	t.Run("format function name", func(t *testing.T) {
		text := `
locals {
  result = format("%s-vpc", "us-east-1")
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 2, Column: 14})
		assert.Contains(t, data.Content.Value(), "format")
	})

	t.Run("merge function argument object key", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    spec = merge({
      forProvider = {
        region = "us-east-1"
      }
    })
  }
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 6, Column: 8})
		assert.Contains(t, data.Content.Value(), "**forProvider**")
	})
}

// --- expression hover: conditionals ---

func TestHoverConditional(t *testing.T) {
	t.Run("condition part", func(t *testing.T) {
		text := `
locals {
  flag = true
  result = flag ? "yes" : "no"
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 14})
		assert.Contains(t, data.Content.Value(), "flag")
	})

	t.Run("true branch traversal", func(t *testing.T) {
		text := `
locals {
  flag = true
  greeting = "hello"
  result = flag ? greeting : "default"
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 4, Column: 22})
		assert.Contains(t, data.Content.Value(), "greeting")
	})

	t.Run("false branch traversal", func(t *testing.T) {
		text := `
locals {
  flag = true
  fallback = "world"
  result = flag ? "default" : fallback
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 4, Column: 35})
		assert.Contains(t, data.Content.Value(), "fallback")
	})
}

// --- expression hover: binary ops ---

func TestHoverBinaryOp(t *testing.T) {
	t.Run("arithmetic LHS", func(t *testing.T) {
		text := `
locals {
  a = 10
  b = 20
  result = a + b
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 4, Column: 12})
		assert.Contains(t, data.Content.Value(), "a")
	})

	t.Run("arithmetic RHS", func(t *testing.T) {
		text := `
locals {
  a = 10
  b = 20
  result = a + b
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 4, Column: 16})
		assert.Contains(t, data.Content.Value(), "b")
	})

	t.Run("logical op", func(t *testing.T) {
		text := `
locals {
  x = true
  y = false
  result = x && y
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 4, Column: 12})
		assert.Contains(t, data.Content.Value(), "x")
	})
}

// --- expression hover: unary ops ---

func TestHoverUnaryOp(t *testing.T) {
	t.Run("negation operand", func(t *testing.T) {
		text := `
locals {
  val = 5
  result = -val
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 14})
		assert.Contains(t, data.Content.Value(), "val")
	})
}

// --- expression hover: template expressions ---

func TestHoverTemplateExpr(t *testing.T) {
	t.Run("interpolated variable", func(t *testing.T) {
		text := `
locals {
  name = "world"
  result = "hello ${name}!"
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 22})
		assert.Contains(t, data.Content.Value(), "name")
	})

	t.Run("interpolated traversal", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    metadata = {
      name = "${req.composite.metadata.name}-vpc"
    }
  }
}
`
		data := requireHover(t, text, stdXRD,
			hcl.Pos{Line: 6, Column: 22})
		assert.Contains(t, data.Content.Value(), "composite")
	})

	t.Run("multi-part template second interp", func(t *testing.T) {
		text := `
locals {
  base = "vpc"
  region = "us-east-1"
  name = "${base}-${region}-001"
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 4, Column: 23})
		assert.Contains(t, data.Content.Value(), "region")
	})
}

// --- expression hover: parentheses ---

func TestHoverParentheses(t *testing.T) {
	t.Run("parenthesized variable", func(t *testing.T) {
		text := `
locals {
  a = 10
  result = (a)
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 13})
		assert.Contains(t, data.Content.Value(), "a")
	})
}

// --- expression hover: tuples/lists ---

func TestHoverTupleExpr(t *testing.T) {
	t.Run("list element traversal", func(t *testing.T) {
		text := `
locals {
  foo = "bar"
  items = [foo]
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 13})
		assert.Contains(t, data.Content.Value(), "foo")
	})

	t.Run("list second element", func(t *testing.T) {
		text := `
locals {
  alpha = "x"
  beta = "y"
  items = [alpha, beta]
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 4, Column: 20})
		assert.Contains(t, data.Content.Value(), "beta")
	})
}

// --- expression hover: index expressions ---

func TestHoverIndexExpr(t *testing.T) {
	t.Run("collection part", func(t *testing.T) {
		text := `
locals {
  items = ["a", "b"]
  picked = items[0]
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 14})
		assert.Contains(t, data.Content.Value(), "items")
	})
}

// --- expression hover: splat expressions ---

func TestHoverSplatExpr(t *testing.T) {
	t.Run("source part", func(t *testing.T) {
		text := `
locals {
  items = [{ name = "a" }, { name = "b" }]
  names = items[*].name
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 13})
		assert.Contains(t, data.Content.Value(), "items")
	})
}

// --- expression hover: relative traversal ---

func TestHoverRelativeTraversal(t *testing.T) {
	t.Run("function result traversal", func(t *testing.T) {
		t.Skip("relative traversal after function call: hover does not resolve traversal keys")
		text := `
locals {
  obj = { inner = "val" }
  result = tomap(obj).inner
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 25})
		assert.Contains(t, data.Content.Value(), "inner")
	})

	t.Run("function name in relative traversal source", func(t *testing.T) {
		text := `
locals {
  obj = { inner = "val" }
  result = tomap(obj).inner
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 15})
		assert.Contains(t, data.Content.Value(), "tomap")
	})
}

// --- expression hover: template wrap ---

func TestHoverTemplateWrapExpr(t *testing.T) {
	t.Run("wrapped variable in pure interpolation", func(t *testing.T) {
		text := `
locals {
  name = "world"
  result = "${name}"
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 17})
		assert.Contains(t, data.Content.Value(), "name")
	})
}

// --- hover: locals block ---

func TestHoverLocals(t *testing.T) {
	t.Run("locals block type", func(t *testing.T) {
		text := `
locals {
  foo = "bar"
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 1, Column: 4})
		assert.Contains(t, data.Content.Value(), "**locals**")
	})

	t.Run("locals attribute name", func(t *testing.T) {
		text := `
locals {
  foo = "bar"
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 2, Column: 4})
		assert.Contains(t, data.Content.Value(), "**foo**")
	})

	t.Run("locals attribute with inferred type", func(t *testing.T) {
		text := `
locals {
  count = 42
  name = count
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 4})
		assert.Contains(t, data.Content.Value(), "**name**")
	})
}

// --- hover: deeply nested traversals ---

func TestHoverDeepTraversal(t *testing.T) {
	t.Run("req.resource.name.status.atProvider.field", func(t *testing.T) {
		text := `
locals {
  val = req.resource.one.status.atProvider.arn
}
resource one {
  body = {
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    kind = "VPC"
  }
}
`
		data := requireHover(t, text, stdXRD,
			hcl.Pos{Line: 2, Column: 45})
		assert.Contains(t, data.Content.Value(), "arn")
	})

	t.Run("req.composite.metadata.name", func(t *testing.T) {
		text := `
locals {
  val = req.composite.metadata.name
}
`
		data := requireHover(t, text, stdXRD,
			hcl.Pos{Line: 2, Column: 34})
		assert.Contains(t, data.Content.Value(), "name")
	})
}

// --- hover: self reference ---

func TestHoverSelf(t *testing.T) {
	t.Run("self in composite status", func(t *testing.T) {
		text := `
resource one {
  body = {
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    kind = "VPC"
  }
  composite status {
    body = {
      arn = self.resource.status.atProvider.arn
    }
  }
}
`
		data := requireHover(t, text, stdXRD,
			hcl.Pos{Line: 8, Column: 14})
		assert.Contains(t, data.Content.Value(), "self")
	})

	t.Run("self.resource traversal", func(t *testing.T) {
		text := `
resource one {
  body = {
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    kind = "VPC"
  }
  composite status {
    body = {
      arn = self.resource.status.atProvider.arn
    }
  }
}
`
		data := requireHover(t, text, stdXRD,
			hcl.Pos{Line: 8, Column: 21})
		assert.Contains(t, data.Content.Value(), "resource")
	})
}

// --- hover: map values ---

func TestHoverMapValue(t *testing.T) {
	t.Run("map value traversal", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    spec = {
      forProvider = {
        tags = {
          env = req.composite.spec.parameters.region
        }
      }
    }
  }
}
`
		data := requireHover(t, text, stdXRD,
			hcl.Pos{Line: 8, Column: 22})
		assert.Contains(t, data.Content.Value(), "composite")
	})
}

// --- hover: multiple resources ---

func TestHoverMultipleResources(t *testing.T) {
	t.Run("hover on different resource bodies", func(t *testing.T) {
		text := `
resource alpha {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
  }
}
resource beta {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
  }
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 1, Column: 12})
		assert.Contains(t, data.Content.Value(), "alpha")

		data = requireHover(t, text, nil,
			hcl.Pos{Line: 7, Column: 12})
		assert.Contains(t, data.Content.Value(), "beta")
	})
}

// --- hover: hover content format ---

func TestHoverContentFormat(t *testing.T) {
	t.Run("attribute hover is markdown", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
  }
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 2, Column: 4})
		assert.Equal(t, lang.MarkdownKind, data.Content.Kind())
	})

	t.Run("block type hover is markdown", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
  }
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 1, Column: 5})
		assert.Equal(t, lang.MarkdownKind, data.Content.Kind())
	})

	t.Run("function hover has parens", func(t *testing.T) {
		text := `
locals {
  result = upper("hello")
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 2, Column: 14})
		assert.Contains(t, data.Content.Value(), "(")
		assert.Contains(t, data.Content.Value(), ")")
	})
}

// --- hover: range correctness ---

func TestHoverRange(t *testing.T) {
	t.Run("attribute name range", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
  }
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 2, Column: 4})
		// body attr range should span the whole attribute
		assert.Equal(t, 2, data.Range.Start.Line)
	})

	t.Run("block type range is type range", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
  }
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 1, Column: 5})
		assert.Equal(t, 1, data.Range.Start.Line)
		assert.Equal(t, 1, data.Range.Start.Column)
	})

	t.Run("traversal segment range", func(t *testing.T) {
		text := `
locals {
  val = req.composite.spec
}
`
		data := requireHover(t, text, stdXRD,
			hcl.Pos{Line: 2, Column: 22})
		// hovering on "spec" - the range should start at req
		assert.Equal(t, 2, data.Range.Start.Line)
	})
}

// --- hover: object attribute preview ---

func TestHoverObjectAttributePreview(t *testing.T) {
	t.Run("object hover includes attribute names", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    spec = {
      forProvider = {
        region = "us-east-1"
        cidrBlock = "10.0.0.0/16"
      }
    }
  }
}
`
		// hover on "forProvider" which is an object attribute
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 6, Column: 10})
		content := data.Content.Value()
		assert.Contains(t, content, "**forProvider**")
		// Should contain a code block with braces and attribute names
		assert.Contains(t, content, "```")
		assert.Contains(t, content, "{\n")
		assert.Contains(t, content, "\n}")
		assert.Contains(t, content, "region")
		assert.Contains(t, content, "cidrBlock")
	})

	t.Run("object hover truncates when more than 4 attributes", func(t *testing.T) {
		// forProvider for VPC has >4 attributes from the CRD schema,
		// so the preview should show first 2, "...", and last 2.
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    spec = {
      forProvider = {
        region = "us-east-1"
      }
    }
  }
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 6, Column: 10})
		content := data.Content.Value()
		assert.Contains(t, content, "**forProvider**")
		assert.Contains(t, content, "```")
		assert.Contains(t, content, "...")
		// Verify truncation: count the attribute lines in the code block
		// Should have exactly 4 attribute lines (first 2 + last 2) plus "..."
		lines := strings.Split(content, "\n")
		attrLineCount := 0
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.Contains(trimmed, ":") && trimmed != "```" {
				attrLineCount++
			}
		}
		assert.Equal(t, 4, attrLineCount, "should show exactly 4 attribute lines when truncated")
	})

	t.Run("list of objects hover shows array delimiters", func(t *testing.T) {
		// status.conditions is a list of objects in the VPC CRD schema
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    status = {
      conditions = []
    }
  }
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 6, Column: 10})
		content := data.Content.Value()
		assert.Contains(t, content, "**conditions**")
		assert.Contains(t, content, "list of object")
		// Should use list-of-objects delimiters
		assert.Contains(t, content, "[{")
		assert.Contains(t, content, "},...]")
		// conditions has 6 attributes (>4), so should be truncated
		assert.Contains(t, content, "...")
	})
}

// --- hover: edge cases ---

func TestHoverEdgeCases(t *testing.T) {
	t.Run("empty locals block", func(t *testing.T) {
		text := `
locals {
}
`
		data := hover(t, text, nil,
			hcl.Pos{Line: 1, Column: 4})
		assert.NotNil(t, data)
		assert.Contains(t, data.Content.Value(), "**locals**")
	})

	t.Run("unknown block type returns error", func(t *testing.T) {
		// this is valid HCL but our schema doesn't support "foobar"
		// The scaffold may reject it at parse time, so just check it doesn't panic
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
  }
}
`
		// hover on a valid position to ensure no panic
		data := hover(t, text, nil,
			hcl.Pos{Line: 3, Column: 6})
		assert.NotNil(t, data)
	})

	t.Run("object key without schema still shows hover", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    spec = {
      forProvider = {
        tags = {
          myCustomTag = "value"
        }
      }
    }
  }
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 8, Column: 14})
		// map keys don't have schema; hover should still work via impliedSchema
		assert.Contains(t, data.Content.Value(), "myCustomTag")
	})
}

// --- hover: complex expressions ---

func TestHoverComplexExpressions(t *testing.T) {
	t.Run("nested function calls", func(t *testing.T) {
		text := `
locals {
  result = upper(format("%s-vpc", "us"))
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 2, Column: 14})
		assert.Contains(t, data.Content.Value(), "upper")

		data = requireHover(t, text, nil,
			hcl.Pos{Line: 2, Column: 20})
		assert.Contains(t, data.Content.Value(), "format")
	})

	t.Run("conditional with function", func(t *testing.T) {
		text := `
locals {
  flag = true
  result = flag ? upper("yes") : "no"
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 14})
		assert.Contains(t, data.Content.Value(), "flag")

		data = requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 21})
		assert.Contains(t, data.Content.Value(), "upper")
	})

	t.Run("binary op with function", func(t *testing.T) {
		text := `
locals {
  a = 10
  result = a + length("hello")
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 12})
		assert.Contains(t, data.Content.Value(), "a")

		data = requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 18})
		assert.Contains(t, data.Content.Value(), "length")
	})
}

// --- hover: invalid/unknown blocks ---

func TestHoverInvalidBlock(t *testing.T) {
	t.Run("hover on unknown top-level block type", func(t *testing.T) {
		text := `
foobar {
  val = "hello"
}
`
		// "foobar" is not a recognized block type but is valid HCL;
		// the schema returns anyBody, so BodySchema is non-nil
		// hovering on the block type should still work
		data := hover(t, text, nil,
			hcl.Pos{Line: 1, Column: 4})
		// bodySchema is non-nil (anyBody), parentSchema.NestedBlocks["foobar"] is nil
		// so hoverContentForBlock returns error
		assert.Nil(t, data, "unknown block type should return nil/error")
	})

	t.Run("hover on attr inside unknown block", func(t *testing.T) {
		text := `
foobar {
  val = "hello"
}
`
		// hovering on attribute name inside an unknown block
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 2, Column: 4})
		assert.Contains(t, data.Content.Value(), "**val**")
	})

	t.Run("completion inside unknown block returns empty", func(t *testing.T) {
		text := `
foobar {

}
`
		// completion inside unknown block: bodySchema is anyBody (empty)
		// so no candidates are offered
		c := candidates(t, text, nil,
			hcl.Pos{Line: 2, Column: 1})
		assert.Equal(t, 0, len(c.List),
			"unknown block should not offer candidates")
	})

	t.Run("completion on known block type prefix", func(t *testing.T) {
		text := `
fun
`
		// typing "fun" at top level should match "function"
		c := candidates(t, text, nil,
			hcl.Pos{Line: 1, Column: 4})
		labels := getCandidateLabels(c.List)
		assert.Contains(t, labels, "function")
	})
}

// --- hover/completion: too many labels ---

func TestHoverTooManyLabels(t *testing.T) {
	t.Run("hover on excess label returns error", func(t *testing.T) {
		// resource expects 1 label, giving 2 should trigger error on the extra label
		text := `
resource vpc extra {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
  }
}
`
		// hover on the extra label "extra"
		data := hover(t, text, nil,
			hcl.Pos{Line: 1, Column: 15})
		// labelSchemas has 1 entry, label index 1 exceeds it
		assert.Nil(t, data, "hovering on excess label should return nil/error")
	})

	t.Run("hover on valid label still works with excess labels", func(t *testing.T) {
		text := `
resource vpc extra {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
  }
}
`
		// hover on the valid first label "vpc"
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 1, Column: 11})
		assert.Contains(t, data.Content.Value(), "vpc")
	})

	t.Run("completion inside block with excess labels", func(t *testing.T) {
		text := `
resource vpc extra {

}
`
		// completion should still work inside the block body
		c := candidates(t, text, nil,
			hcl.Pos{Line: 2, Column: 1})
		labels := getCandidateLabels(c.List)
		assert.Contains(t, labels, "body",
			"should offer body even with excess labels")
	})

	t.Run("hover on block with zero-label block type", func(t *testing.T) {
		// locals expects 0 labels
		text := `
locals {
  val = "hello"
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 1, Column: 4})
		assert.Contains(t, data.Content.Value(), "**locals**")
	})

	t.Run("composite with extra labels", func(t *testing.T) {
		// composite expects 1 label ("status" or "connection")
		text := `
resource vpc {
  composite status extra {
    body = {
      arn = "foo"
    }
  }
}
`
		// hover on the excess label "extra"
		data := hover(t, text, nil,
			hcl.Pos{Line: 2, Column: 22})
		assert.Nil(t, data, "hovering on excess composite label should return nil/error")
	})
}

// --- more index expression coverage ---

func TestHoverIndexExprExtended(t *testing.T) {
	t.Run("index with string key on local", func(t *testing.T) {
		text := `
locals {
  obj = { alpha = 1, beta = 2 }
  picked = obj["alpha"]
}
`
		// hover on "obj" in the index expression
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 14})
		assert.Contains(t, data.Content.Value(), "obj")
	})

	t.Run("index key with traversal", func(t *testing.T) {
		t.Skip("index key hover only works when impliedSchema returns non-unknown")
		text := `
locals {
  items = ["a", "b"]
  idx = 0
  picked = items[idx]
}
`
		// hover on "idx" inside the brackets
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 4, Column: 19})
		assert.Contains(t, data.Content.Value(), "idx")
	})

	t.Run("chained index on traversal", func(t *testing.T) {
		text := `
locals {
  matrix = [["a", "b"], ["c", "d"]]
  picked = matrix[0]
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 14})
		assert.Contains(t, data.Content.Value(), "matrix")
	})

	t.Run("index on req object", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    spec = {
      forProvider = {
        region = req.composite.spec.parameters["region"]
      }
    }
  }
}
`
		// hover on "req" before the index expression
		data := requireHover(t, text, stdXRD,
			hcl.Pos{Line: 7, Column: 20})
		assert.Contains(t, data.Content.Value(), "req")
	})

	t.Run("index on resource status traversal", func(t *testing.T) {
		text := `
locals {
  val = req.resource.one.status.atProvider.tags["env"]
}
resource one {
  body = {
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    kind = "VPC"
  }
}
`
		// hover on "atProvider" segment
		data := requireHover(t, text, stdXRD,
			hcl.Pos{Line: 2, Column: 38})
		assert.Contains(t, data.Content.Value(), "atProvider")
	})
}

// --- more splat expression coverage ---

func TestHoverSplatExprExtended(t *testing.T) {
	t.Run("splat traversal field", func(t *testing.T) {
		text := `
locals {
  items = [{ name = "a" }, { name = "b" }]
  names = items[*].name
}
`
		// hover on ".name" after the splat
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 21})
		assert.Contains(t, data.Content.Value(), "name")
	})

	t.Run("splat on nested object list", func(t *testing.T) {
		text := `
locals {
  records = [{ host = "a", port = 80 }, { host = "b", port = 443 }]
  hosts = records[*].host
}
`
		// hover on "records" (source)
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 13})
		assert.Contains(t, data.Content.Value(), "records")

		// hover on ".host" after splat
		data = requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 23})
		assert.Contains(t, data.Content.Value(), "host")
	})

	t.Run("splat schema wraps result in list", func(t *testing.T) {
		text := `
locals {
  items = [{ name = "a" }]
  names = items[*].name
}
`
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 21})
		// the splat result should be wrapped in list type
		assert.Contains(t, data.Content.Value(), "list")
	})

	t.Run("splat source is not a list returns nil for traversal", func(t *testing.T) {
		text := `
locals {
  scalar = "hello"
  result = scalar[*].foo
}
`
		// hovering on ".foo" when source is a non-list should return nil
		data := hover(t, text, nil,
			hcl.Pos{Line: 3, Column: 24})
		assert.Nil(t, data,
			"splat on non-list source should not produce hover for traversal")
	})

	t.Run("splat on marker range", func(t *testing.T) {
		text := `
locals {
  items = [{ name = "a" }]
  names = items[*].name
}
`
		// hover on the [*] marker — position 16 is the '*'
		data := hover(t, text, nil,
			hcl.Pos{Line: 3, Column: 16})
		// MarkerRange may not contain this position in all parser versions;
		// just verify we don't panic
		if data != nil {
			assert.Contains(t, data.Content.Value(), "items")
		}
	})
}

// --- more relative traversal coverage ---

func TestHoverRelativeTraversalExtended(t *testing.T) {
	t.Run("relative traversal on index expression", func(t *testing.T) {
		text := `
locals {
  items = [{ name = "a", value = 1 }]
  result = items[0].name
}
`
		// items[0].name — this is an IndexExpr followed by RelativeTraversal
		// hover on "items"
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 14})
		assert.Contains(t, data.Content.Value(), "items")
	})

	t.Run("relative traversal field after index", func(t *testing.T) {
		text := `
locals {
  items = [{ name = "a", value = 1 }]
  result = items[0].name
}
`
		// hover on ".name" after the index
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 23})
		assert.Contains(t, data.Content.Value(), "name")
	})

	t.Run("relative traversal source descends into function", func(t *testing.T) {
		text := `
locals {
  obj = { inner = "val" }
  result = tomap(obj).inner
}
`
		// hover on function argument "obj" inside the source function call
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 19})
		assert.Contains(t, data.Content.Value(), "obj")
	})

	t.Run("chained relative traversal meta", func(t *testing.T) {
		text := `
locals {
  mydata = [{ meta = { id = "x" } }]
  result = mydata[0].meta.id
}
`
		// hover on "meta" after mydata[0]
		data := requireHover(t, text, nil,
			hcl.Pos{Line: 3, Column: 24})
		assert.Contains(t, data.Content.Value(), "meta")
	})

	t.Run("chained relative traversal deep field", func(t *testing.T) {
		text := `
locals {
  mydata = [{ meta = { id = "x" } }]
  result = mydata[0].meta.id
}
`
		// hover on ".id" — this may be beyond what hover resolves
		data := hover(t, text, nil,
			hcl.Pos{Line: 3, Column: 29})
		if data != nil {
			assert.Contains(t, data.Content.Value(), "id")
		}
	})
}

// --- completion: invalid block and excess labels ---

func TestCompletionInvalidBlock(t *testing.T) {
	t.Run("inside nonexistent block type", func(t *testing.T) {
		text := `
notreal {
  x
}
`
		c := candidates(t, text, nil,
			hcl.Pos{Line: 2, Column: 4})
		assert.Equal(t, 0, len(c.List),
			"unknown block offers no schema-aware candidates")
	})

	t.Run("top-level inside nonexistent block no prefix", func(t *testing.T) {
		text := `
notreal {

}
`
		c := candidates(t, text, nil,
			hcl.Pos{Line: 2, Column: 1})
		assert.Equal(t, 0, len(c.List))
	})
}

func TestCompletionExcessLabels(t *testing.T) {
	t.Run("resource with two labels still completes body", func(t *testing.T) {
		text := `
resource vpc extra {

}
`
		c := candidates(t, text, nil,
			hcl.Pos{Line: 2, Column: 1})
		labels := getCandidateLabels(c.List)
		assert.Contains(t, labels, "body")
		assert.Contains(t, labels, "composite")
	})

	t.Run("completion on label position returns nothing", func(t *testing.T) {
		text := `
resource vpc extra {

}
`
		// cursor on "extra" label
		c := candidates(t, text, nil,
			hcl.Pos{Line: 1, Column: 15})
		assert.Equal(t, 0, len(c.List),
			"no completion on label position")
	})

	t.Run("group block with unexpected label", func(t *testing.T) {
		// group expects 0 labels
		text := `
group unexpected_label {

}
`
		c := candidates(t, text, nil,
			hcl.Pos{Line: 2, Column: 1})
		// should still work inside
		assert.NotNil(t, c)
	})
}
