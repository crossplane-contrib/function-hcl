resource my-s3-bucket {
  // self.name will be set to "my-s3-bucket"

  // locals are private to this resource
  locals {
    resourceName = "${req.composite.metadata.name}-bucket"
    params       = req.composite.spec.parameters
    tagValues = {
      foo = "bar"
    }
  }

  // body contains the resource definition as a schemaless object.
  body {
    apiVersion = "s3.aws.upbound.io/v1beta1"
    kind       = "Bucket"
    metadata = {
      name = resourceName
    }
    spec = {
      forProvider = {
        forceDestroy = true
        region       = params.region
        tags         = tagValues
      }
    }
  }
}
