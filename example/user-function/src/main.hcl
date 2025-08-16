function toProviderK8sObject {
  arg name {
    // type        = string
    description = "metadata name of the return object"
  }
  arg manifest {
    // type        = object
    description = "the inner manifest to be wrapped into the k8s provider object"
  }
  arg providerName {
    // type        = string
    description = "name of the K8s provider"
    default     = "default"
  }
  //returns {
    // type        = object
    //description = "wrapper k8s object"
  //}

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
