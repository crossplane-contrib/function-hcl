---
title: "Language Guide"
linkTitle: "Language Guide"
weight: 3
description: >
  A tutorial-style guide to every construct in the function-hcl DSL.
---

This section walks through every feature of the function-hcl DSL, one topic at a time.
Each page builds on concepts introduced earlier, so if you're new to function-hcl,
reading them in order is recommended.

The guide covers:

1. [Source Format](./source-format/) -- how HCL files are packaged as input
2. [HCL Basics](./hcl-basics/) -- identifiers, blocks, attributes, and other fundamentals
3. [The req Variable](./variables/) -- accessing the Crossplane request state
4. [Local Variables](./local-variables/) -- defining and scoping locals
5. [Resource Blocks](./resource-blocks/) -- declaring individual composed resources
6. [Resource Collections](./resource-collections/) -- creating multiple resources with `for_each`
7. [Groups](./groups/) -- scoping locals to a set of resources
8. [Conditions](./conditions/) -- conditionally creating resources
9. [Composite Status](./composite-status/) -- writing back to the XR status
10. [Composite Connection Details](./composite-connection/) -- writing connection details
11. [Resource Ready Status](./ready-status/) -- controlling resource readiness
12. [Context](./context/) -- sharing data with downstream pipeline steps
13. [Requirements](./requirements/) -- requesting extra resources from Crossplane
14. [User-Defined Functions](./user-functions/) -- writing and invoking custom functions
15. [Validation](./validation/) -- rejecting invalid input with an error message
