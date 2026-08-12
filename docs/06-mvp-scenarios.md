# 6. MVP scenarios

Each scenario validates a design decision made in the other documents. A scenario is only useful if it can fail visibly — each is worded to actually exercise the behavior, not just describe it.

**S1 — Basic hierarchical resolution**
A base + one layer per environment (dev/staging/prod) are defined; resolving for a given environment produces the expected final configuration, with no value leaking from another environment.
*Validates: [01-problem.md](01-problem.md) (hierarchy).*
*Implemented and tested: `resolve.TestResolve_HierarchicalBase`.*

**S2 — Declarative merge of a list**
A list-valued key is annotated with a merge strategy (replace / union / append); two successive layers affecting that key produce a result matching the declared strategy — and a different result if the annotation changes, without touching the engine.
*Validates: [02-resolution-model.md §2.1](02-resolution-model.md).*
*Implemented and tested: `resolve.TestResolve_DeclarativeListMerge`.*

**S3 — Dynamic layer depending on accumulated state**
A dynamic layer (script) reads a value produced by previous layers (e.g. `env`) and computes a derived value inserted into the accumulator — reproduces the `env`/`foo` example from [02-resolution-model.md §2.6](02-resolution-model.md).
*Implemented and tested: `resolve.TestResolve_DynamicLayerReadsAccumulatedState`.*

**S4 — Provenance of a value**
Looking up the explain directly from configblender ([05-recipe-and-crd.md §5.3](05-recipe-and-crd.md)) gives, for any key in the final configuration, the exact source layer — including for a value produced by a dynamic layer.
*Validates: [02-resolution-model.md §2.2/§2.3](02-resolution-model.md).*
*Implemented and tested: `resolve.TestResolve_Explain`.*

**S5 — Isolation of a misbehaving dynamic layer**
A dynamic layer attempts an operation outside its allowed scope (network access, host filesystem access), or consumes unbounded resources; execution is blocked without compromising the resolution of other layers.
*Validates: [03-security.md](03-security.md).*
*Implemented and tested (resource-limiting part): `resolve.TestResolve_DynamicLayerStepLimit`. The "hermetic by construction" part (no file/network access) is a property of Starlark itself, not a test to write on configblender's side.*

**S6 — Coexistence with a secrets manager**
A deployment combines configuration produced by configblender with secrets injected independently by Vault (K8s Secret); no sensitive data passes through configblender, no conflict between the two sources.
*Validates: [04-kubernetes.md §4.1](04-kubernetes.md).*
*Satisfied by construction rather than directly tested: the controller only reads/writes a ConfigMap (never a Secret) and has no dependency on Vault — no dedicated test is relevant, the absence of interaction is the property being sought.*

**S7 — Consumption by the application in the cluster**
An application pod mounts the ConfigMap managed by configblender (final tree) as a volume, with no direct knowledge of configblender; an update to the resolution propagates to the pod via the native K8s mechanism for syncing mounted ConfigMaps.
*Validates: [04-kubernetes.md §4.3/§4.4](04-kubernetes.md) and the runtime nature ([03-security.md](03-security.md)).*
*Implemented and tested on the production side (ConfigMap creation + update on Recipe change, `internal/controller.TestReconcile_CreatesConfigMap`/`TestReconcile_UpdatesConfigMapOnRecipeChange`, controller-runtime fake client). The mount on the application-pod side itself is a native K8s mechanism, not testable without a real cluster.*

**Explicitly out of MVP scope** (consistent with the non-goals already established):
- Schema validation ([04-kubernetes.md §4.5](04-kubernetes.md)).
- GitOps validation pipeline for scripts — mentioned in [03-security.md](03-security.md) as a possible complementary approach, not required for the MVP.
- Support for multiple dynamic languages — multi-language extensibility ([02-resolution-model.md §2.6](02-resolution-model.md)) is an architectural constraint for the MVP, not a delivered feature (a single language is functionally enough).
