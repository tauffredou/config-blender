# 1. Problem statement and overview of existing strategies

## 1.1 Problem statement

Deploying the same application across multiple environments (dev, staging, prod, potentially several instances per customer) creates a structural tension between two requirements:

- **Isolation**: a value specific to one environment must never leak into another (e.g. a staging URL used by mistake in prod).
- **Maintainability**: configuration must not be fully duplicated per environment, or it becomes unmanageable as the number of environments and configuration keys grows.

Several strategies exist to reconcile these two requirements, each with its own limitations.

## 1.2 Overview of existing strategies

| Strategy | Principle | Main limitation |
|---|---|---|
| Hierarchy (Ansible-style `group_vars`/`host_vars`) | Layered config (base → group → host) | Hard to debug: the final value is a mental merge of N files; a value misplaced at an intermediate level can leak into every child environment |
| Interpolation (`${var}`) | A key references another key's value | Hidden coupling between keys; resolution-order ambiguity when combined with hierarchy |
| Default values | Guarantee that an update doesn't break the app | Masks regressions: the deployment "works" with an unsuitable default instead of failing explicitly; the meaning of the default is often disconnected from actual usage |
| In-app computation | Dynamic config computed in code | Convenient early in a project (config close to the code), but doesn't scale: logic scattered across business code, not testable in isolation, requires a redeploy to change behavior |

These limitations motivate the two design axes explored in depth in [02-resolution-model.md](02-resolution-model.md).
