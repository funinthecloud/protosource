# TODO

Cross-repo tracking for the protosource ecosystem. Items marked against sibling repos are links only — the work lives there.

## protosource (framework)

### Azure / Cosmos DB rollout (first client engagement)

Decisions locked:
- Cosmos NoSQL API (not Table API).
- Runtime: Azure Container Apps (scale-to-zero). Functions ruled out — Go runs as a custom handler, and cold starts on Consumption are 2–5s.
- Prototyping in personal Azure subscription; client's IaC stays separate.
- Cross-cloud is first-class — same proto/handler layer deploys to AWS Lambda + Azure Container Apps.

Step-by-step:
- [x] **Step 1.** `azure/cosmosclient` interface + `opaquedata/cosmos` OpaqueStore (Put/Get/Delete/Query, all 7 sort operators, GSI bounds, TTL filter, NA coercion). Unit-tested with a mock ContainerClient.
- [x] **Step 2.** `stores/cosmosdbstore` — `Save`/`Load`/`LoadTail`/`SaveAggregate` using `TransactionalBatch` for atomic per-aggregate event writes + `EnsureDatabase`/`EnsureContainers` (`DefaultTimeToLive = -1`). Wire provider set + `Events`/`AggregatesContainerClient` aliases. 22 unit tests with in-memory mock cosmos client, race-clean. Live emulator integration tests deferred to step 3.
- [x] **Step 3.** Wire provider set + `cmd/testcosmos` HTTP server + `cmd/testcosmos-setup` CLI. Lives on PR #80 alongside steps 1 & 2.
- [x] **Step 4.** `deploy/modules/cosmos-eventstore` — Cosmos account (serverless, Session consistency) + SQL database + events/aggregates containers (`/a` and `/pk` partition keys, `default_ttl = -1`) + data-plane RBAC scaffold (Cosmos DB Built-in Data Contributor at the database scope). `terraform validate` clean against `hashicorp/azurerm ~> 4.0`. Path follows the `deploy/modules/` convention from PR #79's migration plan.
- [x] **Step 5.** `deploy/modules/container-app-service` — User-assigned Managed Identity, ACR Basic with AcrPull RBAC, Log Analytics workspace (optional reuse), Container Apps environment, Container App with `revision_mode = Single`, external ingress, scale-to-zero defaults (0..3 replicas), `secret` blocks pulling values from Key Vault via the same identity. Principal ID outputs feed `cosmos-eventstore.data_contributor_principal_ids` directly. `terraform validate` clean against `hashicorp/azurerm ~> 4.0`.
- [x] **Step 6.** `deploy/bootstrap` (one-shot state backend: RG + Storage Account with versioning/soft-delete + tfstate container, local state by design) and `deploy/envs/azure-dev` (RG + container-app-service + cosmos-eventstore wired together, principal_id auto-flows into the Cosmos data-plane RBAC, Cosmos env vars auto-injected into the Container App). Cold-start instructions inlined as a header comment in `envs/azure-dev/main.tf`. `terraform validate` clean for both. `.gitignore` updated to track examples and ignore real `terraform.tfvars`.
- [x] **Step 7.** End-to-end pipe proven via `cmd/testcosmos` running on Azure Container Apps against a live Cosmos account — `curl $(tofu output -raw container_app_url)/test/v1/<id>` returns the domain 404 from the real handler stack.

### Framework gaps

- [ ] **Snapshot-aware event TTL.** Pre-snapshot events should get TTL while snapshots persist. Deferred — needs a triggered downstream process (DynamoDB Streams) to safely mark pre-snapshot events with TTL only after confirming the snapshot exists. Writing TTL proactively risks data loss if the snapshot does not arrive.
- [ ] **Multi-aggregate projections.** Projections that join across aggregate types (e.g. `Order + Customer → OrderWithCustomerView`). Likely event-driven via DynamoDB Streams rather than synchronous in the pipeline.
- [ ] **Evaluate supporting RS256 / ES256 on the `authz.Authorizer` side.** Not strictly a framework change — the Authorizer is pluggable — but the handler template currently hard-codes no algorithm awareness. If a future consumer needs signing-algorithm routing at the handler level (e.g. per-aggregate audience), revisit.

### Nice-to-have polish

- [ ] **httpauthz retry logic.** A concrete future client (lives in protosource-auth) could benefit from exponential-backoff retries on 5xx. Out of scope for the framework itself but worth linking.
- [ ] **Plugin integration test** that asserts every `isCommand` handler path emits the new `actor := authz.UserIDFromContext(ctx)` line. Currently covered indirectly by `authz/handler_integration_test.go`.

### Done ✓

- [x] Single-aggregate projections (PR #23)
- [x] Nested collections with `map<string, Message>` + ADD/REMOVE via `collection` annotation (PR #24, #25)
- [x] Wire-friendly provider sets + shared `dynamoclient` / `opaquedata` infra (PR #35)
- [x] TypeScript client generation (`protoc-gen-protosource-ts` + `@protosource/client`)
- [x] Go client generation (`client.gotext` → `*.protosource.client.pb.go`) — typed HTTP client extracted from the `*mgr` CLI template, importable by other Go apps
- [x] Showcase app: [`todoapp`](https://github.com/funinthecloud/todoapp) ships both `backend-bolt` and `backend-lambda` + a React frontend
- [x] `authz.Authorizer` interface + generated handler integration (PR #64)
- [x] Actor resolution prefers `authz.UserIDFromContext(ctx)` over `request.Actor` (PR #65, v0.1.3)
- [x] Unknown authorizer errors → 503 `AUTHZ_UNAVAILABLE` instead of 403 (PR #65, v0.1.3)
- [x] Exported `dynamodbstore.EnsureTables` + `NumGSIs` for library use (PR #68, v0.1.5) — downstream projects import instead of duplicating table creation logic. Tables created with deletion protection, PITR, TTL.

## protosource-auth

Tracked in its own TODO section of [`protosource-auth/CLAUDE.md`](https://github.com/funinthecloud/protosource-auth/blob/main/CLAUDE.md). Highlights that affect the framework:

- [ ] **JWKS endpoint** for offline JWT verification — would let downstream consumers skip the per-request `/authz/check` round-trip for JWT validation. Needs a follow-up plugin change to stamp JWKS URLs into generated handlers.
- [ ] **Multi-aggregate projection for the user→function cache.** Waiting on framework-side multi-aggregate projections (above).

## todoapp

- [ ] **Switch to `directauthz` in-process authorizer.** The Lambda backend currently uses `httpauthz.New(url)` for HTTP-based auth. Switch to `directauthz.New(checker)` using shared DynamoDB tables — eliminates the network hop.
- [ ] **Frontend shadow-token integration.** The React frontend still sends `X-Actor`. Update it to POST `/login` against protosource-auth, store the shadow token (cookie or localStorage), and send `Authorization: Bearer <token>` on every request.
- [x] **Deploy the auth service alongside the lambda backend.** protosource-auth now ships as a Lambda (`cmd/protosource-auth-lambda`) with its own SAM template.
