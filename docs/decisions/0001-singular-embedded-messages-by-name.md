# 0001. Singular embedded message fields applied by name, not by inference

- Status: Accepted
- Date: 2026-06-25

## Context

Aggregates already support two ways of populating fields in the generated
`On(event)` method: scalar fields are copied by matching field **name**
(`aggregate.Foo = e.GetFoo()`), and collection (`map<string, Message>`) fields
use an explicit `collection:` annotation (ADD/REMOVE, target, key_field).

There was no story for a **singular** (non-collection) embedded message field on
an aggregate — e.g. `OidcConfig oidc = 13` on an `Issuer`. When an event carried
such a sub-message, `On()` emitted only `setModified(e)` and never assigned the
field, forcing consumers (protosource-auth's OIDC federation, GH#96) to hand-edit
generated files — a patch `buf generate` clobbers in CI.

A first implementation (PR #97, later reverted) matched the event's embed to the
aggregate field by message **type** and inferred the assignment, plus a
name-suffix heuristic for "clear" events. Review and a generation experiment then
showed the existing scalar path already handles a singular embed when the event
field name matches the aggregate field — and that the inference was redundant.

## Decision

Apply singular embedded message fields by the **same by-name mechanism used for
scalars**. The convention: name the event's embedded field to match the aggregate
field. A "set" event carries the populated message; a "clear" event carries the
same-named field left empty, and `On()`'s unconditional copy nils it. No new
annotation, no type inference, no template branch for the set/clear itself.

Two supporting pieces were added:
- `validateSingularEmbed` fails codegen when an event carries an embedded message
  of a type present on the aggregate but under a **different** field name (the
  silent no-op that was the original GH#96 symptom), with a rename hint.
- `commandEventArg` lets `EmitEvents` pass a zero value when a command lacks an
  event field, so a clear command (no embed) can emit a clear event (empty embed).

## Rejected alternatives

- **Type-based inference (the original PR #97 approach)** — rejected because
  matching by message type is a non-unique key: an aggregate with two fields of
  the same message type cannot be disambiguated, so the generator had to *refuse*
  those cases, and it emitted a duplicate assignment alongside the scalar copy. It
  existed only to tolerate divergent field names (`config` vs `oidc`), which is
  not a requirement — names can simply be made to match.
- **Explicit `SingularMessageMapping`/`target:` annotation (parallel to
  `collection:`)** — rejected because it adds annotation surface and per-event
  ceremony to express something the by-name scalar copy already does for free.
  Reconsider only if a real need arises to route an embed to a differently-named
  aggregate field (e.g. a single event setting one of two same-typed fields).
- **Keep inference but fix its warts** — rejected because it preserves the
  unprincipled middle: still type-keyed, still ambiguous, still needs a clear
  heuristic, for no benefit over by-name.

## Consequences

- Easier: singular embeds need zero framework code on the `On()` side; the
  ambiguity class disappears (field names are unique by construction); two
  same-typed fields are trivially distinguished by name.
- Harder / risk: "clear" is opt-in by convention — an event must carry an empty
  same-named field, and `validateSingularEmbed` *cannot* enforce that a clear
  event was written (an event that omits the field is indistinguishable from one
  that intentionally doesn't touch it). This is documented, not validated.
- Load-bearing: the set/clear behavior depends on the generated copy being
  **unconditional**. `TestBilling_SetThenClear` (order example) guards it through
  Apply→Load so a future nil-guard "optimization" can't silently break clears.
- Cross-repo: protosource-auth must rename `OIDCConfigSet.config` → `oidc` and add
  an empty `OIDCConfig oidc` to `OIDCConfigCleared`; the validator turns the first
  into a build failure (good), but not the second.
