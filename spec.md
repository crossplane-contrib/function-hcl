# Specification

## Basics

We support creating crossplane resources using a custom function written on top of 
[HCL](https://github.com/hashicorp/hcl), the same language on which Terraform syntax is based. 
It is important to note that this is _not_ Terraform, 
just a DSL (domain specific language) that has things in common with Terraform syntax. 

## Source code

Source code is written in [HCL syntax](https://github.com/hashicorp/hcl/blob/main/hclsyntax/spec.md)
and may be spread across multiple files. 
All files are still treated as one unit. 
The only difference between having multiple files versus concatenating them into a single file is that 
line numbers will be different in error messages.

The function accepts a single block as text as input. 
This MUST be in [txtar](https://pkg.go.dev/golang.org/x/tools/txtar#hdr-Txtar_format) format such that original 
file names are maintained and line numbers agree with the source code.

## External Variables

External variables are not user-defined - rather they are standard and are created from
the `RunFunctionRequest` passed to the function implementation.

These are accessible as `req.<something>`

* `req.context` - the crossplane context (`map[string]any`)
* `req.composite` - the observed composite resource body i.e. the XR (`map[string]k8sObject`)
* `req.composite_connection` - the observed connection details object of the composite resource (`map[string][]byte`)
* `req.resource` - the resource bodies of observed resource keyed by crossplane resource name (`map[string]k8sObject`).
* `req.connection` - the connection details of observed resources keyed by crossplane resource name (`map[string]map[string][]byte`).
* `req.resources` - the list of resource bodies of observed resources keyed by crossplane resource collection name (`map[string][]k8sObject`).
* `req.connections` - the list of connection details of observed resources keyed by crossplane resource collection name (`map[string][]map[string][]byte`).
* `req.extra_resources` - map of a list of resource bodies keyed by extra resource name. (`map[string][]map[string]any`)

## Local variables

These behave like Terraform but do **not** need to be prefixed with `local.`. 
A local named `foo` is accessible simply as `foo`. 

Example:

```hcl
    locals {
        baseName = req.composite.metadata.name
        computedName = "${baseName}-bucket"  // note how it refers to the `baseName` local 
    }
```

Unlike Terraform, local variables can only access information from `req`, other local variables, and some
other classes of variables described later. They cannot access information from arbitrary blocks. 

Also unlike Terraform, function-hcl allows you to create a `locals` block in many other places so you
can use temporary variables in each resource without worrying about file-level collisions.

Note that local variables cannot shadow names from a parent scope. That is, it is invalid to declare a local variable
called `x` inside a resource block if a local variable `x` also exists at the file level. It _is_ valid to use the
same local variable name in different resource blocks so long as there is no file-level local with that name.

All `locals` blocks in a given scope are processed together and variable ordering does not matter. 
The following example is treated exactly the same as the previous example.

```hcl
    locals {
      computedName = "${baseName}-bucket"
    }

    locals {
      baseName = req.composite.metadata.name
    }
```

## Special variables

Some automatic variables are automatically available in specific blocks and have dynamic values based on the context in
which they are defined.
These are documented for each such block.
These variables are prefixed with `self.` or `each.`.

## Expressions and functions

All HCL syntax as specified on [this page](https://github.com/hashicorp/hcl2/blob/master/hcl/hclsyntax/spec.md)

All Terraform functions as of 1.5.7 are supported, _except_

* any function that is related to file handling, since function-hcl is a memory-only system
* impure functions like `uuid`, `uuid5` etc. that introduce randomness.
  The intent is to have a hermetic system where a given set of inputs always lead to the same outputs.

It is also possible to write your own functions. See the section on user-defined functions.

## Create a resource

Use the `resource` block to create a resource. This

* defines a desired resource with a specific crossplane name.
* the `body` attribute is the Kubernetes object you wish to create.
* you can add locals that are scoped to just the resource. 
* you can include other blocks related to composite status, connection details etc.
  These are described in a later section.

Special variables that are available are:

* `self.name` - gives you the crossplane name of the resource for the block.
* `self.resource` - gives you the observed resource for the resource being in the current block.
  This can be an incomplete value if no observed resource exists.
* `self.connection` - gives you the connection details of the resource.
  This can also be an incomplete value.

The above variables will also be available for other blocks within the resource block, described later.

```hcl
// format: resource <crossplane-name>
resource my-s3-bucket {
  // self.name will be set to "my-s3-bucket"

  // locals are private to this resource
  locals {
    resourceName = "${req.composite.metadata.name}-bucket"
    params       = req.composite.spec.parameters
    tagValues = {
      foo : "bar"
    }
  }

  // body contains the resource definition as a schemaless object.
  // it is a single object so you can either use `:` or `=` to assign values as allowed by HCL.
  // The example below deliberately mixes things up to show both are possible.
  // However `body` itself can only be assigned with a `=` sign since it is a block attribute.
  body = {
    apiVersion = "s3.aws.upbound.io/v1beta1"
    kind       = "Bucket"
    metadata : {
      name : resourceName
    }
    spec : {
      forProvider : {
        forceDestroy : true
        region = params.region
        tags   = tagValues
      }
    }
  }
}
```

## Create a list of resources

The `resources` block defines a list of resources to be created based on an input list, set, or map. 

* the `for_each` attribute must evaluate to a supported collection (list, set, or map)
* the `name` attribute can use the iterator key and value
* the `template` block has access to iterator information, otherwise it behaves exactly like a `resource` block.

Example:

```hcl
// format: resources <base-crossplane-name> 
resources additional_buckets  {
    locals {
      params   = req.composite.spec.parameters
      suffixes = req.composite.spec.parameters.suffixes
    }
    // list, map, or set to iterate on
    // the iterator variable is always called `each` and provides `each.key` and `each.value`
    // which is accessible in the name expression and the body.
    for_each = suffixes

    // the name attribute allows you to provide an expression to generate the crossplane name of
    // each resource created. It is optional and by default is set to "${self.basename}-${each.key}"
    // where `self.basename` is the name of the resource collection.
    // You can change this by explicitly specifying a name expression.
    name="${self.basename}-${each.key}" // optional, default is as shown

    // the template block allows you to render each child resource. It has exactly the same semantics
    // as a resource block and anything you can do in a resource block is allowed here. 
    template {
      locals {
        resourceName = "${req.composite.metadata.name}-${self.name}"
      }
      // Note that it is your responsibility to
      // set metadata.name to a unique, stable name. The `self.name` special variable contains the
      // output of the name expression and may be used for this purpose.
      body = {
        apiVersion = "s3.aws.upbound.io/v1beta1"
        kind       = "Bucket"
        metadata = {
          name = resourceName
        }
        spec = {
          forProvider = {
            forceDestroy = true
            region       = params.region
          }
        }
      }
    }
}
```

Special variables that are available are:

* `self.basename` - the name given to the resources block
* `self.resources` - the collection of observed resources. Can be an incomplete value if no observed resources exist.
* `self.connections` - the collection of observed connections. Can be an incomplete value if no observed connections exist.
* `each.key` - the current key of the iterator which is the index for arrays, the map key for maps and the actual value
   for sets. This is available for the `name` attribute as well as in the `template` block.
* `each.value` - the current value of the iterator which is the value in the array index, value for the map key or the
   value from a set.

## Groups of resources

The `group` block allows you to group related resources together. It allows you to create a "scope" where the local
variables you define are only available to the resources in the group. 

```hcl

group {
  locals {
    foo = "bar"
  }
  
  resource one {
    // use foo somewhere ...
  }

  resource two {
    // foo is also available here ...
  }
}

// but not here ...

```

## Create resources conditionally

Use a `condition` attribute to create a resource only if specific conditions are met. 
This is the equivalent of Terraform `count`, except it is a boolean value rather than an actual count. 
You can have conditions for individual resources, resource lists, and groups.

```hcl
// The resource block can have an optional condition attribute that is an expression which must evaluate to a 
// boolean value. Incomplete values are not allowed here. Use `try` and `can` if something could be missing.
resource s3_acl {
    condition = try(req.composite.spec.parameters.createAcls, true) // defaults to true if unspecified
    body {
        // ...
    }
}

// you can do the same thing for the resources block; in this case the entire list of resources is skipped.
resources s3_acls {
    // the condition applies to the entire list.
    // To filter out individual elements, filter the object that will be looped on, instead.
    condition = req.composite.spec.parameters.createAcls 

    for_each = req.composite.spec.parameters.suffixes
    body {
        // ...
    }
}

// and for groups
group {
  condition = req.composite.spec.parameters.createMoreBuckets
  
  resource additional-bucket-one {
    // ...    
  }

  resource additional-bucket-two {
    // ...
  }

}

```

## Write composite status

This block can be specified any number of times and each block can update specific fields in the status.

Status blocks can be written at any level. At the top level, you could do:

```hcl
composite status {
  body = {
    foobarId = req.resource.foobar.status.atProvider.id
  }
}
```

But it is much easier to do this in a `resource` block that gives you direct access to the observed version of the
resource in `self.resource`. 

```hcl
resource foobar {
  
  // resource definition etc.
  // ...

  composite status {
    body = {
      foobarId = self.resource.status.atProvider.id
    }
  }
}
```

If you create multiple status blocks that update the same status attribute with different values, the function will return
an error. Objects are however merged.

So it's ok for two resources to  produce status as follows:

```hcl
  composite status {
    body = {
      foo = { bar : { baz : { x : 10 } } }
  
      // not ok
      // clash = 10
    }
  }

  composite status {
    body {
      foo = { bar: { baz : { y : 12 } } }

      // not ok
      // clash = 20
    }
  }
```

## Write composite connection details

Can be specified any number of times and each block can update specific fields in the connection details.
Two blocks cannot update the same attribute if they have different values.

All values need to be strings that are base64 encoded otherwise the function returns an error.

This works similar to how the status blocks work in terms of scoping. 

At the top-level:

```hcl
composite connection {
  body {
    url = base64encode(req.resource.foobar.status.atProvider.url)
  }
}
```

Within a resource block:

```hcl
resource foobar {
  
  // resource definition etc.
  // ...

  composite connection {
    body {
      url = base64encode(self.resource.status.atProvider.url)
    }
  }
}
```

## Set resource ready status

You can use the `ready` block under any resource.

```hcl
resource foo {
  // ...
  ready {
    value = "READY_TRUE"
  }
}
```

The value must evaluate to a string and be one of `READY_UNSPECIFIED`, `READY_TRUE`, or `READY_FALSE`


## Write to the context

This block allows you to set values on the context. You need to specify the key and value as attributes.
You can update a single key that has an object value from multiple blocks using the same rules as described
for `composite status` blocks.

```hcl
context  {
  key   = "example.com/foo-bar-baz"
  value = {
    foo = {
      bar = "baz"
    }
    bar: 10
    baz: "quux"
  }
}

context  {
  key   = "example.com/foo-bar-baz"
  value = { 
    foo : {
      baz: "bar"
    }
  }
}

```

## Set requirements in the response for extra resources

You can ask for extra resources that crossplane will supply when requested. 

You can ask for requirements by object name:

```hcl
requirement my-config {
  select {
    apiVersion = "apiextensions.crossplane.io/v1beta1"
    kind       = "EnvironmentConfig"
    matchName  = "foo-bar"
  }
}
```

or by object labels:

```hcl
requirement my-config {
  select {
    apiVersion   = "apiextensions.crossplane.io/v1beta1"
    kind         = "EnvironmentConfig"
    matchLabels  = { foo = "bar" }
  }
}
```

The name given to the requirement is the key in `req.extra_resources` that will be set to an array of matching objects.
So, for example you can access resources produced from the requirement above in an expression as follows:

```hcl
  locals {
    myLabelValue = req.extra_resources.my-config[0].metadata.labels["my-label"]
  }
```

Note that you need to specify an array index to access the first matching object.

In addition, `requirement` blocks can have a `condition` attribute and `locals` blocks.

```hcl
requirement labels-config {
  condition = req.composite.metadata.labels["special"] == "true"
  locals {
    ecName = "foo-bar"
  }
  select {
    apiVersion = "apiextensions.crossplane.io/v1beta1"
    kind       = "EnvironmentConfig"
    matchName  = ecName
  }
}
```

* The requirement is skipped if the condition does not evaluate to true. The usual rules for conditions apply.
* Local variables can be used as temporary variables for complex calculations.

## User defined functions

### Defining functions

Functions can be defined in the code using a `function` block. They **must** be defined at the top-level. For example,
they cannot be defined under a `group`.

```hcl
function addNumbers {
  arg a {}
  arg b { default = 1 }

  locals {
    output = a + b
  }
  
  body = output
}
```

* The above block defines a user function named `addNumbers` that takes 2 arguments `a` and `b`. `b` has a default value
of 1 if it is not supplied. `a` must be supplied by the caller.
* Arguments are accessed in the function implementation as though they are local variables. Functions do not have access
  to any other state. For example, using `req.composite` etc. will fail.
* The `body` attribute is the return value of the function
* A function may use local variables for temporary calculations in `locals` blocks.
* A function can call other standard functions or invoke other user functions in its implementation.

### Invoking user functions

A standard function `invoke` may be used to invoke user functions.

```hcl
    locals {
      c = invoke("addNumbers", { a: 2, b: 3})
    }
```
* The first parameter to `invoke` is a user function name that **must** be a static string. Using variables is not allowed.
* The second parameter is a object that provides values to the function's arguments. 
  Arguments with defaults may be omitted.

### Recursion

It is possible (but not encouraged) to write self or mutually-recursive functions. 
For example, this is a valid `factorial` function.

```hcl
function factorial {
  arg n {}
  body = n < 1 ? 1 : n * invoke("factorial", { n: n - 1 })
}
```

Infinite recursion is prevented by a call stack that can only grow to 100. 
The expression `invoke("factorial",{ n: 101 })` will fail.

## Auto discarding incomplete values

function-hcl will automatically drop resource, status, connection, requirement, and context blocks if there are expressions that
refer to unknown values in them. 

For example, in this status block:

```hcl
resource {
  // ...
  composite status {
    body {
      url = self.resource.status.atProvider.url
    }
  }
}

```

The observed resource may not even exist if it is just being created. 
Even if it exists, it may not yet have a `url` status property.
In either case, function-hcl will not treat this as an error but will simply drop the status from its output.

This also apply to resources

```hcl

resource vpc {
  // ...

  composite status {
    body = {
      vpcId = self.resource.status.ayProvider.vpcId
    }
  }
}

resource subnet {
  //...
  body = {
    // ...
    spec: {
      forProvider: {
        vpcId: req.composite.status.vpcId
      }
    }
  }
}

```

In the above example the `vpc` resource writes it id to the composite status when available, and the subnet resource
sets the `vpcId` attribute in its spec based on the composite status.

In this case the `subnet` resource will not be rendered until the composite status has been updated with the `vpcId`.

### Fail-safe mechanisms

The rules for discarding things are:

* If _any_ expression in a block is incomplete, the _entire_ block is skipped. 
* If a resource already has an observed value (i.e. it has been created), but now has an incomplete value,
  refuse to drop the resource and return an error instead.
  This probably means that the user changed some code and introduced a typo or reliance on new information not yet 
  available.
  There will still be events that can you can inspect using `kubectl describe <xr>` that will show you what went wrong.

## Events and status values

The function reports a custom condition value called `FullyResolved` which is true only when there are no incomplete
values encountered in processing. In addition, it also exposes a condition value called `HclDiagnostics` that can contain
additional debugging information, especially useful in the case of incomplete values.

Examples:

```yaml
conditions:
  - lastTransitionTime: "2025-06-01T20:14:59Z"
    message: all items complete
    reason: AllItemsProcessed
    status: "True"
    type: FullyResolved

  - type: HclDiagnostics
    lastTransitionTime: "2024-01-01T00:00:00Z"
    message: "hcl.Diagnostics contains no warnings"
    reason: Eval
    status: "True"
```

Additional warning events show the variables and the source code positions that could not be evaluated.

## Error conditions

The following are treated as errors:

* Basic parsing errors in the HCL
* Basic schema violations
* References to a local variable name that does not exist
* Circular references in locals expressions (e.g. `locals{ a = b, b = a}`)
* Two resources are produced with the same crossplane name
* A condition value is incomplete
* A resource that is available in the observed state has become incomplete
* A non-object composite status value or a connection value is produced from two places, and they have different values
* A requirement block defines both `matchName` and `matchLabels` or does not specify either.
* There is a data type mismatch in the attributes of a requirement block.
* A function or an arg has a name that is not an identifier
* The invocation of a user function is for a non-existent function.
* A user function is invoked incorrectly, with an missing or bad keys in the parameters object.
