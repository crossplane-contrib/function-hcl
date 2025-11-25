
// the resource block defines a single resource to be created, the name is the crossplane name
resource my-bucket {
  // body defines the output you want and can use expressions anywhere
  // req.composite is the composite resource
  // similarly req.resources.my-bucket will give you the observed bucket resource
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
}
