# TODO

Cross-repo tracking for the protosource ecosystem. Items marked against sibling repos are links only — the work lives there.

## protosource (framework)

### Framework gaps

- [ ] **Snapshot-aware event TTL.** Pre-snapshot events should get TTL while snapshots persist. Deferred — needs a triggered downstream process (DynamoDB Streams) to safely mark pre-snapshot events with TTL only after confirming the snapshot exists. Writing TTL proactively risks data loss if the snapshot does not arrive.
- [ ] **Multi-aggregate projections.** Projections that join across aggregate types (e.g. `Order + Customer → OrderWithCustomerView`). Likely event-driven via DynamoDB Streams rather than synchronous in the pipeline.
- [ ] **Extract Go client library from `*mgr` CLI commands.** The generated `cli.gotext` currently inlines HTTP logic in a standalone `main.go`. Extract the request/response handling into a generated client package importable by other Go applications — mirrors the existing `protoc-gen-protosource-ts` client.
- [ ] **Evaluate supporting RS256 / ES256 on the `authz.Authorizer` side.** Not strictly a framework change — the Authorizer is pluggable — but the handler template currently hard-codes no algorithm awareness. If a future consumer needs signing-algorithm routing at the handler level (e.g. per-aggregate audience), revisit.

### Nice-to-have polish

- [ ] **httpauthz retry logic.** A concrete future client (lives in protosource-auth) could benefit from exponential-backoff retries on 5xx. Out of scope for the framework itself but worth linking.
- [ ] **Plugin integration test** that asserts every `isCommand` handler path emits the new `actor := authz.UserIDFromContext(ctx)` line. Currently covered indirectly by `authz/handler_integration_test.go`.

### Done ✓

- [x] Single-aggregate projections (PR #23)
- [x] Nested collections with `map<string, Message>` + ADD/REMOVE via `collection` annotation (PR #24, #25)
- [x] Wire-friendly provider sets + shared `dynamoclient` / `opaquedata` infra (PR #35)
- [x] TypeScript client generation (`protoc-gen-protosource-ts` + `@protosource/client`)
- [x] Showcase app: [`todoapp`](https://github.com/funinthecloud/todoapp) ships both `backend-bolt` and `backend-lambda` + a React frontend
- [x] `authz.Authorizer` interface + generated handler integration (PR #64)
- [x] Actor resolution prefers `authz.UserIDFromContext(ctx)` over `request.Actor` (PR #65, v0.1.3)
- [x] Unknown authorizer errors → 503 `AUTHZ_UNAVAILABLE` instead of 403 (PR #65, v0.1.3)

## protosource-auth

Tracked in its own TODO section of [`protosource-auth/CLAUDE.md`](https://github.com/funinthecloud/protosource-auth/blob/main/CLAUDE.md). Highlights that affect the framework:

- [ ] **JWKS endpoint** for offline JWT verification — would let downstream consumers skip the per-request `/authz/check` round-trip for JWT validation. Needs a follow-up plugin change to stamp JWKS URLs into generated handlers.
- [ ] **Multi-aggregate projection for the user→function cache.** Waiting on framework-side multi-aggregate projections (above).

## todoapp

- [ ] **Frontend shadow-token integration.** The React frontend still sends `X-Actor`. Update it to POST `/login` against protosource-auth, store the shadow token, and send `Authorization: Bearer <token>` on every request.
- [ ] **Deploy the auth service alongside the lambda backend.** Today `protosource-auth` runs as a standalone binary; adding a lambda deployment target for the auth service itself would let todoapp deploy both in one stack.
