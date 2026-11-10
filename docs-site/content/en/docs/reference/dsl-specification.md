---
title: "DSL Specification"
linkTitle: "DSL Spec"
weight: 1
description: >
  Complete specification for the function-hcl DSL.
---

{{% alert title="Source of truth" color="info" %}}
This page is derived from [`spec.md`](https://github.com/crossplane-contrib/function-hcl/blob/main/function/spec.md)
in the repository. If you find a discrepancy, the repository version is authoritative.
{{% /alert %}}

## Input Format

The function accepts its HCL program in [txtar format](https://pkg.go.dev/golang.org/x/tools/txtar#hdr-Txtar_format)
via the `input` field of the Composition pipeline step. All files are treated as one unit.

## External Variables

Created automatically from the `RunFunctionRequest`. Accessed as `req.<field>`.

| Variable                   | Type                                  | Description                                         |
|----------------------------|---------------------------------------|-----------------------------------------------------|
| `req.composite`            | object                                | Observed composite resource (XR)                    |
| `req.composite_connection` | map(string, bytes)                    | Observed connection details of the composite        |
| `req.resource`             | map(string, object)                   | Observed resource bodies, keyed by crossplane name  |
| `req.connection`           | map(string, map(string, bytes))       | Observed connection details, keyed by resource name |
| `req.resources`            | map(string, list(object))             | Observed resource collections, keyed by base name   |
| `req.connections`          | map(string, list(map(string, bytes))) | Connection details of collections                   |
| `req.context`              | map(string, any)                      | Pipeline context                                    |
| `req.extra_resources`      | map(string, list(object))             | Extra resources from `requirement` blocks           |

## Top-Level Blocks

### `locals`

```hcl
locals {
  <name> = <expression>
}
```

- Accessed by name directly (no `local.` prefix).
- Ordering does not matter; locals with incomplete values are evaluated lazily.
- Circular references are an error.
- Cannot shadow names from parent scopes.
- Can be defined at: top level, `resource`, `resources` template, `group`, `requirement`, `function`.

### `resource`

```hcl
resource <crossplane-name> {
  condition = <bool>            # optional
  locals { ... }                # optional
  body = { <k8s-manifest> }    # required
  composite status { body = { ... } }      # optional, repeatable
  composite connection { body = { ... } }  # optional, repeatable
  ready { value = <string> }   # optional
}
```

**Special variables**: `self.name`, `self.resource`, `self.connection`

### `resources`

```hcl
resources <base-name> {
  condition = <bool>            # optional
  locals { ... }                # optional
  for_each = <collection>       # required (list, set, or map)
  name = <expression>           # optional, default: "${self.basename}-${each.key}"
  template {
    locals { ... }              # optional
    body = { <k8s-manifest> }  # required
    composite status { ... }    # optional
    composite connection { ... } # optional
    ready { ... }               # optional
  }
}
```

**Special variables**: `self.basename`, `self.name` (in template), `self.resources`, `self.connections`, `each.key`, `each.value`

### `group`

```hcl
group {
  condition = <bool>            # optional
  locals { ... }                # optional
  resource <name> { ... }       # any number
  resources <name> { ... }      # any number
  validation { ... }            # any number
}
```

### `validation`

```hcl
validation {
  condition     = <bool>        # required
  error_message = <string>      # required
}
```

- Can appear at top level or inside `group`.
- Runs before every other block, so a failing rule is reported even when a resource that reads the same
  input fails to evaluate.
- `condition` false: the evaluation fails with `validation failed; <error_message>`. Only the first
  failing block is reported.
- `condition` not known yet: the block is skipped with a warning.
- `condition` with a known type other than bool, or `error_message` with a known type other than string:
  the evaluation fails, whatever the condition's value.

### `composite status`

```hcl
composite status {
  body = { <status-fields> }
}
```

Can appear at top level or inside `resource`/`resources` template. Multiple blocks are merged;
conflicting non-object leaf values are an error.

### `composite connection`

```hcl
composite connection {
  body = { <connection-details> }
}
```

All values must be base64-encoded strings. Same merging/conflict rules as status.

### `context`

```hcl
context {
  key   = <string>
  value = <any>
}
```

Same merging/conflict rules as status. Can appear at top level or inside resource blocks.

### `requirement`

```hcl
requirement <name> {
  condition = <bool>            # optional
  locals { ... }                # optional
  select {
    apiVersion  = <string>
    kind        = <string>
    matchName   = <string>      # XOR
    matchLabels = <map(string)> # XOR
  }
}
```

Must specify exactly one of `matchName` or `matchLabels`.

### `function`

```hcl
function <name> {
  arg <name> {
    default     = <value>       # optional
    description = <string>      # optional
  }
  locals { ... }                # optional
  body = <return-value>         # required
}
```

- Must be defined at top level.
- No access to external state (`req`, `self`, etc.).
- Invoked with `invoke("name", { arg: value })`.
- Call stack limit: 100.

### `ready`

```hcl
ready {
  value = <string>  # "READY_UNSPECIFIED" | "READY_TRUE" | "READY_FALSE"
}
```

## Auto-Discard Rules

1. If any expression in a block is incomplete, the entire block is skipped.
2. If a resource already has an observed value but now has an incomplete value, return a fatal error.
3. Incomplete `condition` values are treated as `false`.

## Status Conditions

| Condition | `True` when | `False` when |
|-----------|------------|-------------|
| `FullyResolved` | No incomplete values encountered | One or more blocks deferred |
| `HclDiagnostics` | No HCL warnings | Warnings present (details in message) |
