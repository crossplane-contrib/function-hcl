// top-level locals behave like Terraform locals and are available everywhere
// and accessed just using their name (no need to put "local." in front of it like Terraform)
locals {
  comp = req.composite // req.composite contains the composite resource

  // locals for commonly used things to reduce typing
  compName = comp.metadata.name
  params   = comp.spec.parameters
}

resource my-bucket {
  // a resource block (and indeed most other blocks) can have locals private to it.
  // These locals are not available outside this block
  locals {
    region = params.region // but this has access to locals in the outer scope
  }

  // body defines the output you want and can use expressions wherever
  body {
    apiVersion = "s3.aws.upbound.io/v1beta1"
    kind       = "Bucket"
    metadata = {
      name = "${compName}-bucket" // use the composite name in a template expression
    }
    spec = {
      forProvider = {
        region = region // assigns to the region local
      }
    }
  }
}





