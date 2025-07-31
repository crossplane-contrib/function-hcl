# function-hcl
---

A crossplane function that uses an opinionated DSL built on [HCL](https://github.com/hashicorp/hcl) 
to model desired resources. It has more than a passing familiarity with Terraform syntax.

```
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: example
spec:
  compositeTypeRef:
    apiVersion: example.crossplane.io/v1
    kind: XR
  mode: Pipeline
  pipeline:
  - step: create-a-bucket
    functionRef:
      name: function-hcl
    input: |

     -- main.hcl --
     
     locals {
        comp     = req.composite
        compName = comp.metadata.name
        params   = comp.spec.parameters
     }

     resource my-bucket {
        locals {
            region = params.region
        }

        body = {
          apiVersion: "s3.aws.upbound.io/v1beta1"
          kind: "Bucket"
          metadata: {
            name: "${compName}-bucket"
          }
          spec: {
            forProvider: {
              region: region
            }
          }
        }
        
        composite status {
          body = {
            bucketArn = self.resource.status.atProvider.arn
          }
        }
     }
```

The function has the following interesting properties:

* The ability to discard incomplete resources from the output. 
  For instance, if your desired object has a value in its spec that is derived from the status of some other object, 
  that object will only be rendered  when the status of the dependency is updated and can be resolved.**(*)** 
  This also applies to composite status, connection details, and context values and eliminates a lot of boilerplate
  conditional state tracking in the code.

* Special variables allow you to access the observed version of the current resource in the context of
  of the resource block.
  This feature makes it dead simple to set composite status.
  See the example above.

* Unification of composite status, connection details, and context values such that they can be partially
  updated from multiple locations in the code.

* Support for the [txtar format](https://pkg.go.dev/golang.org/x/tools/txtar#hdr-Txtar_format) that allows 
  for multiple HCL files to be packaged as a single script and unpacked in the function. 
  This preserves all file names and line numbers that appear in error messages.

* An interface that people coming from the Terraform world will be able to grok easily even if many of the
  details are different. 


**(*)** - if the thought of automatically dropping resources scares you - and it _should_ - the function provides 
safeguards such that it will _never_ drop a resource that is already known to exist. 
It will error out in these cases. 

In addition, it emits an event for every such discarded resource telling you exactly which expressions are incomplete, 
and maintains a status condition explicitly for this purpose. 
This allows you to fix any typos that prevent resources from being rendered as opposed to unknown dependency state.

Start with the [examples](example/README.md), then read the [spec](spec.md).

**Implementation/ License note:** This repo contains code copied from the Terraform repository and modified for use.
Care has been taken to copy this from the `v1.5.7` tag of the terraform codebase which had a Mozilla Public 2.0 license.

