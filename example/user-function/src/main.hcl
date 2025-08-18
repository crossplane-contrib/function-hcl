function toProviderK8sObject {
  arg name {
    description = "metadata name of the return object"
  }
  arg manifest {
    description = "the inner manifest to be wrapped into the k8s provider object"
  }
  arg providerName {
    description = "name of the K8s provider"
    default     = "default"
  }

  locals {
    objectName = "foo-${name}"
  }

  body = {
    apiVersion = "kubernetes.crossplane.io/v1alpha1"
    kind       = "Object"
    metadata = {
      name = objectName
    }
    spec = {
      forProvider = {
        manifest = manifest
      }
      providerConfigRef = {
        name = providerName
      }
    }
  }
}

resource local-provider-config {
  locals {
    manifest = {
      apiVersion : "kubernetes.crossplane.io/v1alpha1"
      kind : "ProviderConfig"
    }
  }
  body = invoke("toProviderK8sObject", {
    name     = "foobar"
    manifest = manifest
  })
}
