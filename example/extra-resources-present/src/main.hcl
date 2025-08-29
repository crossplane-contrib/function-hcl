locals {
  comp = req.composite  // req.composite contains the composite resource
  compName = comp.metadata.name
  params   = comp.spec.parameters
}

// get an environment config out of which we will extract labels
requirement labels-config {
  condition = true
  locals {
    ecName = "foo-bar"
  }
  select {
    apiVersion  = "apiextensions.crossplane.io/v1beta1"
    kind        = "EnvironmentConfig"
    matchLabels = { foo = "bar" }
  }
}

resource my-bucket {
  body = {
    apiVersion = "s3.aws.upbound.io/v1beta1"
    kind       = "Bucket"
    metadata = {
      name = "${compName}-bucket"
      // set the labels to the labels data value in the first env config object
      labels = req.extra_resources.labels-config[0].data.labels
    }
    spec = {
      forProvider = {
        region = params.region
      }
    }
  }
}






