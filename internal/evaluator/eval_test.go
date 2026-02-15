package evaluator_test

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/crossplane-contrib/function-hcl/internal/evaluator"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/simple.json
var baseRequestJSON string

//go:embed testdata/multi.json
var mutiRequestJSON string

func baseRequest(t *testing.T, data string) *fnv1.RunFunctionRequest {
	var ret fnv1.RunFunctionRequest
	err := json.Unmarshal([]byte(data), &ret)
	require.NoError(t, err)
	return &ret
}

func makeRequest(t *testing.T, data string, fns ...func(request *fnv1.RunFunctionRequest)) *fnv1.RunFunctionRequest {
	req := baseRequest(t, data)
	for _, fn := range fns {
		if fn == nil {
			continue
		}
		fn(req)
	}
	return req
}

type testCase struct {
	name     string
	hcl      string
	request  string
	fn       func(request *fnv1.RunFunctionRequest)
	asserter func(t *testing.T, res *fnv1.RunFunctionResponse, err error)
}

func mustFile(t *testing.T, path string) string {
	f := filepath.Join("testdata", path)
	b, err := os.ReadFile(f)
	require.NoError(t, err)
	return string(b)
}

func logResult(t *testing.T, res *fnv1.RunFunctionResponse) {
	b, err := json.MarshalIndent(res, "", "  ")
	t.Log(string(b))
	require.NoError(t, err)
}

func TestPositiveEval(t *testing.T) {
	tests := []testCase{
		{
			name: "one resource",
			hcl:  mustFile(t, "simple.hcl"),
			asserter: func(t *testing.T, res *fnv1.RunFunctionResponse, err error) {
				assert.Equal(t, 2, len(res.Desired.Resources))
				assert.Contains(t, res.Desired.Resources, "primary-bucket")
				assert.Contains(t, res.Desired.Resources, "secondary-bucket")

				pb := res.Desired.Resources["primary-bucket"]
				obj := pb.GetResource().AsMap()
				md := obj["metadata"]
				mdObj, ok := md.(map[string]any)
				require.True(t, ok)
				labels := mdObj["labels"]
				labelsObj, ok := labels.(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "bar", labelsObj["foo"])
				_, ok = labelsObj["baz"]
				assert.False(t, ok)
			},
		},
		{
			name:    "multiple resources",
			hcl:     mustFile(t, "multi.hcl"),
			request: mutiRequestJSON,
			asserter: func(t *testing.T, res *fnv1.RunFunctionResponse, err error) {
				assert.Equal(t, 3, len(res.Desired.Resources))
				assert.Contains(t, res.Desired.Resources, "cm-config-map-foo")
				assert.Contains(t, res.Desired.Resources, "cm-config-map-bar")
				assert.Contains(t, res.Desired.Resources, "cm-config-map-baz")
			},
		},
		{
			name: "incomplete locals allowed",
			hcl: `
				locals {
				  foo = "${req.resources.primary_bucket.status.arn}"
				  bar = "${foo} ARN"
				}
			`,
		},
		{
			name: "incomplete conditions allowed",
			hcl: `
				locals {
				  foo = "${req.resources.primary_bucket.status.arn}"
				}
				group {
				  condition = can(foo)
				}
			`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := test.request
			if base == "" {
				base = baseRequestJSON
			}
			req := makeRequest(t, base, test.fn)
			e, err := evaluator.New(evaluator.Options{})
			require.NoError(t, err)

			res, err := e.Eval(req, evaluator.File{
				Name:    "main.hcl",
				Content: test.hcl,
			})
			require.NoError(t, err)
			logResult(t, res)
			if test.asserter != nil {
				test.asserter(t, res, err)
			}
		})
	}
}

func TestNegativeEval(t *testing.T) {
	tests := []testCase{
		{
			name: "3-part cycle",
			hcl: `
				locals {
				  foo = "${bar}x"
				  bar = "${baz}y"
				  baz = foo
				}
			`,
			asserter: func(t *testing.T, res *fnv1.RunFunctionResponse, err error) {
				assert.Contains(t, err.Error(), "cycle")
			},
		},
		{
			name: "self-ref",
			hcl: `
				locals {
					foo = "${foo}bar"
				}
			`,
			asserter: func(t *testing.T, res *fnv1.RunFunctionResponse, err error) {
				assert.Contains(t, err.Error(), "cycle")
			},
		},
		{
			name: "duplicate local",
			hcl: `
				locals {
				  foo = "${req.resource.primary_bucket.status.arn}"
				}
				locals {
					foo = "bar"
				}
			`,
			asserter: func(t *testing.T, res *fnv1.RunFunctionResponse, err error) {
				assert.Contains(t, err.Error(), "duplicate")
			},
		},
		{
			name: "local has blocks",
			hcl: `
				locals {
				  	foo = "${req.resources.primary_bucket.status.arn}"
					bar {
						baz = true
					}
				}
			`,
			asserter: func(t *testing.T, res *fnv1.RunFunctionResponse, err error) {
				assert.Contains(t, err.Error(), "Blocks are not allowed here")
			},
		},
		{
			name: "bad local ref",
			hcl: `
				locals {
				  	foo = "${bar}"
				}
			`,
			asserter: func(t *testing.T, res *fnv1.RunFunctionResponse, err error) {
				assert.Contains(t, err.Error(), `reference to non-existent variable; bar`)
			},
		},
		{
			name: "reserved word",
			hcl: `
				locals {
				  	req = "foo"
				}
			`,
			asserter: func(t *testing.T, res *fnv1.RunFunctionResponse, err error) {
				assert.Contains(t, err.Error(), `attempt to shadow variable; req`)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := test.request
			if base == "" {
				base = baseRequestJSON
			}
			req := makeRequest(t, base, test.fn)
			e, err := evaluator.New(evaluator.Options{})
			require.NoError(t, err)

			res, err := e.Eval(req, evaluator.File{
				Name:    "main.hcl",
				Content: test.hcl,
			})
			require.Error(t, err)

			if test.asserter != nil {
				test.asserter(t, res, err)
			}
		})
	}
}
