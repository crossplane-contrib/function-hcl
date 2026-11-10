---
title: "Validation"
linkTitle: "Validation"
weight: 15
description: >
  Rejecting invalid input with an error message.
---

The `validation` block fails the evaluation with its `error_message` when its `condition` is false.
Use it to reject invalid input instead of rendering nothing. It can appear at the top level and in groups.

## Syntax

```hcl
validation {
  condition     = length(try(req.composite.spec.parameters.subnets, [])) > 0
  error_message = "subnets must list at least one subnet"
}
```

## Behavior

| Condition | Result |
|-----------|--------|
| `true` | Nothing happens. |
| `false` | The evaluation fails with `validation failed; <error_message>`, whether or not the resources already exist. |
| Cannot be evaluated yet | The block is skipped with a warning, like other incomplete values. |
| A known type other than bool, even if part of the value is not known yet or fails to evaluate | The evaluation fails. |

A condition that reads a missing attribute cannot be evaluated, so it is skipped rather than failed. Read
optional inputs with `try`, as in the example above, so that a missing value makes the condition false.

## The error message

An `error_message` with a known type other than string fails the evaluation whatever the condition's
value. The message is only needed when the condition is false, so it can read values that exist only when
the input is invalid:

```hcl
validation {
  condition     = !can(req.composite.spec.parameters.zone)
  error_message = "zone ${req.composite.spec.parameters.zone} is not allowed"
}
```

When the condition is false and the message cannot be built, the evaluation still fails as a validation
failure, and says why: `validation failed; error_message could not be evaluated: ...`.

## Order

Validation blocks run before every other block, at the top level and in every group whose `condition` is
true, wherever they appear in the source. A rule that fails is reported even when a resource that reads
the same input fails to evaluate. When several rules fail, only the first one in source order is reported.

A `validation` block inside a group whose `condition` is false, or not known yet, is not evaluated.
A group condition that calls `timestamp()` or `uuid()` can change between this first pass and the
render. When it becomes true, the group's validation blocks run in source order with its other blocks,
so the group never renders without them.

The language server completes the block and its two attributes and shows their descriptions on hover.
