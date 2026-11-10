package evaluator_test

import (
	"strings"
	"testing"

	"github.com/crossplane-contrib/function-hcl/function/internal/evaluator"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validationBucket = `
resource primary-bucket {
  body = {
    apiVersion = "aws.com/v1"
    kind       = "S3Bucket"
    metadata   = { name = "primary" }
  }
}
`

// subnetBucket reads an input that the base request doesn't have, so evaluating it fails.
const subnetBucket = `
resource primary-bucket {
  body = {
    apiVersion = "aws.com/v1"
    kind       = "S3Bucket"
    metadata   = { name = "primary" }
    spec       = { subnet = req.composite.spec.parameters.subnets[0] }
  }
}
`

// noObservedResources turns the base request, which observes primary-bucket, into a new XR.
func noObservedResources(req *fnv1.RunFunctionRequest) {
	req.Observed.Resources = nil
}

func TestValidation(t *testing.T) {
	tests := []struct {
		name        string
		hcl         string
		more        []evaluator.File // files evaluated after main.hcl
		fn          func(*fnv1.RunFunctionRequest)
		errSubstr   string // empty when the render must succeed
		resources   []string
		warnSubstrs []string // expected in the results of a render that succeeds
	}{
		{
			name: "condition true",
			hcl: `
				validation {
				  condition     = req.composite.spec.parameters.region == "us-east-1"
				  error_message = "region must be us-east-1"
				}
			` + validationBucket,
			resources: []string{"primary-bucket"},
		},
		{
			name: "condition false, new XR",
			hcl: `
				validation {
				  condition     = length(try(req.composite.spec.parameters.subnets, [])) > 0
				  error_message = "subnets must list at least one subnet"
				}
			` + validationBucket,
			fn:        noObservedResources,
			errSubstr: "validation failed; subnets must list at least one subnet",
		},
		{
			name: "condition false, existing XR",
			hcl: `
				validation {
				  condition     = req.composite.spec.parameters.region == "eu-west-1"
				  error_message = "region must be eu-west-1"
				}
			` + validationBucket,
			errSubstr: "validation failed; region must be eu-west-1",
		},
		{
			name: "condition not known yet",
			hcl: `
				validation {
				  condition     = req.resource["primary-bucket"].status.atProvider.arn != ""
				  error_message = "the bucket has no ARN"
				}
			` + validationBucket,
			fn:        noObservedResources,
			resources: []string{"primary-bucket"},
		},
		{
			name: "condition not a bool",
			hcl: `
				validation {
				  condition     = "yes"
				  error_message = "never shown"
				}
			`,
			errSubstr: "validation condition must be a bool; got string",
		},
		{
			name: "condition not a bool and not known yet",
			hcl: `
				locals {
				  arn = req.resource["primary-bucket"].status.atProvider.arn
				}
				validation {
				  condition     = [arn]
				  error_message = "never shown"
				}
			` + validationBucket,
			fn:        noObservedResources,
			errSubstr: "validation condition must be a bool; got tuple",
		},
		{
			name: "condition not a bool, with an evaluation error",
			hcl: `
				validation {
				  condition     = [req.resource["primary-bucket"].status.atProvider.arn]
				  error_message = "never shown"
				}
			` + validationBucket,
			fn:        noObservedResources,
			errSubstr: "validation condition must be a bool; got tuple",
		},
		{
			name: "condition true, error_message not a string, with an evaluation error",
			hcl: `
				validation {
				  condition     = true
				  error_message = [req.composite.spec.parameters.missing]
				}
			` + validationBucket,
			errSubstr: "validation error_message must be a string; got tuple",
		},
		{
			name: "condition not known yet, error_message not a string",
			hcl: `
				validation {
				  condition     = req.resource["primary-bucket"].status.atProvider.arn != ""
				  error_message = 123
				}
			` + validationBucket,
			fn:        noObservedResources,
			errSubstr: "validation error_message must be a string; got number",
		},
		{
			name: "condition true, error_message not a string",
			hcl: `
				validation {
				  condition     = true
				  error_message = 123
				}
			` + validationBucket,
			errSubstr: "validation error_message must be a string; got number",
		},
		{
			name: "condition true, error_message reads a missing attribute",
			hcl: `
				validation {
				  condition     = !can(req.composite.spec.parameters.zone)
				  error_message = "zone ${req.composite.spec.parameters.zone} is not allowed"
				}
			` + validationBucket,
			resources: []string{"primary-bucket"},
		},
		{
			name: "condition false, error_message reads a missing attribute",
			hcl: `
				validation {
				  condition     = can(req.composite.spec.parameters.zone)
				  error_message = "zone ${req.composite.spec.parameters.zone} is required"
				}
			` + validationBucket,
			errSubstr: "validation failed; error_message could not be evaluated: ",
		},
		{
			name: "existing XR, a resource before the block reads the invalid input",
			hcl: subnetBucket + `
				validation {
				  condition     = length(try(req.composite.spec.parameters.subnets, [])) > 0
				  error_message = "subnets must list at least one subnet"
				}
			`,
			errSubstr: "validation failed; subnets must list at least one subnet",
		},
		{
			name: "existing XR, a resource in an earlier file reads the invalid input",
			hcl:  subnetBucket,
			more: []evaluator.File{{Name: "validation.hcl", Content: `
				validation {
				  condition     = length(try(req.composite.spec.parameters.subnets, [])) > 0
				  error_message = "subnets must list at least one subnet"
				}
			`}},
			errSubstr: "validation.hcl:3,23-81: validation failed; subnets must list at least one subnet",
		},
		{
			name: "existing XR, the block is in a group after a resource that reads the invalid input",
			hcl: subnetBucket + `
				group {
				  validation {
				    condition     = length(try(req.composite.spec.parameters.subnets, [])) > 0
				    error_message = "subnets must list at least one subnet"
				  }
				}
			`,
			errSubstr: "validation failed; subnets must list at least one subnet",
		},
		{
			name: "two failing blocks, the first one is reported",
			hcl: `
				validation {
				  condition     = false
				  error_message = "first"
				}
				validation {
				  condition     = false
				  error_message = "second"
				}
			`,
			errSubstr: "validation failed; first",
		},
		{
			name: "condition not known yet, reported as a warning",
			hcl: `
				validation {
				  condition     = req.resource["primary-bucket"].status.atProvider.arn != ""
				  error_message = "the bucket has no ARN"
				}
			` + validationBucket,
			fn:          noObservedResources,
			resources:   []string{"primary-bucket"},
			warnSubstrs: []string{"discarded validation condition"},
		},
		{
			name: "inside a group whose condition is not known yet",
			hcl: `
				group {
				  condition = req.resource["primary-bucket"].status.atProvider.arn != ""
				  validation {
				    condition     = false
				    error_message = "never shown"
				  }
				}
			` + validationBucket,
			fn:          noObservedResources,
			resources:   []string{"primary-bucket"},
			warnSubstrs: []string{"discarded group"},
		},
		{
			name: "inside a group that is skipped",
			hcl: `
				group {
				  condition = false
				  validation {
				    condition     = false
				    error_message = "never shown"
				  }
				}
			` + validationBucket,
			resources: []string{"primary-bucket"},
		},
		{
			name: "error_message missing",
			hcl: `
				validation {
				  condition = true
				}
			`,
			errSubstr: `Missing required argument; The argument "error_message" is required`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := makeRequest(t, baseRequestJSON, test.fn)
			e, err := evaluator.New(evaluator.Options{})
			require.NoError(t, err)

			files := append([]evaluator.File{{Name: "main.hcl", Content: test.hcl}}, test.more...)
			res, err := e.Eval(req, files...)
			if test.errSubstr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.errSubstr)
				return
			}
			require.NoError(t, err)
			for _, name := range test.resources {
				assert.Contains(t, res.Desired.Resources, name)
			}
			var results []string
			for _, r := range res.Results {
				results = append(results, r.GetMessage())
			}
			for _, want := range test.warnSubstrs {
				assert.Contains(t, strings.Join(results, "\n"), want)
			}
		})
	}
}
