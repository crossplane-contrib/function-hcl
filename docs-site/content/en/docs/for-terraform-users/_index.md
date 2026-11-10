---
title: "For Terraform Users"
linkTitle: "For Terraform Users"
weight: 7
description: >
  A guide for people coming from Terraform to function-hcl.
---

function-hcl is built on [HCL](https://github.com/hashicorp/hcl), the same language that powers Terraform.
If you've written Terraform modules, the syntax will feel familiar -- but the semantics differ in important ways.

This page maps Terraform concepts to their function-hcl equivalents and highlights the key differences.

## What's the same

- **HCL syntax** -- all [HCL expressions](https://github.com/hashicorp/hcl/blob/main/hclsyntax/spec.md) work as expected: string templates, conditionals, `for` expressions, splat operators, etc.
- **Built-in functions** -- all Terraform functions from v1.5.7 are available, except file I/O and impure functions (like `uuid`).
- **Locals** -- `locals {}` blocks define named values. Variable ordering doesn't matter; dependencies are resolved automatically.
- **String templates** -- `"${var}-suffix"` interpolation works identically.

## What's different

| Terraform                              | function-hcl                                            | Notes                                                                                                |
|----------------------------------------|---------------------------------------------------------|------------------------------------------------------------------------------------------------------|
| `resource "aws_s3_bucket" "my_bucket"` | `resource my-bucket`                                    | One label (crossplane name), not two (type + name). The Kubernetes `apiVersion`/`kind` go in `body`. |
| `local.foo`                            | `foo`                                                   | Locals are accessed directly by name, no `local.` prefix needed.                                     |
| `count`                                | `condition`                                             | Boolean (true/false) instead of a count. Use `resources` with `for_each` for multiple instances.     |
| `for_each` on a resource               | `resources` block                                       | A separate block type for collections, with `for_each`, `name`, and `template` sub-blocks.           |
| `data` sources                         | `requirement` blocks                                    | Request extra resources from Crossplane (e.g. EnvironmentConfigs). No true CSP data sources.         |
| `output`                               | `composite status` / `composite connection` / `context` | Different mechanisms for different outputs.                                                          |
| `module`                               | `function` + `invoke()`                                 | User-defined functions that return values, not resource trees.                                       |
| `vars`                                 | `req`                                                   | No user-defined inputs. All inputs come in via the `RunFunctionRequest` available as `req`           |
| State management                       | Crossplane handles it                                   | No `.tfstate` file. Crossplane tracks observed/desired state.                                        |

## Local variables: key differences

In Terraform, locals live in a single flat namespace per module. In function-hcl:

- Locals can be defined at **multiple scopes**: top-level, inside `resource` blocks, inside `group` blocks, inside `resources` templates.
- Inner scopes can access outer scope locals, but **cannot shadow** them.
- No `local.` prefix -- just use the name directly.

```hcl
# Terraform
locals {
  name = var.name
}
resource "aws_s3_bucket" "b" {
  bucket = local.name # must use local. prefix
}

# function-hcl
locals {
  name = req.composite.metadata.name
}
resource my-bucket {
  body = {
    # ...
    metadata = { name = name } # no prefix needed
  }
}
```

## Resources: key differences

Terraform resources have a _type_ and a _name_. function-hcl resources have only a _name_ (the crossplane resource name), and the type is expressed in the `body` as `apiVersion` and `kind`.

```hcl
# Terraform
resource "aws_s3_bucket" "my_bucket" {
  bucket = "my-bucket"
  region = "us-east-1"
}

# function-hcl
resource my-bucket {
  body = {
    apiVersion = "s3.aws.upbound.io/v1beta1"
    kind       = "Bucket"
    metadata   = { name = "my-bucket" }
    spec = {
      forProvider = {
        region = "us-east-1"
      }
    }
  }
}
```

## Collections: for_each

In Terraform, `for_each` is an argument on a `resource`. In function-hcl, collections use a dedicated `resources` (plural) block:

```hcl
# Terraform
resource "aws_s3_bucket" "buckets" {
  for_each = toset(var.bucket_names)
  bucket   = each.value
}

# function-hcl
resources buckets {
  for_each = toset(params.bucketNames)
  template {
    body = {
      apiVersion = "s3.aws.upbound.io/v1beta1"
      kind       = "Bucket"
      metadata   = { name = each.value }
      spec       = { forProvider = { region = params.region } }
    }
  }
}
```

The `each.key` and `each.value` variables work the same way.

## Validation

Terraform's variable `validation` blocks become top-level [`validation`](../language-guide/validation/)
blocks that check `req.composite`. Prefer rules in the XRD schema, which reject invalid input before it
is stored; a `validation` block covers rules the schema cannot express.

## No equivalent in function-hcl

These Terraform concepts have no direct equivalent:

- **Providers and provider configuration** -- handled by Crossplane at the cluster level
- **Backend configuration** -- Crossplane manages state
- **Provisioners** -- not applicable
- **`moved` / `import` blocks** -- Crossplane handles resource lifecycle
