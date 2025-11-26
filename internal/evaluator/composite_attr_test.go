package evaluator

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluatorAttr_ProcessComposite_Status(t *testing.T) {
	hclContent := `
resource "database" {
  body = {
    apiVersion = "postgresql.cnpg.io/v1"
    kind       = "Cluster"
    metadata = {
      name = "my-db"
    }
  }
  
  composite "status" {
	body = {
      database_ready = true
	  connection_host = "my-db.default.svc.cluster.local"
	  replica_count = 3
	}
  }
}
`

	evaluator := createTestEvaluator(t)
	ctx := createTestEvalContext()
	content := parseHCL(t, evaluator, hclContent, "test.hcl")

	diags := evaluator.processGroup(ctx, content)
	require.Empty(t, diags)

	// verify resource was created
	assert.Contains(t, evaluator.desiredResources, "database")

	// verify composite status was set
	assert.Len(t, evaluator.compositeStatuses, 1)
	status := evaluator.compositeStatuses[0]

	assert.Equal(t, true, status["database_ready"])
	assert.Equal(t, "my-db.default.svc.cluster.local", status["connection_host"])
	assert.EqualValues(t, 3, status["replica_count"])
}

func TestEvaluatorAttr_ProcessComposite_StatusWithLocals(t *testing.T) {
	hclContent := `
resource "web-app" {
  body = {
    apiVersion = "apps/v1"
    kind       = "Deployment"
    metadata = {
      name = "webapp"
    }
  }
  
  composite "status" {
    locals {
      ready_replicas = 2
      total_replicas = 3
      readiness_percentage = (ready_replicas / total_replicas) * 100
    }
    body = {
      deployment_ready = ready_replicas == total_replicas
	  readiness_percent = readiness_percentage
	  endpoint_url = "https://webapp.example.com"
	}
  }
}
`

	evaluator := createTestEvaluator(t)
	ctx := createTestEvalContext()
	content := parseHCL(t, evaluator, hclContent, "test.hcl")

	diags := evaluator.processGroup(ctx, content)
	require.Empty(t, diags)

	// verify composite status with computed values
	assert.Len(t, evaluator.compositeStatuses, 1)
	status := evaluator.compositeStatuses[0]

	assert.Equal(t, false, status["deployment_ready"]) // 2 != 3
	assert.InDelta(t, 66.666666, status["readiness_percent"], 0.001)
	assert.Equal(t, "https://webapp.example.com", status["endpoint_url"])
}

func TestEvaluatorAttr_ProcessComposite_Connection(t *testing.T) {
	hclContent := `
resource "database" {
  body = {
    apiVersion = "postgresql.cnpg.io/v1"
    kind       = "Cluster"
    metadata = {
      name = "my-db"
    }
  }
  
  composite "connection" {
	body = {
      username = "dXNlcm5hbWU="  // base64("username")
	  password = "cGFzc3dvcmQ="  // base64("password") 
	  host     = "aG9zdA=="      // base64("host")
	}
  }
}
`

	evaluator := createTestEvaluator(t)
	ctx := createTestEvalContext()
	content := parseHCL(t, evaluator, hclContent, "test.hcl")

	diags := evaluator.processGroup(ctx, content)
	require.Empty(t, diags)

	// verify resource was created
	assert.Contains(t, evaluator.desiredResources, "database")

	// verify composite connection details were set
	assert.Len(t, evaluator.compositeConnections, 1)
	connections := evaluator.compositeConnections[0]

	// verify base64 decoded values
	assert.Equal(t, []byte("username"), connections["username"])
	assert.Equal(t, []byte("password"), connections["password"])
	assert.Equal(t, []byte("host"), connections["host"])
}

func TestEvaluatorAttr_ProcessComposite_ConnectionInvalidBase64(t *testing.T) {
	hclContent := `
resource "database" {
  body = {
    apiVersion = "postgresql.cnpg.io/v1"
    kind       = "Cluster"
    metadata = {
      name = "my-db"
    }
  }
  
  composite "connection" {
	body = {
      username = "dXNlcm5hbWU="  # valid base64
	  password = "invalid-base64!" # invalid base64
	}
  }
}
`

	evaluator := createTestEvaluator(t)
	ctx := createTestEvalContext()
	content := parseHCL(t, evaluator, hclContent, "test.hcl")

	diags := evaluator.processGroup(ctx, content)
	require.Empty(t, diags.Errs())

	// verify resource was created
	assert.Contains(t, evaluator.desiredResources, "database")

	// no connection details should be added due to invalid base64
	assert.Empty(t, evaluator.compositeConnections)

	// should have a discard entry for bad secret
	assert.Len(t, evaluator.discards, 1)
	assert.Equal(t, discardReasonBadSecret, evaluator.discards[0].Reason)
	assert.Equal(t, discardTypeConnection, evaluator.discards[0].Type)
}

func TestEvaluatorAttr_ProcessComposite_MultipleStatuses(t *testing.T) {
	hclContent := `
resource "frontend" {
  body = {
    apiVersion = "apps/v1"
    kind       = "Deployment"
    metadata = {
      name = "frontend"
    }
  }
  
  composite "status" {
	body = {
      frontend_ready = true
      frontend_replicas = 2
	}
  }
}

resource "backend" {
  body = {
    apiVersion = "apps/v1"
    kind       = "Deployment"
    metadata = {
      name = "backend"
    }
  }
  
  composite "status" {
	body = {
      backend_ready = true
	  backend_replicas = 3
	  shared_config = "common-value"
	}
  }
}
`

	evaluator := createTestEvaluator(t)
	ctx := createTestEvalContext()
	content := parseHCL(t, evaluator, hclContent, "test.hcl")

	diags := evaluator.processGroup(ctx, content)
	require.Empty(t, diags)

	// verify both resources were created
	assert.Contains(t, evaluator.desiredResources, "frontend")
	assert.Contains(t, evaluator.desiredResources, "backend")

	// verify multiple composite statuses were collected
	assert.Len(t, evaluator.compositeStatuses, 2)

	// verify frontend status
	frontendStatus := evaluator.compositeStatuses[0]
	assert.Equal(t, true, frontendStatus["frontend_ready"])
	assert.EqualValues(t, 2, frontendStatus["frontend_replicas"])

	// verify backend status
	backendStatus := evaluator.compositeStatuses[1]
	assert.Equal(t, true, backendStatus["backend_ready"])
	assert.EqualValues(t, 3, backendStatus["backend_replicas"])
	assert.Equal(t, "common-value", backendStatus["shared_config"])
}

func TestEvaluatorAttr_ProcessComposite_StatusIncomplete(t *testing.T) {
	hclContent := `
resource "incomplete-status" {
  body = {
    apiVersion = "v1"
    kind       = "ConfigMap"
    metadata = {
      name = "test"
    }
  }
  
  composite "status" {
	body = {
      ready = true
      unknown_field = req.composite.spec.nonexistent_field
	}
  }
}
`
	evaluator := createTestEvaluator(t)
	ctx := createTestEvalContext()
	content := parseHCL(t, evaluator, hclContent, "test.hcl")

	diags := evaluator.processGroup(ctx, content)
	require.Empty(t, diags.Errs())

	// verify resource was created
	assert.Contains(t, evaluator.desiredResources, "incomplete-status")

	// no composite status should be added due to incomplete evaluation
	assert.Empty(t, evaluator.compositeStatuses)

	// should have a discard entry for incomplete status
	foundDiscard := false
	for _, discard := range evaluator.discards {
		if discard.Type == discardTypeStatus && discard.Reason == discardReasonIncomplete {
			foundDiscard = true
			break
		}
	}
	assert.True(t, foundDiscard, "expected incomplete status discard")
}

func TestEvaluatorAttr_ProcessResources_WithComposite(t *testing.T) {
	hclContent := `
resources "workers" {
  for_each = ["worker-1", "worker-2"]
  
  template {
    body = {
      apiVersion = "batch/v1"
      kind       = "Job"
      metadata = {
        name = "${self.basename}-${each.key}"
      }
    }
  }
  
  composite status {
    body = {
      workers_created = 2
    }
  }

  composite status {
    body = {
      worker_names = [for r in self.resources : r.metadata.name]
	}
  }
}
`

	evaluator := createTestEvaluator(t)
	ctx := createTestEvalContext()
	content := parseHCL(t, evaluator, hclContent, "test.hcl")

	diags := evaluator.processGroup(ctx, content)
	require.Empty(t, diags.Errs())

	// verify resources were created
	assert.Contains(t, evaluator.desiredResources, "workers-0")
	assert.Contains(t, evaluator.desiredResources, "workers-1")

	// verify composite status from resources block
	require.Len(t, evaluator.compositeStatuses, 1)
	status := evaluator.compositeStatuses[0]

	// note: self.resources would be empty in this mock test since we don't populate observed resources
	// the test verifies the code path executes without error
	assert.Contains(t, status, "workers_created")
}

func TestEvaluatorAttr_ProcessComposite_InvalidLabel(t *testing.T) {
	hclContent := `
resource "test-resource" {
  body = {
    apiVersion = "v1"
    kind       = "ConfigMap"
    metadata = {
      name = "test"
    }
  }
  
  composite invalid-label {
    body = {
      some_field = "value"
    }
  }
}
`

	evaluator := createTestEvaluator(t)
	ctx := createTestEvalContext()
	content := parseHCL(t, evaluator, hclContent, "test.hcl")

	err := evaluator.processGroup(ctx, content)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid composite label")
}

func TestEvaluatorAttr_ProcessComposite_ConnectionNonStringValue(t *testing.T) {
	hclContent := `
resource "database" {
  body = {
    apiVersion = "postgresql.cnpg.io/v1"
    kind       = "Cluster"
    metadata = {
      name = "my-db"
    }
  }
  
  composite "connection" {
    body = {
      port = 5432  # non-string value should cause error
    }
  }
}
`

	evaluator := createTestEvaluator(t)
	ctx := createTestEvalContext()
	content := parseHCL(t, evaluator, hclContent, "test.hcl")

	err := evaluator.processGroup(ctx, content)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `connection key "port" was not a string, got float64`)
}

func TestEvaluatorAttr_ValidBase64Encoding(t *testing.T) {
	// helper test to verify our base64 test data is correct
	testCases := []struct {
		encoded string
		decoded string
	}{
		{"dXNlcm5hbWU=", "username"},
		{"cGFzc3dvcmQ=", "password"},
		{"aG9zdA==", "host"},
	}

	for _, tc := range testCases {
		decoded, err := base64.StdEncoding.DecodeString(tc.encoded)
		require.NoError(t, err)
		assert.Equal(t, tc.decoded, string(decoded))
	}
}

func TestEvaluator_ProcessComposite_NewNegativeCaseForAttr(t *testing.T) {
	hclContent := `
  composite "connection" {
    body = {
      port = 5432  # non-string value should cause error
    }
    body {
      port = 5432  # non-string value should cause error
    }
  }
`

	evaluator := createTestEvaluator(t)
	ctx := createTestEvalContext()
	content := parseHCL(t, evaluator, hclContent, "test.hcl")

	err := evaluator.processGroup(ctx, content)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `test.hcl:2,3-25: invalid resource body; body attribute and block cannot be specified at the same time`)
}
