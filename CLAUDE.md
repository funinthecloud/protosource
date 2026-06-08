# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

# protosource

An event sourcing framework where domain models are defined entirely in protocol buffers. A buf plugin generates all Go boilerplate — including the aggregate's `On` method, builders, event emission, snapshot support, lifecycle validation, state transitions, and authorization — so domain logic is expressed purely through proto annotations.

## Build & Run

```bash
go generate ./...                        # install tools (buf, wire, protoc-gen-go)
go install ./cmd/protoc-gen-protosource  # IMPORTANT: install plugin to $GOPATH/bin so buf can find it
buf generate                             # generate Go code from proto/
go build ./...                           # build everything
go test ./...                            # run tests
go test ./stores/memorystore/...         # run tests for a single package
go test -run TestApply ./...             # run a specific test by name
go vet ./...                             # static analysis
```

**IMPORTANT**: When you are actively modifying the plugin (`protosourceify.go` or the `*.gotext` templates), you must:
- run `go install ./cmd/protoc-gen-protosource` (and the `-ts` equivalent)

Normal development of applications that *use* protosource no longer requires this step. See the "Developing the code generator itself" section below.

### TypeScript Client Generation

```bash
go install ./cmd/protoc-gen-protosource-ts  # install TS plugin to $GOPATH/bin
cd ts/client && npm install && cd -          # protoc-gen-es lives in ts/client/node_modules
buf generate --template buf.gen.ts.yaml      # generate framework TS runtime types
cd ts/client && npm run build                # build @protosource/client runtime
```

Go generation (`buf.gen.yaml`) and TypeScript generation (`buf.gen.ts.yaml`) are **two separate `buf generate` invocations**. `buf.gen.ts.yaml` runs `protoc-gen-es` + `protoc-gen-protosource-ts` and is scoped via `inputs.paths` to the framework's own protos (`funinthecloud/protosource/**`, minus `options`/`opaquedata`), emitting nested files into `ts/client/src/gen/` (e.g. `gen/funinthecloud/protosource/apierror/v1/apierror_v1_pb.ts`). Scoping keeps example/domain types out of the published `@protosource/client` package. All generated files are tracked in git.

The same rule applies when modifying the TS generator: use `go install` or the local template override.

## Releasing

Real releases are triggered automatically on `v*` tags via GitHub Actions (see `.github/workflows/release-binaries.yml`).

When changing `.goreleaser.yaml`, the templates, or anything release-related, test locally first using snapshot mode:

```bash
# Fast check — just builds the binaries
goreleaser build --snapshot --clean

# Full simulation of what a release would look like (including archives)
goreleaser release --snapshot --clean
```

After the second command, inspect the `dist/` directory. You should see one combined archive per platform containing both plugins (e.g. `protosource_vX.Y.Z-next_linux_amd64.tar.gz`).

When actively developing the generator itself, continue using `buf generate --template buf.gen.local.yaml`.

## Architecture

### Runtime Core (`protosource.go`)

The framework's central types:
- **`Repository`** — processes commands through a pipeline (validate, authorize, evaluate, emit, persist) and rebuilds aggregates from event history. Created via `New(prototype, store, serializer, ...opts)`.
- **`Store`** — persistence interface (`Save`/`Load`). Optional `AggregateStore` for materialized views, `SnapshotTailStore` for optimized snapshot loading.
- **`Serializer`** — marshals events to/from `Record` protos.
- **`Aggregate`**, **`Commander`**, **`Event`** — domain interfaces implemented by generated code.
- **`PostApplyHook`** — optional interface (`AfterOn()`) for derived field computation after event replay/materialization.
- **`Projector`** — optional interface (`Projections()`) for materialized projection views, generated for aggregates with projection messages.
- **`EventTTLer`** — optional interface (`EventTTLSeconds()`) for aggregates with `event_ttl_seconds` annotation. Repository stamps records with TTL before persisting.
- **`Request`/`Response`/`HandlerFunc`** — provider-agnostic HTTP abstractions for generated handlers.

### Authorization (`authz/`)

Every generated handler — commands and reads (`Get`, `Load`, `History`, `QueryBy…`) — calls `authz.Authorizer.Authorize(ctx, req, "{proto_package}.{Op}")` before doing any work. `{Op}` is the command name for writes, or `Get{Aggregate}` / `Load{Aggregate}` / `History{Aggregate}` / `QueryBy{Fields}[With{SKFields}]` for reads. The name is stamped at codegen time.

- **`authz.Authorizer`** — interface with one method. Returns `(context.Context, error)` so implementations can enrich ctx with `WithUserID` / `WithJWT` for downstream handlers.
- **`authz/allowall`** — no-op implementation; the default wire binding for consumers that enforce authorization elsewhere.
- **`authz.ErrUnauthenticated` → 401**, **`ErrForbidden` → 403**, **anything else → 503 `AUTHZ_UNAVAILABLE`**. The 503 default is deliberate: transient auth-service failures (timeout, DNS, upstream 5xx) must not look like permission denials to load balancers and retry logic.
- Generated handlers prefer `authz.UserIDFromContext(ctx)` over `request.Actor` when stamping `cmd.Actor`, so shadow-token flows get the resolved user id in the audit trail instead of the raw bearer.

Reference implementation: [`funinthecloud/protosource-auth`](https://github.com/funinthecloud/protosource-auth) — a full shadow-token auth service (User/Role/Token/Issuer/Key aggregates + HTTP endpoints + mgr CLI) that ships `httpauthz.Authorizer` as the concrete client.

### Code Generation (`cmd/protoc-gen-protosource/`)

The buf plugin reads proto annotations and generates four files per domain package:
- `*.protosource.pb.go` — aggregate `On` method, command builders, event emission, version validation, authorization, snapshot support (from `protosource.gotext`)
- `*.protosource.lambda.pb.go` — per-command HTTP handlers plus read endpoints `GET /{id}` (materialized), `GET /load/{id}` (event replay), `GET /{id}/history`, and `GET /query/...` (from `lambda.gotext`). All handlers — commands and reads — call `authz.Authorizer.Authorize` with a canonical function name (`{proto_package}.{Op}` where Op is the command name or `Get{Aggregate}` / `Load{Aggregate}` / `History{Aggregate}` / `QueryBy{Fields}…`).
- `*.protosource.wire.pb.go` — Wire `Repository` wrapper, `ProvideRepository`, and `ProviderSet` in the same package (from `wire.gotext`). Store-agnostic: the concrete `protosource.Store` is wired separately.
- `*mgr/main.go` — CLI manager for interactive testing (from `cli.gotext`); commands with embedded message fields accept JSON args, commands with repeated/map fields are omitted

The plugin logic is in `protosourceify.go`; templates are in `content/`.

### TypeScript Client Generation (`cmd/protoc-gen-protosource-ts/`)

A separate buf plugin that generates one TypeScript client file per domain package:
- `*.protosource.client.ts` — typed HTTP client class with command, load, history, and query methods (from `client.tstext`)

Uses a copied subset of the Go plugin's annotation-reading logic (message classification, opaque/GSI extraction, route prefix) plus TS-specific functions (`tsType`, `tsFieldName`, `tsQueryFormatExpr`).

**Sync warning:** The TS plugin copies GSI-related types (`opaqueUsedGSI`, `opaqueUsedGSIs`, `gsiQueryRoutePath`, `pkFieldsKey`) from the Go plugin. Changes to GSI method naming or collision logic must be mirrored in both `cmd/protoc-gen-protosource/protosourceify.go` and `cmd/protoc-gen-protosource-ts/protosourceify.go`.

### TypeScript Runtime (`ts/client/`)

Published as `@protosource/client`. Mirrors Go's `httpclient/` package:
- **`ProtosourceClient`** — generic HTTP client with `apply()`, `load()`, `history()`, `query()` methods
- **`AuthProvider`** interface with `BearerTokenAuth` and `NoAuth` implementations
- **`APIError`** — structured error from server responses; decoded from the `apierror.v1.Error` wire body (proto binary or JSON) plus the HTTP status line
- Content negotiation: protobuf binary default, `useJSON` option for debug mode
- Uses `fetch` API (browser + Node 18+) and `@bufbuild/protobuf` v2 for serialization

Generated TS clients import from `@protosource/client` (runtime) and sibling `*_pb.js` files (protoc-gen-es types).

### Transport Layer

- **`Router`** (`router.go`) — lightweight path-pattern router mapping `(method, path)` to `HandlerFunc`, with `{param}` extraction.
- **`adapters/awslambda/`** — converts API Gateway proxy requests to/from `Request`/`Response`. Supports `Wrap` (single handler) and `WrapRouter` (router dispatch).
- **`adapters/httpstandard/`** — converts `net/http` requests to/from `Request`/`Response`. Includes `BearerTokenExtractor` and `HeaderExtractor` for actor identity.

**Wire format: binary protobuf by default, everywhere.** Generated handlers, `httpclient`, `ts/client`, and the `*mgr` CLIs all default to `application/protobuf` and content-negotiate per request via `Accept`/`Content-Type`. JSON (`protojson`) is a dev/debug opt-in: `httpclient.WithJSON()`, TS `ProtosourceClient({ useJSON: true })`, or `<aggregate>mgr -json`. There is no global server-side mode — each request stands alone. JSON should never reach production traffic; the only signal it has is the `Content-Type` on the wire (no log line).

**Error bodies are content-negotiated too.** Non-2xx responses carry an `apierror.v1.Error` (`code`/`message`/`detail`) marshaled in the request's negotiated format — protobuf binary by default, JSON when the request opted in — with the HTTP status on the status line, not in the body. Both clients (`httpclient.APIError`, TS `APIError`) decode by the response `Content-Type` and fall back to a synthetic `UNKNOWN` error carrying the raw body when it isn't a valid `Error` (e.g. a plaintext LB 503 or HTML gateway page).

### Command Processing Pipeline

`Repository.Apply` processes commands in order:

1. **VersionValidator** — lifecycle gate (create requires version==0, mutation requires version>0)
2. **ProtoValidater** — annotation-driven field constraints via buf/protovalidate
3. **StateGuard** — state-machine transition gate via `allowed_states` (rejects commands whose current state is not in the allowed list)
4. **EventEmitter check** — fail fast if command cannot emit events
5. **CommandEvaluator** — optional custom business logic (return `ErrSkip` for silent no-op)
6. **EventEmitter** — emit events (generated from `produces_events`)
7. **Persist** — save events to store
8. **Materialize** _(optional)_ — if store implements `AggregateStore`, apply events via `On`, run `PostApplyHook.AfterOn()` if implemented, persist materialized aggregate (write-only, best-effort)

Steps 1-3 and 6 are generated. For a custom state guard (e.g. inspecting fields beyond `State`), implement `GuardState` on the command type. For custom evaluation, implement `Evaluate`. For derived fields from collections, implement `AfterOn` on the aggregate (see PostApplyHook below). See `docs/pipeline.md` for details.

## DynamoDB Table Design

Two tables: **events** (`a`/`v` String/Number) and **aggregates** (`pk`/`sk` String/String + 20 GSIs).
The aggregates table IS the opaquedata single-table — there is no separate opaque table.
All materialized aggregates must implement `AutoPKSK`; there is no fallback storage path.

### SK Convention
- `"AGG"` — the materialized aggregate itself
- `"NA"` — unused GSI slots only (not for main table SK)
- `"PROJ#<Name>"` — reserved for future projection rows

### TTL
Both tables have DynamoDB TTL enabled on the `t` attribute. Events table TTL is used for ephemeral aggregates (`event_ttl_seconds` annotation). Aggregates table TTL is used by the opaquedata layer for expiring materialized state.

### GSIs
Always create all 20 GSI pairs (`gsi1pk`/`gsi1sk` through `gsi20pk`/`gsi20sk`). Empty GSIs cost nothing with PAY_PER_REQUEST billing.

### GSI Method Naming
When multiple GSIs share the same PK fields, the PK-only query method (`QueryByColor`) is generated once (first GSI wins). `WithSK`/`BetweenSK` variants disambiguate naturally via SK field names. Server-side `Select` methods and lambda handlers for duplicate-PK GSIs use `ViaGSI{N}` suffix and SK-scoped route paths (`/query/by-color-with-number`) to ensure each queries the correct DynamoDB index.

## Cosmos DB Container Design

Cross-cloud parity with DynamoDB: two containers, **events** (partitionKey `a`, item id `v`) and **aggregates** (partitionKey `pk`, item id `sk`). Same opaquedata model — the 20 GSI slot pairs (`gsi1pk`/`gsi1sk` … `gsi20pk`/`gsi20sk`) live as document properties; Cosmos serves them via cross-partition queries, no per-index objects required.

- **API:** Cosmos NoSQL (Core SQL). `github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos`.
- **TTL:** stored as both `t` (absolute epoch, used by our query filter) and `ttl` (relative seconds, used by Cosmos auto-purge). Containers must enable `DefaultTimeToLive = -1` so per-item `ttl` is honored.
- **Concurrency:** events use `CreateItem` (Cosmos rejects duplicate `id` within a partition, giving the same version-uniqueness guarantee as Dynamo's conditional `Put`).
- **Throughput:** serverless for dev, autoscale RU/s for prod — chosen at the tofu module, not in store code.
- **Auth:** Managed Identity → Cosmos data-plane RBAC. No connection strings in app config.

Packages:
- `azure/cosmosclient` — `ContainerClient` interface (one per container) wrapping `*azcosmos.ContainerClient`. Adds `ExecuteCreateBatch` (atomic per-partition writes via `TransactionalBatch`) and a `Pager` for early-termination queries. Exposes `BatchError` + `IsBatchConflict` for version-collision detection.
- `opaquedata/cosmos` — `OpaqueStore` implementation. Single-partition for main-pk queries; cross-partition for `GSIIndex > 0`.
- `stores/cosmosdbstore` — `CosmosDBStore` implementing `Store` + `AggregateStore` + `SnapshotTailStore`. Event docs use `id = strconv(version)` so Cosmos's per-partition id uniqueness provides the same conditional-write guarantee Dynamo gets from `TransactWriteItems`. Includes `EnsureDatabase` / `EnsureContainers` (creates containers with `DefaultTimeToLive = -1` so per-item `ttl` is honored). Wire-typed `EventsContainerClient` / `AggregatesContainerClient` aliases keep the DI graph unambiguous.

## Proto Conventions

Domain protos import `funinthecloud/protosource/options/v1/options_v1.proto` and use these annotations:

```protobuf
option (funinthecloud.protosource.options.v1.protosource_file).enabled = true;

message Sample {
  option (funinthecloud.protosource.options.v1.protosource_message_type).aggregate = {};
}

// Ephemeral aggregate with 24h event TTL:
message TempSession {
  option (funinthecloud.protosource.options.v1.protosource_message_type).aggregate = {
    event_ttl_seconds: 86400
  };
}
message Create {
  option (funinthecloud.protosource.options.v1.protosource_message_type).command = {
    produces_events: ["Created", "Unlocked"]  // two-event pattern for initial state
    lifecycle: COMMAND_LIFECYCLE_CREATION
  };
}
message Created {
  option (funinthecloud.protosource.options.v1.protosource_message_type).event = {};
}
message Locked {
  option (funinthecloud.protosource.options.v1.protosource_message_type).event = {
    sets_state: "STATE_LOCKED"  // On will set aggregate.State = State_STATE_LOCKED
  };
}
message Snapshot {
  option (funinthecloud.protosource.options.v1.protosource_message_type).snapshot = { every_n_events: 50 };
}
```

### Command/Event Guidelines

**One command, one event** is the recommended pattern. The `Created` event should use `sets_state` to set the initial state directly:
```protobuf
message Created {
  option (...).event = { sets_state: "STATE_DRAFT" };
}
```

**Multi-event commands** (`produces_events: ["Created", "Unlocked"]`) are valid when the second event is also a standalone command. For example, `Unlock` is a real command that independently produces the `Unlocked` event — the `Create` command reuses it to set initial state. Don't create events that exist solely to set initial state on creation (e.g., a `Drafted` event that no command produces independently).

The `sets_state` annotation on event messages generates state assignments in the `On` method.

The `On` method is **fully generated**. For scalar fields, event fields are mechanically copied to matching aggregate fields. For collections, events use a `collection` annotation (see below).

### Collection Fields

Aggregates use `map<string, Message>` fields for collections. The map key is a string field on the element message identified by `key_field`. Events declare their collection action via annotation:

```protobuf
message LineItem {
  string item_id     = 1;  // map key
  string description = 2;
  int64  price_cents = 3;
  int32  quantity    = 4;
}

message Order {
  option (...).aggregate = {};
  // ... scalar fields ...
  map<string, LineItem> items = 14;
}

message ItemAdded {
  option (...).event = {
    collection: { target: "items", action: COLLECTION_ACTION_ADD, key_field: "item_id" }
  };
  // ... internal fields 1-4 ...
  LineItem item = 5;  // element to insert (key extracted from item_id)
}

message ItemRemoved {
  option (...).event = {
    collection: { target: "items", action: COLLECTION_ACTION_REMOVE, key_field: "item_id" }
  };
  // ... internal fields 1-4 ...
  string item_id = 5;  // key identifying which element to delete
}
```

**Generated On():**
- ADD: `aggregate.Items[e.GetItem().GetItemId()] = e.GetItem()` (O(1), idempotent — re-adding same key overwrites)
- REMOVE: `delete(aggregate.Items, e.GetItemId())` (O(1))

**Rules:**
- Collection fields must be `map<string, Message>` — string keys only
- `key_field` is required for both ADD and REMOVE; must name a string field on the element message
- An event either does collection work OR scalar field copying — not both (exactly one domain field)
- ADD events must have exactly one embedded field matching the map's value type
- REMOVE events must have a string field matching `key_field`
- REMOVE is not valid on creation events
- Commands with collection events carry the embedded message type (e.g., `LineItem item = 3`), parsed from JSON in the generated CLI
- Multiple independent collections on one aggregate are supported (each with its own events)

### PostApplyHook (Derived Fields)

For computed/derived fields (totals, counts, etc.), implement `AfterOn()` on the aggregate in a hand-written file:

```go
// order_derived.go
func (o *Order) AfterOn() {
    o.ItemCount = int32(len(o.Items))
    var total int64
    for _, item := range o.Items {
        total += item.GetPriceCents() * int64(item.GetQuantity())
    }
    o.TotalCents = total
}
```

`AfterOn()` is called: (1) once after all events are replayed during Load, (2) once after all new events are applied during materialization in Apply, and (3) in generated `EmitEvents` on the cloned aggregate before snapshot emission (only when a snapshot will actually be created). **`AfterOn` is a reserved method name** — do not use it as a command or event message name.

### Field Contracts

Command messages must have `id` (field 1, string) and `actor` (field 2, string).
Event messages must have `id` (field 1), `version` (field 2, int64), `at` (field 3, int64), `actor` (field 4, string). Domain fields start at 5.

## Conventions

- Module path: `github.com/funinthecloud/protosource`
- Go 1.25+
- Generated Go files (`options/v1/`, `record/v1/`, `history/v1/`, `apierror/v1/`, `example/app/`) are auto-generated — never edit by hand
- The options proto lives in this repo; no external BSR dependency
- `buf.gen.yaml` uses `module=` option to strip the Go module prefix, placing generated code at the repo root
- Proto files use explicit `option go_package` to control Go output paths independently of proto directory structure

## Formatting

**IMPORTANT**: Proto files MUST be formatted with `clang-format`, not `buf format`. After creating or modifying any `.proto` file, run:

```bash
clang-format --style=file -i proto/**/*.proto
```

The `.clang-format` config in the repo root controls the style (Google-based, aligned declarations/assignments, no column limit).

## Workflow

**IMPORTANT**: Before starting work on a new branch, always fetch and branch from the latest remote main to avoid merge conflicts with recently merged PRs:

```bash
git fetch origin
git checkout -b <branch-name> origin/main
```

## Releasing

Pushes to `main` and git tags (`v*`) trigger `.github/workflows/release.yml`:

- On `main` pushes: proto module is pushed with label `main` (for `:main` consumers).
- On `v*` tags: proto module pushed with the tag label + `@protosource/client` npm package published.

Requires `BUF_TOKEN` in GitHub Actions secrets. npm publishing uses OIDC (`id-token: write`).

Hosted buf plugins require a pro account, so plugins are not published to BSR. Consumers build from source using local plugin mode (`buf.gen.yaml` with `local:` pointing to the installed binary).

## TODO

See [TODO.md](TODO.md) for remaining framework work and cross-repo tracking.


<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:7510c1e2 -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

**Architecture in one line:** issues live in a local Dolt DB; sync uses `refs/dolt/data` on your git remote; `.beads/issues.jsonl` is a passive export. See https://github.com/gastownhall/beads/blob/main/docs/SYNC_CONCEPTS.md for details and anti-patterns.

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->
