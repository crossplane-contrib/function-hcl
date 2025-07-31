// the resources block defines multiple resources to be created, the associated name is
// used as a basename.
resources my-bucket {

  locals {
    params = req.composite.spec.parameters
    numBuckets = try(params.numBuckets, 1)
  }

  // iterate over a list with bucket numbers
  for_each = range(numBuckets)

  // you can optionally define how the crossplane name of individual resources
  // are created. each.key and each.value are set for each iteration with
  // Terraform semantics.
  name = "${self.basename}-${each.value}" // this is the default definition

  // define the template to be used for each iteration
  template {
    // you can have locals here too
    locals {
      name = "${req.composite.metadata.name}-bucket-${each.value}"
    }

    body = {
      apiVersion : "s3.aws.upbound.io/v1beta1"
      kind : "Bucket"
      metadata : {
        name : name
        annotations: {
          base-name: self.basename // this has the name given to the resources block
          crossplane-name: self.name // this has the generated crossplane name of the resource
        }
      }
      spec : {
        forProvider : {
          region : params.region
        }
      }
    }
  }
}
