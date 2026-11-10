---
title: "Error Conditions"
linkTitle: "Error Conditions"
weight: 3
description: >
  Every error condition the function can produce.
---

The following conditions are treated as errors by function-hcl. When an error occurs, the function
returns a `Fatal` result that surfaces on the composite resource.

## Parsing Errors

| Error            | Description                                       |
|------------------|---------------------------------------------------|
| HCL syntax error | Invalid HCL syntax in the source files            |
| Schema violation | Block structure doesn't match the expected schema |

## Variable Errors

| Error                  | Description                                                                   |
|------------------------|-------------------------------------------------------------------------------|
| Unknown local variable | Reference to a local variable name that doesn't exist                         |
| Circular reference     | Circular dependencies in locals (e.g. `locals { a = b; b = a }`)              |
| Local shadowing        | A local variable in an inner scope has the same name as one in a parent scope |

## Resource Errors

| Error                               | Description                                                                                                                      |
|-------------------------------------|----------------------------------------------------------------------------------------------------------------------------------|
| Duplicate resource name             | Two resources produce the same crossplane name                                                                                   |
| Existing resource became incomplete | A resource that exists in the observed state now has an incomplete value. This is a safety check to prevent accidental deletion. |

## Condition Errors

| Error                 | Description                                                                                                                                   |
|-----------------------|-----------------------------------------------------------------------------------------------------------------------------------------------|
| Incomplete condition  | A `condition` value that cannot be evaluated (note: incomplete conditions are treated as `false`, but certain evaluation failures are errors) |
| Non-boolean condition | A `condition` evaluates to something other than `true` or `false`                                                                             |

## Status and Connection Errors

| Error                         | Description                                                                              |
|-------------------------------|------------------------------------------------------------------------------------------|
| Conflicting status values     | Two `composite status` blocks set the same non-object leaf attribute to different values |
| Conflicting connection values | Two `composite connection` blocks set the same key to different values                   |
| Non-string connection value   | A connection detail value is not a base64-encoded string                                 |

## Requirement Errors

| Error                             | Description                                            |
|-----------------------------------|--------------------------------------------------------|
| Both matchName and matchLabels    | A `requirement` block specifies both selection methods |
| Neither matchName nor matchLabels | A `requirement` block specifies no selection method    |
| Type mismatch                     | Data type mismatch in `select` attributes              |

## Function Errors

| Error                 | Description                                                                    |
|-----------------------|--------------------------------------------------------------------------------|
| Invalid function name | A `function` or `arg` has a name that is not a valid identifier                |
| Unknown function      | `invoke` references a non-existent function                                    |
| Bad invocation        | `invoke` called with missing required arguments or unrecognized argument names |
| Stack overflow        | Call stack exceeds 100 frames (infinite recursion protection)                  |

## Validation Errors

| Error                      | Description                                                                                                       |
|----------------------------|-------------------------------------------------------------------------------------------------------------------|
| Validation failed          | A `validation` condition is `false`. The error is `validation failed; <error_message>`                            |
| Message not evaluated      | A `validation` condition is `false` and its `error_message` cannot be built. The error says why, after `validation failed;` |
| Non-boolean condition      | A `validation` condition has a known type other than bool                                                        |
| Non-string message         | A `validation` `error_message` has a known type other than string, whatever the condition                         |

## Context Errors

| Error                      | Description                                                                 |
|----------------------------|-----------------------------------------------------------------------------|
| Conflicting context values | Two `context` blocks write different non-object values to the same key path |
