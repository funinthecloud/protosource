# Architecture Review — protosource (May 2026)

**Date captured:** 2026-05-19  
**Commit:** `be136c7` ("fix(httpstandard): propagate r.Host into Headers["host"]")  
**Working tree:** clean  
**Context:** Full-project review via `/arch-review` (no uncommitted changes to focus on). Review performed against Separation of Concerns, SOLID, Scalability, and Maintainability.

This document is a snapshot for tracking improvement (or regression) over time. For the canonical current design, see:

- [Claude.md](../Claude.md) (detailed contributor guide)
- [docs/pipeline.md](pipeline.md)
- [docs/proto-annotations.md](proto-annotations.md)
- [docs/consumer-guide.md](consumer-guide.md)

---

## Executive Summary

protosource is a **code-generation-centric event-sourcing framework**. Domain models are defined declaratively in Protocol Buffers using custom annotations (`funinthecloud.protosource.options.v1`). A pair of buf plugins (`protoc-gen-protosource` and `protoc-gen-protosource-ts`) emit the vast majority of the mechanical Go and TypeScript code (aggregate `On()` replay, command builders, event emission, state-machine guards, authorization wiring, snapshot support, GSI query methods, Lambda handlers, Wire providers, and typed HTTP clients).

The **runtime core** (`protosource.go`, ~630 LOC) is deliberately small, interface-driven, and generic. It implements a strict, auditable command-processing pipeline with well-defined extension points for custom business logic.

**Health snapshot** (90-day window via jcodemunch):
- 175 files, 2,668 symbols, 2,262 functions/methods
- Average cyclomatic complexity: **2.47** (excellent)
- Dependency cycles: **0** (excellent)
- Dead code: ~4.4% (mostly test harness `main.go` files and generated `*mgr` CLIs — acceptable)
- Unstable modules: 2
- Top hotspots: the Go code generator (`cmd/protoc-gen-protosource/protosourceify.go` — several high-churn validation/generation methods) and the massive test CLI `example/app/test/v1/testmgr/main.go` (cyclomatic 80)
- Composite repo health: **89.9 (B)**

The "B" is driven almost entirely by generator churn and test harnesses, **not** by core runtime or layering problems.

---

## Major Architectural Layers & Data Flow

```
┌─────────────────────────────────────────────────────────────────┐
│  Proto DSL (source of truth)                                    │
│  options/v1/options_v1.proto + domain protos with annotations   │
│  (aggregate, command {produces_events, lifecycle}, event        │
│   {sets_state, collection}, snapshot, opaque + 20 GSIs,         │
│   projection, event_ttl_seconds, etc.)                          │
└─────────────────────────────────────────────────────────────────┘
                               │ buf generate
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│  Code Generators (build-time only, never in runtime graph)      │
│  • cmd/protoc-gen-protosource/ (protosourceify.go + *.gotext)   │
│    → *.protosource.pb.go (On, builders, EmitEvents, guards)     │
│    → *.protosource.lambda.pb.go (authz-wrapped handlers)        │
│    → *.protosource.wire.pb.go                                   │
│    → *<agg>mgr/ (interactive test CLIs)                          │
│  • cmd/protoc-gen-protosource-ts/ (parallel, some duplication)  │
│    → *.protosource.client.ts                                    │
└─────────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│  Runtime Core (tiny, stable, heavily interface-driven)          │
│  protosource.go: Repo, Store, Serializer, AggregateStore,       │
│  SnapshotTailStore, Projector, PostApplyHook, EventTTLer, ...   │
│                                                                   │
│  Repository.Apply (line 299) — fixed 9-step pipeline:           │
│    1. VersionValidator (lifecycle)                              │
│    2. ProtoValidater (protovalidate + CEL)                      │
│    3. StateGuard (state machine transitions)                    │
│    4. EventEmitter check (fail fast)                            │
│    5. CommandEvaluator (user hook, ErrSkip support)             │
│    6. EmitEvents                                                │
│    7. Persist (with optional compress + TTL stamping)           │
│    8. Best-effort AggregateStore materialization + AfterOn()    │
│    9. Best-effort Projector projections                         │
└─────────────────────────────────────────────────────────────────┘
                               │
               ┌───────────────┼───────────────┐
               ▼               ▼               ▼
        ┌────────────┐  ┌────────────┐  ┌────────────┐
        │  Stores    │  │  Adapters  │  │   Authz    │
        │ (memory,   │  │ (awslambda,│  │ (pluggable │
        │  dynamodb, │  │  httpstd)  │  │  Authorizer│
        │  cosmos,   │  │            │  │  + allowall│
        │  mysql) +  │  │            │  │            │
        │ opaquedata │  │            │  │            │
        │ (single-   │  │            │  │            │
        │  table +   │  │            │  │            │
        │  20 GSIs)  │  │            │  │            │
        └────────────┘  └────────────┘  └────────────┘
                               │
                               ▼
                        ┌────────────┐
                        │ TS Runtime │
                        │ @protosource/client │
                        └────────────┘
```

**Key observation from tectonic analysis:** The generator + example generated code forms a large "nexus" plate. Many framework files (adapters, authz, cmd plugins, aws/azure clients) appear as "drifters" relative to directory structure because they are pulled in via the example aggregates used for testing the generator. This is an artifact of the example-driven test strategy rather than a true layering violation.

---

## Evaluation Against Core Principles

### Separation of Concerns — Strong
- Generation is **strictly** a build-time concern (the plugins are never imported by running services).
- Runtime core has zero knowledge of protobuf descriptors or concrete store implementations beyond narrow interfaces.
- Stores know nothing about commands, authz, or HTTP.
- Authz is a single, trivially-replaceable seam called uniformly from all generated handlers.
- The only notable cross-cutting surface is the opaquedata/GSI annotation vocabulary that consumers must learn to get efficient secondary indexes.

### SOLID — Excellent (with one practical concentration of complexity)
- **DIP**: Pervasive. `Store`, `Serializer`, `Authorizer`, `Repo`, plus the family of optional `*er` interfaces (`StateGuard`, `CommandEvaluator`, `PostApplyHook`, `Projector`, `EventTTLer`) are all discovered by type assertion or interface satisfaction.
- **OCP**: The "generate the 95% + provide narrow, explicit hooks for the 5%" model is textbook Open/Closed. Consumers almost never edit generated files; they add `_derived.go`, `_evaluators.go`, etc.
- **SRP**: Pipeline steps are discrete concerns. Each generator responsibility (On emission, handler scaffolding, Wire DI, CLI) lives in its own template section.
- **ISP**: Fine-grained optional interfaces rather than one giant "everything" interface.
- **LSP**: Generated types satisfy the contracts the runtime expects.

**Caveat:** The generator (`protosourceify.go`) itself is a large procedural file (multiple 20–40 cyclomatic methods, high churn). This is the main place where complexity has concentrated as features (collections, projections, enum maps, TS parity, GSI collision handling) have been added.

### Scalability — Good for intended workloads
- Event log is the sole source of truth.
- Materialization and projections are explicitly **best-effort** and never block the write path (only `Warn` logging on failure).
- Snapshot + `SnapshotTailStore` support reduces replay cost for long-lived aggregates.
- Fixed 20 GSI slots + single-table opaquedata design gives flexible read patterns without per-aggregate tables or secondary index objects. Dynamo and Cosmos implementations maintain parity (Cosmos pays in cross-partition query cost).
- TTL, compression, and `EventTTLer` are first-class citizen features.
- Cross-cloud (AWS + Azure) parity is a deliberate and valuable architectural property.

**Limits (acknowledged):** 20 GSI ceiling, eventual-consistency window on materialized views, no built-in repair/reconciliation tool for drift, and the annotation surface for GSI/opaque design leaks some physical storage concerns into the domain model.

### Maintainability — B / Solid with known hotspots
**Strengths**
- Tiny, low-complexity, cycle-free runtime core.
- Explicit, narrow, reserved-name extension points (`AfterOn` is even a reserved method name).
- Outstanding self-documentation (`Claude.md` is genuinely one of the best contributor guides in the ecosystem).
- 0 dependency cycles and low average complexity give the codebase a "calm" feel.

**Hotspots & Debt**
- Generator churn (multiple methods in `protosourceify.go` appear in the top-5 hotspots with 24 commits in 90 days).
- Go vs TS plugin duplication of GSI/opaque/route logic (explicitly called out in `Claude.md` as a "sync warning").
- All generated `*.protosource.*.pb.go` files are committed (standard for protobuf ecosystems, but produces large noisy diffs on annotation or template changes).
- Example aggregates (`example/app/*`) serve dual duty as generator test fixtures and canonical usage samples → tight coupling.
- Many small `cmd/*/main.go` and `*mgr/main.go` test harnesses (the highest hotspot is the 80-cyclomatic `testmgr`).
- Build requires `go install` of the plugin binary into `$GOPATH/bin` (not just `go build`), plus clang-format, buf, wire, etc. Dockerfiles for the plugins help but the ceremony is non-trivial.
- Implicit contract between what the generator emits and what `Repository.Apply` (and the optional interfaces) expects. No machine-checked "generated code satisfies runtime contract" test beyond "generate + run the examples."

---

## Risks & Architectural Smells

1. **Generator is the complexity magnet** — high cognitive load and change surface for the most critical piece of the system.
2. **Go/TS plugin sync burden** — any new query type, handler feature, or GSI naming rule must be implemented (and kept identical) in two places.
3. **Committed generated code** — acceptable but creates upgrade friction for consumers and large PRs for framework authors.
4. **Example-driven testing** — powerful, but the "drifter" signal in tectonic analysis and the monster test CLIs are symptoms of this choice.
5. **Silent drift on best-effort views** — materialization/projection failures are only logged. No first-class repair path exists yet.
6. **Leaky storage abstraction** — the 20 GSI + opaque annotations require consumers to think about physical indexing.
7. **Toolchain ceremony** — the "install the plugin binary" requirement is the most common onboarding friction point.

---

## Prioritized Recommendations

| Priority | Recommendation | Impact | Effort | Notes |
|----------|----------------|--------|--------|-------|
| High | Factor shared annotation model, GSI/opaque logic, and template data structures between the two plugins (or add mechanical verification) | High (reduces duplication & drift) | Medium | Treat as first-class technical debt |
| High | Add `.jcodemunch.jsonc` `architecture.layers` definition and wire `get_layer_violations` into CI / `make lint` | High (prevents future layering erosion) | Low | Core vs generated vs stores vs adapters vs generators |
| Medium | Introduce a small intermediate representation between annotation parsing and code emission to shrink the monolithic `protosourceify.go` | Medium | Medium-High | Makes future language targets cheaper |
| Medium | Provide (or prominently document) a standard reconciliation/repair pattern or CLI for materialized views after best-effort write failures | Medium (operational safety) | Medium | Especially valuable for Cosmos |
| Low/Med | Add a `protosource doctor` / buf plugin step that detects stale generated files or hook signature drift after regeneration | Medium (devex) | Low | Nice quality-of-life win |
| Docs | Expand the existing "Generator Internals" knowledge (currently scattered in Claude.md and protosourceify.go comments) into a short dedicated design note | Low | Low | Helps future contributors |

---

## Overall Verdict

The architecture is **sound, principled, and well-matched to the problem statement** ("let domain experts declare event-sourced models in proto and get safe, authorized, queryable, snapshotting implementations for free").

The design successfully pushes almost all repetitive and error-prone code into a versioned, testable code generator while keeping a tiny, interface-driven, auditable runtime. Zero cycles, low complexity, strong dependency inversion, and explicit extension points are real structural wins.

The current B-grade health score is **not** a sign of distress in the core runtime or layering — it is almost entirely a reflection of the generator being the locus of active feature development and the test harnesses being large one-off CLIs.

With continued attention to generator modularity and Go/TS synchronization, the system has **excellent long-term maintainability** and can grow to additional languages, stores, and query patterns without the architecture collapsing under its own weight.

---

**Captured by:** Grok 4.3 architecture-reviewer persona + jcodemunch-assisted analysis  
**Next review trigger ideas:** after major generator refactor, addition of a 3rd language target, or introduction of layer rules in `.jcodemunch.jsonc`.
