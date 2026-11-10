package completion

import (
	"log"
	"path/filepath"
	"sort"
	"testing"

	"github.com/crossplane-contrib/function-hcl/language-server/internal/langhcl/lang"
	"github.com/hashicorp/hcl/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var stdXRD = &xrd{
	APIVersion: "aws.example.com/v1alpha1",
	Kind:       "XAWSNetwork",
}

func candidates(t *testing.T, text string, xrd *xrd, pos hcl.Pos) lang.Candidates {
	s := newTextScaffold(t, text, xrd)
	ctx, updatedPos := s.completionContext(t, pos)
	completer := New(ctx)
	candidates, err := completer.CompletionAt(filepath.Base(testFileName), updatedPos)
	require.NoError(t, err)
	return candidates
}

func assertCandidateLabels(t *testing.T, candidates lang.Candidates, expected []string) {
	candidateLabels := getCandidateLabels(candidates.List)
	sort.Strings(candidateLabels)
	sort.Strings(expected)
	assert.EqualValues(t, expected, candidateLabels)
}

func expectCandidateLabels(t *testing.T, text string, xrd *xrd, pos hcl.Pos, expected []string) lang.Candidates {
	candidates := candidates(t, text, xrd, pos)
	assert.NotNil(t, candidates, "should return candidates")
	assert.True(t, candidates.IsComplete, "should have complete list of candidates")
	assertCandidateLabels(t, candidates, expected)
	return candidates
}

func TestCompletionEmptyFile(t *testing.T) {
	candidates := expectCandidateLabels(t, "", nil,
		hcl.Pos{Line: 1, Column: 1},
		[]string{"composite", "context", "function", "group", "locals", "requirement", "resource", "resources", "validation"},
	)
	for _, c := range candidates.List {
		assert.Equal(t, lang.BlockCandidateKind, c.Kind,
			"top-level candidates should be blocks")
		assert.NotEmpty(t, c.TextEdit.NewText,
			"candidate should have text to insert")
	}
}

func TestCompletionAttrsUnderTopLevel(t *testing.T) {
	t.Run("no prefix", func(t *testing.T) {
		text := `
resource vpc {

}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 2, Column: 1},
			[]string{"body", "composite", "condition", "context", "locals", "ready"})
	})

	t.Run("with prefix", func(t *testing.T) {
		text := `
resource vpc {
  c
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 2, Column: 4},
			[]string{"composite", "condition", "context"})
	})

	t.Run("with prefix in name range", func(t *testing.T) {
		text := `
resource vpc {
  con
}
`
		candidates := expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 2, Column: 4},
			[]string{"condition", "context"})
		expectedRange := hcl.Range{
			Filename: testFileName,
			Start:    hcl.Pos{Line: 2, Column: 3, Byte: 17},
			End:      hcl.Pos{Line: 2, Column: 6, Byte: 20},
		}
		assert.EqualValues(t, expectedRange, candidates.List[0].TextEdit.Range)
	})

	t.Run("with prefix in name range with valid hcl", func(t *testing.T) {
		text := `
resource vpc {
  con = "foo"
}
`
		candidates := expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 2, Column: 5},
			[]string{"composite", "condition", "context"},
		) // includes composite since cursor is after co
		// should fully edit the prefix and the whole freaking line
		expectedRange := hcl.Range{
			Filename: testFileName,
			Start:    hcl.Pos{Line: 2, Column: 3, Byte: 17},
			End:      hcl.Pos{Line: 2, Column: 14, Byte: 28},
		}
		assert.EqualValues(t, expectedRange, candidates.List[0].TextEdit.Range)
	})

	t.Run("with prefix at end of name range with valid hcl", func(t *testing.T) {
		text := `
resource vpc {
  con = "foo"
}
`
		candidates := expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 2, Column: 6},
			[]string{"condition", "context"},
		) // includes composite since cursor is after co
		// should fully edit the prefix and the whole freaking line
		expectedRange := hcl.Range{
			Filename: testFileName,
			Start:    hcl.Pos{Line: 2, Column: 3, Byte: 17},
			End:      hcl.Pos{Line: 2, Column: 14, Byte: 28},
		}
		assert.EqualValues(t, expectedRange, candidates.List[0].TextEdit.Range)
	})
}

func TestCompletionUnderValidation(t *testing.T) {
	t.Run("top level", func(t *testing.T) {
		text := `
validation {

}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 2, Column: 1},
			[]string{"condition", "error_message"})
	})

	t.Run("in a group", func(t *testing.T) {
		text := `
group {
  validation {

  }
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 3, Column: 1},
			[]string{"condition", "error_message"})
	})
}

func TestBlockCompletion(t *testing.T) {
	t.Run("resource", func(t *testing.T) {
		text := `
resource `
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 1, Column: 5},
			[]string{"resource", "resources"},
		)
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 1, Column: 8},
			[]string{"resource", "resources"},
		)
		// the following cases do not work, figure out why
		t.Skip("skip failing tests")
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 1, Column: 9},
			[]string{"resource", "resources"},
		)
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 1, Column: 10},
			[]string{},
		)
	})
}

func TestCompletionUnderBodyAttr(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		text := `
resource vpc {
  body = {

  }
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 3, Column: 1},
			[]string{"apiVersion", "kind", "metadata"},
		)
	})

	t.Run("with prefix", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    k
  }
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 3, Column: 6},
			[]string{"kind"})
	})

	t.Run("with kind", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind =
  }
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 3, Column: 11},
			[]string{`"VPC"`, `"XAWSNetwork"`},
		)
	})

	t.Run("with known kind", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion =
  }
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 4, Column: 17},
			[]string{`"ec2.aws.upbound.io/v1beta1"`},
		)
	})

	t.Run("with api version", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    apiVersion =
  }
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 3, Column: 17},
			[]string{`"aws.example.com/v1alpha1"`, `"ec2.aws.upbound.io/v1beta1"`},
		)
	})

	t.Run("with known api version", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    apiVersion = "aws.example.com/v1alpha1"
    kind =
  }
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 4, Column: 11},
			[]string{`"XAWSNetwork"`},
		)
	})

	t.Run("with known schema", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    
  }
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 5, Column: 5},
			[]string{"metadata", "spec"},
		)
	})

	t.Run("with spec", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    spec = 
  }
}
`
		candidates := expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 5, Column: 12},
			[]string{`{…}`},
		)
		assert.True(t, candidates.List[0].TriggerSuggest)
		assert.EqualValues(t, "{\n  \n}", candidates.List[0].TextEdit.NewText)
		log.Printf("%+v\n", candidates.List)
	})

	t.Run("inner attribute under spec", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    spec = {
      forProvider = {

      }
    }
  }
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 7, Column: 1},
			[]string{
				"assignGeneratedIpv6CidrBlock", "cidrBlock", "enableDnsHostnames", "enableDnsSupport",
				"enableNetworkAddressUsageMetrics", "instanceTenancy", "ipv4IpamPoolId", "ipv4IpamPoolIdRef",
				"ipv4IpamPoolIdSelector", "ipv4NetmaskLength", "ipv6CidrBlock", "ipv6CidrBlockNetworkBorderGroup",
				"ipv6IpamPoolId", "ipv6NetmaskLength", "region", "tags",
			},
		)
	})

	t.Run("inner attribute under spec with prefix", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    spec = {
      forProvider = {
        i
      }
    }
  }
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 7, Column: 10},
			[]string{
				"instanceTenancy", "ipv4IpamPoolId", "ipv4IpamPoolIdRef", "ipv4IpamPoolIdSelector",
				"ipv4NetmaskLength", "ipv6CidrBlock", "ipv6CidrBlockNetworkBorderGroup", "ipv6IpamPoolId",
				"ipv6NetmaskLength",
			},
		)
	})

	t.Run("inner attribute value", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    spec = {
      forProvider = {
        enableDnsHostnames =
      }
    }
  }
}
`
		candidates := candidates(t, text, nil,
			hcl.Pos{Line: 7, Column: 29})
		assert.NotNil(t, candidates, "should return candidates")
		c1, c2 := splitNonFunc(candidates)
		assert.True(t, candidates.IsComplete, "should have an incomplete list of candidates")
		assert.Greater(t, len(c1), 30)
		c2Labels := getCandidateLabels(c2)
		sort.Strings(c2Labels)
		assert.EqualValues(t, []string{"false", "req", "self", "true"}, c2Labels)
	})

	t.Run("inner attribute value with prefix", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    spec = {
      forProvider = {
        enableDnsHostnames = re
      }
    }
  }
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 7, Column: 32},
			[]string{"req", "regex", "regexall", "replace", "reverse"},
		)
	})

	t.Run("inner attribute interpolated string with prefix", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    spec = {
      forProvider = {
        enableDnsHostnames = "${re}"
      }
    }
  }
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 7, Column: 35},
			[]string{"req", "regex", "regexall", "replace", "reverse"},
		)
	})

	t.Run("basic locals completion", func(t *testing.T) {
		text := `
locals {
  foo = 10
  bar = f
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 3, Column: 10},
			[]string{"foo", "flatten", "floor", "format", "formatdate", "formatlist"},
		)
	})

	t.Run("map attribute", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    spec = {
      forProvider = {
        tags = {
          environment = req.
        }
      }
    }
  }
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 8, Column: 29},
			[]string{"composite", "composite_connection", "connection", "context", "resource"},
		)
	})
}

func TestCompletionInInterpolatedStrings(t *testing.T) {
	t.Run("string interpolated provides completion", func(t *testing.T) {
		text := `
locals {
  baseName = "my-base-name"
  vpcName = "${b}"
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 3, Column: 17},
			[]string{"base64decode", "base64encode", "base64gzip", "base64sha256", "base64sha512", "baseName"},
		)
	})

	t.Run("string interpolated multi-part completion", func(t *testing.T) {
		text := `
locals {
  baseName = "vpc"
  region = "us-east-1"
  name = "${baseName}-${r}-001"
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 4, Column: 26},
			[]string{"range", "regex", "regexall", "region", "replace", "req", "reverse", "rsadecrypt"},
		)
	})

	t.Run("string interpolated dot recovery", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    metadata = {
      name = "${req.composite.}"
    }
  }
}
`
		expectCandidateLabels(t, text, stdXRD,
			hcl.Pos{Line: 6, Column: 31},
			[]string{"metadata", "spec", "status"},
		)
	})

	t.Run("function in interpolation", func(t *testing.T) {
		text := `
locals {
  result = "${upper(}"
}
`
		t.Skip("this test does not work, fix the code")
		expectCandidateLabels(t, text, stdXRD,
			hcl.Pos{Line: 2, Column: 21},
			[]string{"xxx"},
		)
	})

	t.Run("multiple objects in interpolation", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    metadata = {
      annotations = {
        description = "VPC for ${req.composite.metadata.name} in region ${req.}"
      }
    }
  }
}
`
		expectCandidateLabels(t, text, stdXRD,
			hcl.Pos{Line: 7, Column: 79},
			[]string{"composite", "composite_connection", "connection", "context", "resource"},
		)
	})
}

func TestReqObjectCompletion(t *testing.T) {
	t.Run("req dot completion", func(t *testing.T) {
		text := `
locals {
  foo = req.
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 2, Column: 13},
			[]string{"composite", "composite_connection", "context"},
		)
	})

	t.Run("req dot completion when resource present", func(t *testing.T) {
		text := `
locals {
  foo = req.
}

resource vpc {
  body = {

  }
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 2, Column: 13},
			[]string{"resource", "connection", "composite", "composite_connection", "context"},
		)
	})

	t.Run("composite completion for XR", func(t *testing.T) {
		text := `
locals {
  foo = req.composite.
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 2, Column: 23},
			[]string{},
		)
	})
	t.Run("composite completion for XR with XRD", func(t *testing.T) {
		text := `
locals {
  foo = req.composite.
}
`
		expectCandidateLabels(t, text, stdXRD,
			hcl.Pos{Line: 2, Column: 23},
			[]string{"metadata", "spec", "status"},
		)
	})

	t.Run("composite sub attrs propagate", func(t *testing.T) {
		text := `
locals {
  spec = req.composite.spec
  params = spec.
}
`
		expectCandidateLabels(t, text, stdXRD,
			hcl.Pos{Line: 3, Column: 17},
			[]string{"accountConfig", "parameters"},
		)
	})

	t.Run("resource name completion", func(t *testing.T) {
		text := `
locals {
  status = req.resource.
}
resource one {}
resource two {}
resource three {}

`
		expectCandidateLabels(t, text, stdXRD,
			hcl.Pos{Line: 2, Column: 25},
			[]string{"one", "three", "two"},
		)
	})

	t.Run("resource status-only completion", func(t *testing.T) {
		text := `
locals {
  status = req.resource.one.
}
resource one {
  body = {
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    kind = "VPC"
  }
}
`
		expectCandidateLabels(t, text, stdXRD,
			hcl.Pos{Line: 2, Column: 29},
			[]string{"status"},
		)
	})

	t.Run("resource status at-provider completion", func(t *testing.T) {
		text := `
locals {
  status = req.resource.one.status.atProvider.
}

resource one {
  body = {
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    kind = "VPC"
  }
}
`
		expectCandidateLabels(t, text, stdXRD,
			hcl.Pos{Line: 2, Column: 47},
			[]string{
				"arn", "assignGeneratedIpv6CidrBlock", "cidrBlock", "defaultNetworkAclId", "defaultRouteTableId",
				"defaultSecurityGroupId", "dhcpOptionsId", "enableDnsHostnames", "enableDnsSupport",
				"enableNetworkAddressUsageMetrics", "id", "instanceTenancy", "ipv4IpamPoolId", "ipv4NetmaskLength",
				"ipv6AssociationId", "ipv6CidrBlock", "ipv6CidrBlockNetworkBorderGroup", "ipv6IpamPoolId",
				"ipv6NetmaskLength", "mainRouteTableId", "ownerId", "tags", "tagsAll",
			},
		)
	})

	t.Run("self completion", func(t *testing.T) {
		text := `
resource one {
  body = {
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    kind = "VPC"
  }
  composite status {
    body = {
      arn = s
    }
  }
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 8, Column: 14},
			[]string{
				"self", "setintersection", "setproduct", "setsubtract", "setunion", "sha1", "sha256", "sha512",
				"signum", "slice", "sort", "split", "startswith", "strcontains", "strrev", "substr", "sum",
			},
		)
	})

	t.Run("self completion 2", func(t *testing.T) {
		text := `
resource one {
  body = {
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    kind = "VPC"
  }
  composite status {
    body = {
      arn = self.
    }
  }
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 8, Column: 18},
			[]string{"connection", "name", "resource"},
		)
	})

	t.Run("self completion 3", func(t *testing.T) {
		text := `
resource one {
  body = {
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    kind = "VPC"
  }
  composite status {
    body = {
      arn = self.resource.status.
    }
  }
}
`
		expectCandidateLabels(t, text, stdXRD,
			hcl.Pos{Line: 8, Column: 34},
			[]string{"atProvider", "conditions", "observedGeneration"},
		)
	})
}

func TestSpecialCases(t *testing.T) {
	t.Run("inner attribute merge function param", func(t *testing.T) {
		text := `
resource vpc {
  body = {
    kind = "VPC"
    apiVersion = "ec2.aws.upbound.io/v1beta1"
    spec = merge({
      forProvider = {
        enableDnsHostnames = re
      }
    })
  }
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 7, Column: 32},
			[]string{"req", "regex", "regexall", "replace", "reverse"},
		)
	})

	t.Run("unicode chars", func(t *testing.T) {
		text := `
locals {
  日本語 = "value"
  result = 日
}
`
		expectCandidateLabels(t, text, nil,
			hcl.Pos{Line: 3, Column: 13},
			[]string{"日本語"},
		)
	})
}

func splitNonFunc(candidates lang.Candidates) (c1 []lang.Candidate, c2 []lang.Candidate) {
	for _, c := range candidates.List {
		if c.Kind == lang.FunctionCandidateKind {
			c1 = append(c1, c)
		} else {
			c2 = append(c2, c)
		}
	}
	return
}
