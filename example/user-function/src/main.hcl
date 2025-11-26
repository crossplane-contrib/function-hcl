function toProviderK8sSpec {
  arg manifest {
    description = "the inner manifest to be wrapped into the k8s provider object"
  }
  arg providerName {
    description = "name of the K8s provider"
    default     = "default"
  }

  result = {
    forProvider = {
      manifest = manifest
    }
    providerConfigRef = {
      name = providerName
    }
  }
}

resource local-provider-config {
  locals {
    manifest = {
      apiVersion = "kubernetes.crossplane.io/v1alpha1"
      kind       = "ProviderConfig"
    }
  }
  body {
    apiVersion = "kubernetes.crossplane.io/v1alpha1"
    kind       = "Object"
    metadata = {
      name = "foo-foobar"
    }
    spec = invoke("toProviderK8sSpec", {
      manifest = manifest
    })
  }
}
