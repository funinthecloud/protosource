# Architecture & Code Review — protosource
**Date:** 2026-07-01  
**Reviewer:** Grok (with jcodemunch + subagent support)  
**Scope:** Full repository + explicit focus on documentation accuracy vs. implementation reality  
**Git state:** Clean on main (at start of review)  
**Index:** local/protosource-0113109d (547 files, 8187 symbols, indexed ~4 days prior; contains worktree copies that pollute some signals)

---

## Agent Collection Summary

Subagents were launched for parallel deep review:
- `performance-profiler`: Completed (detailed replay, clone, serialization, materialization, and GSI analysis).
- `bug-finder`: Completed (5 concrete issues with file/line evidence).
- `architecture-reviewer`: Multiple attempts hit "doom loop detected (exploratory stagnation)" or long stagnation with zero output (one ran 24min+ with 5 errors; fresh spawn cancelled after ~20s). No architecture-specific report produced.
- `structural-completeness-reviewer`: Initial attempts failed to register or stalled; the final one (019f1e11-adc5-7300-b996-23048d0302dc) completed successfully after ~102s (68 tool calls) and delivered a detailed report focused on docs-vs-implementation and structural gaps.

All quantitative metrics below were also gathered directly via jcodemunch (`get_repo_health`, `get_hotspots`, `get_dependency_cycles`, `find_dead_code`, `get_tectonic_map`, etc.) plus direct reads of code, generated handlers, docs, workflows, and test runs.

The structural report is reproduced in full below for completeness.

**Key prior artifacts referenced:**
- `ARCHITECTURE_REVIEW.md` (May 2026)
- `docs/architecture-review-2026-05.md`
- `TODO.md`, `CLAUDE.md`, `CHANGELOG.md`, `docs/*.md`
- Existing `gen/` artifacts (the "source of truth" for what the generators actually emit today)

---

## Overall Health (jcodemunch, 90-day window)

- Files: 547 | Symbols: 8187 | Fn/Methods: ~6831
- Average cyclomatic complexity: **2.51** (excellent)
- Dependency cycles: **0**
- Dead code: **1.5%** (100 symbols) — improvement from ~4.4% in the May review
- Unstable modules: **6**
- Composite repo health: **91.9 (A grade)**
  - Strong: complexity (100), dead code (94), cycles (100), test gap (100)
  - Weakest: churn_surface (60) — driven almost entirely by the generator

**Top Hotspots** (complexity × churn):
1. `cmd/protoc-gen-protosource/protosourceify.go::validateCollectionMapping` (cyclo 42, churn 14)
2. `::generate` (29, 14)
3. `::validateFileStructure` / `::validateOpaqueAnnotations` (28 each)
4. Generated mgr mains (`ordermgr/main.go` cyclo 62, `testmgr/main.go` 80)
5. Other generator validators + core `Apply` (cyclo 28 but lower churn)

The generator (`protosourceify.go`) accounts for the majority of high-risk surface.

---

## Architecture Assessment

### Strengths (unchanged from May review and confirmed now)
- Deliberately small, interface-driven runtime core (`protosource.go`).
- Excellent DIP: `Store`, `AggregateStore`, `SnapshotTailStore`, `Serializer`, `Authorizer`, hooks, etc.
- True cross-cloud parity (DynamoDB + CosmosDB opaquedata + multiple store implementations) with consistent single-table + 20-GSI design.
- Authorization is a first-class, stamped cross-cut (every command + read path).
- Content negotiation (protobuf binary default) is consistent end-to-end.
- 0 cycles + clean separation between core / stores / authz / adapters / codegen / deploy / ts.
- Powerful extension points that actually work (`Evaluate`, `GuardState`, `AfterOn`, collection annotations, `sets_state`, `produces_events`, snapshots).

### Weaknesses & Debt (still the same, now with fresh numbers)
- **Generator is the bottleneck** (monolithic `protosourceify.go` + copied logic in the `-ts` plugin). High cyclo + recent churn = fragility.
- Committed generated code + `*mgr` CLIs create "example gravity" and complicate analysis (tectonic map is noisy; many dead-code signals are generated files).
- Worktree copies inside `.claude/worktrees/` pollute tectonic, dead-code, and some health signals.
- No formal layer rules (`.jcodemunch.jsonc` absent; `get_layer_violations` returns 0 because nothing is defined).
- Hard 20-GSI limit is everywhere.
- Dual role of `example/app/` (both documentation surface and test harness).

Tectonic map produced 19 plates but was dominated by the two worktree clones. Main source structure is reasonable but not perfectly modularized in the signals.

---

## Bugs & Issues (from bug-finder subagent + direct verification)

### Critical
1. **Apply silently swallows all load errors** (`protosource.go:314`)
   - `if err != nil { aggregate = r.new() }` treats deserialization failure, store outage, or corrupted record the same as "not found".
   - A real aggregate can be treated as brand new during a mutation command.

2. **MemoryStore returns shared mutable History** (`stores/memorystore/memorystore.go`)
   - `Load` returns the internal pointer; `Save` does in-place `append`.
   - Concurrent (or even sequential retained) callers see mutation. Boltdbstore copies; memorystore does not.

### Warnings
3. **Authorizer docstring lies** (`authz/authorizer.go`)
   - Claims unknown errors → `ErrForbidden` (403).
   - Actual generated `authzErrorResponse` + tests + CLAUDE.md correctly map to 503 `AUTHZ_UNAVAILABLE`.

4. **HTTP adapter bypasses content negotiation on early errors** (`adapters/httpstandard/adapter.go`)
   - Read failures hardcode JSON. Generated paths respect `Accept`/`Content-Type` and use `apierror.v1.Error`.

### Suggestion
5. Materialization path re-applies the just-emitted events (`protosource.go` after Save).
   - Extra `On` + `AfterOn` work. Usually harmless but extra CPU and non-idempotency surface.

These are concrete and evidenced. Tests currently pass because they are mostly happy-path and single-threaded.

---

## Documentation vs. Reality (Primary User Concern — Confirmed)

This was a major focus. Multiple high-visibility instructions do **not** match generated code or current contracts.

**Critical mismatches:**
- **NewHandler signature** (quickstart.md, deployment.md, consumer-guide.md):
  - Docs: `NewHandler(repo, nil)` or `NewHandler(repo, NewClient(...))` (2 args).
  - Reality (all generated `*.protosource.lambda.pb.go`): `NewHandler(repo, client *XxxClient, authorizer authz.Authorizer)`.
  - `authorizer` is now **required** (panics with clear message if nil). Client can be nil for non-query paths, but the call site is wrong.
- `buf.gen.local.yaml` referenced repeatedly in CLAUDE.md for generator development. Does not exist.
- Release workflow names in CLAUDE.md are inconsistent (`release-binaries.yml` vs `release.yml`). Both files exist and do different things.
- README.md is vestigial — no build, no usage, no links to `docs/`.
- Deployment/consumer examples show incomplete wiring post the authorizer hardening.
- Recent changes (mandatory authorizer in handlers, error body content-negotiation via `apierror.v1`, TS ESM requirements) are in code/CHANGELOG but not reflected in usage docs.

**Verified good:**
- `go build ./...`, full `go test ./...`, `buf generate` (with plugin install) all succeed.
- CI (`ci.yml`) does exactly `go install ./cmd/... && buf generate`.
- `.goreleaser.yaml` matches the combined-archive release description.
- Core pipeline docs (pipeline.md, proto-annotations.md, collections.md) are largely accurate.

The drift is mostly around the "how do I actually stand up a handler today?" surface, which changed when authz became non-optional.

---

## Performance Characteristics (from dedicated profiler)

- Every `Apply` and `Load` performs (tail) replay.
- Snapshot emission does unconditional `proto.Clone` (sometimes 2× per command) + `On` + `AfterOn` on the clone.
- Serialization: anypb wrapping + marshal on every event; unconditional decompress in History/Load paths.
- Materialization is synchronous + best-effort (errors only logged).
- Cosmos GSI queries pay cross-partition costs + client-side sorting in some cases.
- Collection-derived fields in `AfterOn` are O(n) on every materialization/snapshot boundary.
- `Apply` itself is already cyclo-28.

Recommendations from the profiler (replay caching, guarded clones, HistoryMeta API, incremental derived fields, offloaded materialization, etc.) remain valid.

---

## Other Observations
- Worktree copies inside the tree are hurting signal quality (recommend scoping future jcodemunch analyses or cleaning them for review runs).
- Generated `*mgr` mains are intentionally "large" (they have to handle every command shape) but are the highest-cyclo outliers.
- Cosmos rollout (per TODO.md) appears largely complete in code and deploy modules.
- Framework lifts (host, keyproviders, etc.) are still pending as tracked.

---

## Prioritized Action Plan

### P0 — Immediate (Correctness + User-Facing Docs)
- Fix the two critical bugs:
  - `protosource.go`: Only fall back to `r.new()` on genuine `ErrAggregateNotFound` (or equivalent empty-history case). Propagate other load errors.
  - `memorystore`: Deep copy on `Load` (use `proto.Clone` like other stores).
- Update documentation for the documented usage instructions:
  - Fix every `NewHandler` example (include authorizer, note client may be nil).
  - Remove references to nonexistent `buf.gen.local.yaml`.
  - Align CLAUDE.md release workflow names and add a small "manual wiring" runnable snippet.
  - Improve README.md with links and a one-paragraph "what you actually run" section.
- Fix the stale docstring in `authz/authorizer.go`.
- Decide on httpstandard early-error behavior (negotiate or document the exception).

### P1 — Short Term (Maintainability + Gaps)
- Add tests for the newly identified scenarios (load failures in Apply, concurrent History+Apply on memorystore, authz unknown-error path, adapter early errors).
- Extract or strongly sync the duplicated GSI/opaque logic between the two `protosourceify.go` files.
- Add `.jcodemunch.jsonc` with layer rules so `get_layer_violations` becomes useful.
- Create a docs-smoke test or example that actually builds the quickstart code as written.
- Clean up or document the worktree pollution for future automated reviews.

### P2 — Medium Term (Performance + Structure)
- Implement profiler recommendations (guard clones, snapshot sidecar or current-state cache, HistoryMeta API, incremental AfterOn for collections, offload materialization).
- Centralize the 20-GSI constant and enforce at codegen time.
- Revisit the dual role of `example/app/` (heavy generated CLIs vs. clean documentation).
- Consider making materialization async or bounded.

### Ongoing
- Treat the generator as the highest-churn/risk area — any feature work should include cross-plugin tests.
- Re-run a similar review (or targeted follow-up) after P0/P1.
- Keep TODO.md and ADRs up to date with findings (e.g., authz contract hardening impact).

---

## Recommended Next Steps

1. Prioritize and implement the P0 bug fixes + the NewHandler/docs updates (these directly address the user's stated concern about "usage instructions do not reflect reality").
2. Write the critical bug fixes behind tests.
3. Optionally produce a follow-up ADR for the authz + handler construction changes if they represent a deliberate breaking evolution.
4. Re-index without (or with explicit ignore of) the `.claude/worktrees` for cleaner future signals.

Raw outputs from the completed subagents, jcodemunch health/hotspot/cycle data, and all prior review documents are available in the session context if deeper raw text is needed.

This review combines agent findings, fresh metrics, and direct reality checks into one picture. The core is healthy (A-grade, 0 cycles); the generator and recent authz hardening are the areas where documentation and implementation have diverged, and two real correctness bugs exist in hot paths.

---

## Structural Completeness Review (from subagent 019f1e11-adc5-7300-b996-23048d0302dc)

**Structural Completeness Review — protosource**

**Date:** 2026-07-01  
**Scope:** Documentation vs. implementation reality; missing/incomplete features; stubs; dead code; interface mismatches; unhandled paths.

### ✅ Clean Removals
No obvious dead code from prior refactors in core runtime. Generated artifacts dominate "dead" signals (expected). One minor: `SnapshotAwareStore` appears only in stale godoc comments in `protosource.go:465-466` (and worktree copies) — the actual interface is `SnapshotTailStore`. No removal needed, just comment fix.

### ❌ Complete Changes — FAIL (multiple mismatches)

#### 1. NewHandler signature — documentation does not match generated code
**Docs claim 2-arg form; reality is 3-arg with required authorizer.**

- `docs/quickstart.md:307`:
  ```go
  handler := taskv1.NewHandler(repo, nil)
  ```
  Note at 325: *"We pass `nil` for the second argument to `NewHandler` because GSI queries require DynamoDB. Commands, Load, and History all work with memorystore."*

- `docs/deployment.md:145,173`:
  ```go
  handler := taskv1.NewHandler(repo, taskv1.NewTaskClient(opaqueStore))
  ```

- `docs/consumer-guide.md:299` mentions `thingv1.NewHandler` without showing signature.

- **Reality** (all generated `*.protosource.lambda.pb.go`):
  ```go
  func NewHandler(repo Repo, client *SampleClient, authorizer authz.Authorizer) *Handler {
      if authorizer == nil {
          panic("samplev1.NewHandler: authorizer must not be nil (use allowall.Authorizer{} for no enforcement)")
      }
      return &Handler{repo: repo, client: client, authorizer: authorizer}
  }
  ```
  Same pattern in `order_v1`, `test_v1`, `samplenosnapshot_v1`.

- **HandleGet unconditionally dereferences client** (`sample_v1.protosource.lambda.pb.go:200`):
  ```go
  aggregate, err := h.client.GetSample(ctx, id)
  ```
  Passing `nil` client → panic on `GET /{id}` (materialized read path). Load/History use `repo` and would work; Get does not.

- **Wire providers do not include handler construction**:
  - `cmd/protoc-gen-protosource/content/wire.gotext` emits only `ProvideRepository` + `ProviderSet`.
  - `gen/example/app/sample/v1/sample_v1.protosource.wire.pb.go:20-24` confirms.
  - `docs/consumer-guide.md:223` claims: `"*.protosource.wire.pb.go` — `ProviderSet`, `NewRepository`, `NewHandler"` — **false**.

#### 2. Authorizer is mandatory; docs don't reflect
- Generated `NewHandler` panics on nil authorizer.
- Quickstart/deployment examples pass no authorizer at all.

#### 3. Store interface documentation drift (multiple CLAUDE.md files)
These document the **old** `AggregateStore`/`LoadAggregate` signatures (e.g. in firestorestore, mssqlstore, postgresqlstore CLAUDE.md files). None of these stores exist as full implementations yet.

#### 4. Incomplete store implementations
- `memorystore` implements only `Store`. No `AggregateStore`, no `SnapshotTailStore`.
- `mysqlstore` implements only `Store`.
- `boltdbstore`, `dynamodbstore`, `cosmosdbstore` implement fuller sets.

#### 5. Adapters bypass content negotiation on early errors
Hardcoded JSON in httpstandard and awslambda adapters for read errors, unlike generated paths.

#### 6. Apply silently swallows non-"not found" load errors
`protosource.go:312-315`: any load err → `r.new()`.

#### 7. MemoryStore returns shared mutable state
Returns internal history; Save appends in place.

#### 8. "SnapshotAwareStore" mentioned but does not exist
Comment drift; interface is `SnapshotTailStore`.

### ✅ No Dev Artifacts
No obvious stubs/panics in production paths. TODOs centralized in TODO.md / CLAUDE.md.

### ⚠️ Dependencies Clean
Minor notes only.

### ❌ Configs Updated — FAIL
- `buf.gen.local.yaml` referenced in CLAUDE.md but does not exist.
- Release workflow naming inconsistent in CLAUDE.md.
- README.md vestigial.

### Critical Issues (blocking / user-facing)
1. Quickstart is un-runnable as written (NewHandler + will panic on Get).
2. Authorizer mandatory but examples omit it.
3. Apply can resurrect corrupt aggregates.
4. Memorystore unsafe for concurrent use.

### Technical Debt Risks
- Store CLAUDE.mds describe old interfaces.
- Adapter early errors are permanent exceptions.
- Wire does not emit NewHandler.
- Snapshot emission assumes >=1 event.
- Memorystore skips materialization paths.

**Summary Checklist** in subagent output: several FAILs on complete changes and configs, focused on docs drift from authz hardening and materialized client param.

---

## Combined Prioritized Action Plan

**P0 (this week — correctness + your primary docs concern)**
- Fix the two critical runtime bugs (Apply load error swallowing; MemoryStore deep-copy on Load).
- Fix all NewHandler examples and notes in quickstart.md, deployment.md, consumer-guide.md (3-arg form + authorizer required + nil client semantics for Get).
- Update CLAUDE.md: remove buf.gen.local.yaml refs, align release workflow names, fix store interface godoc/comments.
- Improve root README with build + minimal working example.
- Fix the authz docstring mismatch and SnapshotAwareStore comment.
- Decide on adapter early-error handling.

**P1 (next)**
- Add regression tests for the above (load failures, concurrent memorystore, authorizer nil, Get with nil client).
- Add `.jcodemunch.jsonc` layer rules.
- Fix Wire template/docs claim about NewHandler (or implement provider if desired).
- Update the various stores/*/CLAUDE.md to match current AggregateStore/SnapshotTailStore interfaces (or mark as aspirational).

**P2 (structural/perf)**
- Give memorystore minimal AggregateStore + Snapshot support or document its limits.
- Address perf hotspots from the profiler agent (clones, replay costs, etc.).
- Clean generator duplication risk.
- Re-review after fixes (including re-index excluding worktrees).

This gives us feedback from the completed structural agent + prior agents + direct jcodemunch work. The architecture-specific agents unfortunately produced nothing due to stagnation. The picture is now quite complete. 

---

## Additional Long-Running Structural Review (ID 019f1df7-e632-7fb1-961f-22ab270a1847 — completed after 40min, 105 calls)

**Structural Completeness Review: protosource**

**Repo:** `local/protosource-0113109d` (8187 symbols, 547 files)

### Clean Removals
Pass. No obvious dead code from replaced implementations found in Go source. No TODO/FIXME/HACK comments in any `.go` files (search returned 0 matches). No commented-out code blocks detected.

### Complete Changes
Pass with caveats. Core pipeline, authz, codegen, and primary stores (DynamoDB/Cosmos) appear structurally complete and internally consistent.

### No Dev Artifacts
Pass. Clean codebase.

### Critical Issues (blocking or debt-inducing)

1. **MySQLStore is incomplete and partially orphaned** (debt-inducing)
   - Implements only `Store`, not `AggregateStore` or `SnapshotTailStore`.
   - `SnapshotInterval() int64` but `Snapshoter` requires `int32` (type mismatch).
   - Never referenced outside its directory.
   - No TTL handling at all (ignores `record.Ttl`).
   - Save uses plain INSERT, no version conflict detection/transactions.

2. **Apply() documentation vs implementation mismatch**
   - Docs claim 7-step pipeline.
   - Actual: 9 steps (includes Materialize + PostApplyHook + Project).
   - Godoc is stale.

3. **MySQLStore TTL handling missing entirely**
   - No read of Ttl on Save, no filter on Load, no tests/options. Problematic for `event_ttl_seconds` ephemeral aggregates.

### Technical Debt Risks

- Store parity gaps (table in output): only Dynamo/Cosmos are full-featured. Memory/Bolt/MySQL partial. MySQL's SnapshotInterval suggests abandoned intent for snapshots.
- No LoadAggregate read path (materialized state is write-only; Load always replays events). Documented as future.
- Singular embedded validation is codegen-time only; generated On() has no runtime guard (silent no-op risk if bypassing generation).
- TS/Go GSI sync is manual risk (per CLAUDE.md warning; no enforcement).

### Docs vs Code Claims Checked
Many verified correct (AfterOn points, GuardState, extractors, UserIDFromContext, ViaGSI, Cosmos TTL, AutoPKSK, collection rules, etc.).

### Summary
- Blocking: None
- Debt-inducing: 3 (MySQLStore, Apply doc, MySQL TTL)
- Lower-risk: 4 (parity, no LoadAggregate, validation surface, TS/Go sync)
- Recommendation: Complete/mark/remove MySQLStore; update Apply godoc.

---

**Final Synthesis Across All Sources (perf-profiler, bug-finder, two structural reviews, direct jcodemunch + doc audits, prior May review):**

**Strengths:**
- Core is small, clean, correct for claimed features.
- 0 cycles, strong A-grade health, low dead code.
- Good parity for main stores (Dynamo/Cosmos full).
- Authz and content negotiation contracts mostly solid.
- Many docs claims in pipeline/annotations/collections are accurate.

**Key Divergences & Issues (docs vs reality + structural gaps):**
- **Usage docs badly out of date on NewHandler/authorizer** (quickstart etc. show 2-arg or nil; reality 3-arg + mandatory + Get uses client).
- Apply swallows errors + godoc wrong (7 vs 9 steps).
- MemoryStore returns mutable shared state.
- MySQLStore is the standout incomplete/orphan (type mismatch, no full interfaces, no TTL, unreferenced).
- Adapters bypass content negotiation for early errors.
- Multiple CLAUDE.md + store docs have stale signatures and claims.
- `buf.gen.local.yaml` doesn't exist but is referenced.
- Materialized state is write-only (no LoadAggregate).
- Generator is the hotspot (and TS/Go sync is manual).
- Worktree noise pollutes some signals.

The two architecture-reviewer attempts produced no feedback (doom loops/stagnation/cancelled). The structural ones (one delayed, one very long) + direct work gave excellent coverage.

**Comprehensive Prioritized Plan:**

**P0 - Fix user-facing docs + critical runtime bugs (do immediately):**
- Update NewHandler examples in quickstart.md, deployment.md, consumer-guide.md + add working minimal example.
- Fix Apply() godoc.
- Implement the two critical bugs fixes (Apply error handling, MemoryStore copy).
- Clean CLAUDE.md references (buf.gen.local, workflows, store interfaces, SnapshotAwareStore).
- Fix root README.
- Decide/fix adapters early error paths.

**P1 - Debt cleanup:**
- Decide on MySQLStore (complete to parity with int32 fix + TTL + AggregateStore/SnapshotTailStore + tx, or deprecate/remove, or mark experimental).
- Add tests for the fixed issues + Get with nil client + MySQL gaps.
- Add .jcodemunch.jsonc layers.
- Update/fix store CLAUDE.md files across the board.
- Fix Wire template claim or implement NewHandler provider.

**P2 - Structural/Perf/Generator:**
- Address store parity (document or implement for memory/bolt/mysql).
- Perf improvements (from profiler: reduce clones/replay costs, async materialization?).
- Generator: reduce hotspots in protosourceify.go, add sync test for TS/Go GSI.
- Consider adding LoadAggregate path and runtime guard for singular embeds.
- Re-index without worktrees for cleaner future reviews.

**P3:**
- Full docs refresh pass.
- Consider MySQL or other partial stores if needed for cross-cloud.

All agent outputs are now incorporated (or noted as missing for arch). The review file is updated with both structural reports.

Ready to start fixing P0 items? Which one first? (I recommend NewHandler docs + the two runtime bugs.)

---

## Follow-up: Cleanups performed (2026-07-01)

- Eliminated empty `stores/mssqlstore/` (spec-only shell with zero references/implementations).
- Removed all `*mgr` cruft (issue #102): deleted `cli.gotext`, pruned CLI generation paths/helpers/tests/testdata from `protoc-gen-protosource`, deleted all committed `gen/**/ *mgr/` directories and binaries. Updated docs (quickstart, consumer-guide, collections, CLAUDE.md) and generator.
- P0 progress (parallel):
  - Fixed `protosource.Apply`: now only falls back to `new()` aggregate on genuine `ErrAggregateNotFound`; other load errors (corrupt, store, deserialize) are propagated.
  - Fixed `memorystore.Load`: returns `proto.Clone` of history (no shared mutable state).
  - Updated `NewHandler` examples + notes in quickstart.md, deployment.md, consumer-guide.md (3-arg form with required authorizer; nil-client semantics documented; switched quickstart read example to `/load/` path).
  - Godoc for `Apply` updated (7→9 steps).
  - Cleaned stale `buf.gen.local.yaml` and release workflow references in CLAUDE.md.
  - Updated root README with build + docs pointers.
  - Fixed authorizer package docstring to match actual 503 mapping in generated handlers.
  - Minor doc fixes (New(0) for current memorystore ctor).
- All changes: `go build ./...` + relevant + full `go test ./...` clean (32s).
- No `*mgr` directories remain under `gen/`. Future `buf generate` will not emit them.

Remaining P0 items (from plan): adapter early-error negotiation, more tests, README polish depth, full store CLAUDE updates.
