
resource my-bucket {
  body {
    apiVersion = "s3.aws.upbound.io/v1beta1"
    kind       = "Bucket"
    metadata = {
      name = "${req.composite.metadata.name}-bucket" // use the composite name in a template expression
    }
    spec = {
      forProvider = {
        region = req.composite.spec.parameters.region
      }
    }
  }

  // self.resource gives you the observed version of the resource in this block.
  // use it to set composite status
  composite status {
    body {
      bucketARN = self.resource.status.atProvider.arn
    }
  }

}
