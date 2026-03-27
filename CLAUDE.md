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

**IMPORTANT**: After modifying the plugin template (`cmd/protoc-gen-protosource/content/*.gotext`) or plugin code (`cmd/protoc-gen-protosource/protosourceify.go`), you MUST run `go install ./cmd/protoc-gen-protosource` before `buf generate`. The `buf generate` command invokes `protoc-gen-protosource` as a local plugin from `$GOPATH/bin`, so `go build` alone is not enough — the binary must be installed.

## Architecture

### Runtime Core (`protosource.go`)

The framework's central types:
- **`Repository`** — processes commands through a pipeline (validate, authorize, evaluate, emit, persist) and rebuilds aggregates from event history. Created via `New(prototype, store, serializer, ...opts)`.
- **`Store`** — persistence interface (`Save`/`Load`). Optional `AggregateStore` for materialized views, `SnapshotTailStore` for optimized snapshot loading.
- **`Serializer`** — marshals events to/from `Record` protos.
- **`Aggregate`**, **`Commander`**, **`Event`** — domain interfaces implemented by generated code.
- **`Request`/`Response`/`HandlerFunc`** — provider-agnostic HTTP abstractions for generated handlers.

### Code Generation (`cmd/protoc-gen-protosource/`)

The buf plugin reads proto annotations and generates four files per domain package:
- `*.protosource.pb.go` — aggregate `On` method, command builders, event emission, version validation, authorization, snapshot support (from `protosource.gotext`)
- `*.protosource.lambda.pb.go` — per-command HTTP handlers, Get, and History endpoints (from `lambda.gotext`)
- `*.protosource.wire.pb.go` — Wire dependency injection provider sets (from `wire.gotext`)
- `*mgr/main.go` — CLI manager for interactive testing (from `cli.gotext`)

The plugin logic is in `protosourceify.go`; templates are in `content/`.

### Transport Layer

- **`Router`** (`router.go`) — lightweight path-pattern router mapping `(method, path)` to `HandlerFunc`, with `{param}` extraction.
- **`adapters/awslambda/`** — converts API Gateway proxy requests to/from `Request`/`Response`. Supports `Wrap` (single handler) and `WrapRouter` (router dispatch).
- **`adapters/httpstandard/`** — converts `net/http` requests to/from `Request`/`Response`. Includes `BearerTokenExtractor` and `HeaderExtractor` for actor identity.

### Command Processing Pipeline

`Repository.Apply` processes commands in order:

1. **VersionValidator** — lifecycle gate (create requires version==0, mutation requires version>0)
2. **ProtoValidater** — annotation-driven field constraints via buf/protovalidate
3. **CommandAuthorizer** — state-machine transitions via `allowed_states`
4. **EventEmitter check** — fail fast if command cannot emit events
5. **CommandEvaluator** — optional custom business logic (return `ErrSkip` for silent no-op)
6. **EventEmitter** — emit events (generated from `produces_events`)
7. **Persist** — save events to store
8. **Materialize** _(optional)_ — if store implements `AggregateStore`, persist materialized aggregate (write-only, best-effort)

Steps 1-3 and 6 are generated. For custom authorization, implement `Authorize` on the command type. For custom evaluation, implement `Evaluate`. See `docs/pipeline.md` for details.

## DynamoDB Table Design

Two tables: **events** (`a`/`v` String/Number) and **aggregates** (`pk`/`sk` String/String + 20 GSIs).
The aggregates table IS the opaquedata single-table — there is no separate opaque table.
All materialized aggregates must implement `AutoPKSK`; there is no fallback storage path.

### SK Convention
- `"AGG"` — the materialized aggregate itself
- `"NA"` — unused GSI slots only (not for main table SK)
- `"PROJ#<Name>"` — reserved for future projection rows

### GSIs
Always create all 20 GSI pairs (`gsi1pk`/`gsi1sk` through `gsi20pk`/`gsi20sk`). Empty GSIs cost nothing with PAY_PER_REQUEST billing.

## Proto Conventions

Domain protos import `funinthecloud/protosource/options/v1/options_v1.proto` and use these annotations:

```protobuf
option (funinthecloud.protosource.options.v1.protosource_file).enabled = true;

message Sample {
  option (funinthecloud.protosource.options.v1.protosource_message_type).aggregate = {};
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

The **two-event pattern** for initial state: CREATION commands emit a domain event (Created) plus a state-transition event (Unlocked), so every state — including the initial one — is an explicit event in the stream. The `sets_state` annotation on event messages generates state assignments in the `On` method.

The `On` method is **fully generated**. Event fields are mechanically copied to matching aggregate fields.

Command messages must have `id` (field 1, string) and `actor` (field 2, string).
Event messages must have `id` (field 1), `version` (field 2, int64), `at` (field 3, int64), `actor` (field 4, string). Domain fields start at 5.

## Conventions

- Module path: `github.com/funinthecloud/protosource`
- Go 1.25+
- Generated Go files (`options/v1/`, `record/v1/`, `history/v1/`, `example/app/`) are auto-generated — never edit by hand
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

## TODO

- [ ] Look deeper into multi-package projections and auto-generation possibilities
- [ ] Build a showcase app: React frontend + Go backend demonstrating event sourcing and CQRS with a to-do list manager domain (multiple lists, items, reordering, etc.) — simple enough to understand, rich enough to show projections and state transitions. Explore GraphQL as the read-side query layer over CQRS projections (natural fit: projections map to graph types, subscriptions for real-time updates)
