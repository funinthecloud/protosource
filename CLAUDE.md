# protosource

An event sourcing framework where domain models are defined entirely in protocol buffers. A buf plugin generates all Go boilerplate — including the aggregate's `On` method, builders, event emission, snapshot support, lifecycle validation, state transitions, and authorization — so domain logic is expressed purely through proto annotations.

## Build & Run

```bash
go generate ./...                        # install tools (buf, wire, protoc-gen-go)
go install ./cmd/protoc-gen-protosource  # IMPORTANT: install plugin to $GOPATH/bin so buf can find it
buf generate                             # generate Go code from proto/
go build ./...                           # build everything
go test ./...                            # run tests
go vet ./...                             # static analysis
```

**IMPORTANT**: After modifying the plugin template (`cmd/protoc-gen-protosource/content/protosource.gotext`) or plugin code (`cmd/protoc-gen-protosource/protosourceify.go`), you MUST run `go install ./cmd/protoc-gen-protosource` before `buf generate`. The `buf generate` command invokes `protoc-gen-protosource` as a local plugin from `$GOPATH/bin`, so `go build` alone is not enough — the binary must be installed.

## Project Layout

```
protosource.go          — runtime: Repository, Store, Serializer, core interfaces
stores/                 — Store implementations (memorystore, dynamodbstore, mysqlstore, boltdbstore)
serializers/            — Serializer implementations (protobinaryserializer, protojsonserializer)
cmd/
  protoc-gen-protosource/ — buf plugin (generates .protosource.pb.go files)
  sample/                 — example CLI showing Create/Update/Load cycle
proto/                  — protobuf definitions (buf module root)
  funinthecloud/protosource/options/v1/ — custom options proto (aggregate, command, event, snapshot, projection)
  funinthecloud/protosource/record/v1/ — record proto (version + data)
  funinthecloud/protosource/history/v1/ — history proto (list of records)
  example/app/sample/v1/            — sample domain with snapshots (fictitious org)
  example/app/samplenosnapshot/v1/  — sample domain without snapshots (fictitious org)
  example/app/test/v1/              — test domain (fictitious org)
options/v1/             — generated Go code for options proto (do not edit by hand)
record/v1/              — generated Go code for record proto (do not edit by hand)
history/v1/             — generated Go code for history proto (do not edit by hand)
example/app/               — generated Go code for example protos (do not edit by hand)
tools/                  — go:generate tool installs (buf, wire, protoc-gen-go)
```

## Key Concepts

- **Aggregate**: the state object, rebuilt by replaying events. First message in the proto file.
- **Command**: present-tense action (Create, Update). Validated, then emits events.
- **Event**: past-tense fact (Created, Updated). Immutable. Fields 1-4 are reserved (id, version, at, actor).
- **Snapshot**: periodic aggregate state capture for replay cost control.
- **Projection**: read-side view model (not yet implemented in runtime).

## Command Processing Pipeline

`Repository.Apply` processes commands through this pipeline in order:

1. **VersionValidator** — lifecycle gate (create requires version==0, mutation requires version>0)
2. **ProtoValidater** — annotation-driven field and cross-field constraints via buf/protovalidate
3. **CommandAuthorizer** — validate command against current aggregate state (state-machine transitions via `allowed_states`)
4. **EventEmitter check** — fail fast if command cannot emit events (before running custom logic)
5. **CommandEvaluator** — optional custom business logic comparing command against aggregate state (duplicate detection, idempotency, conditional no-ops). Return `ErrSkip` for silent no-op, or any other error to abort.
6. **EventEmitter** — emit events (generated from `produces_events`)
7. **Persist** — save events to store
8. **Materialize** _(optional, runtime)_ — if the store implements `AggregateStore`, apply new events to the in-memory aggregate and persist its serialized state (best-effort; event persistence in step 7 is the source of truth)

Steps 1-3 and 6 are generated from proto annotations. For complex authorization beyond state checks, implement `Authorize` by hand on the command type. For custom business logic that inspects the aggregate before event emission, implement `Evaluate` on the command type. See `docs/pipeline.md` for full details.

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

The `On` method is **fully generated**. Event fields are mechanically copied to matching aggregate fields. No hand-written code in the generated files is needed.

Command messages must have `id` (field 1, string) and `actor` (field 2, string).
Event messages must have `id` (field 1), `version` (field 2, int64), `at` (field 3, int64), `actor` (field 4, string). Domain fields start at 5.

## Dependencies

- **buf** for proto management and code generation
- **buf/protovalidate** for annotation-driven field validation
- **protoc-gen-star/v2** for the plugin framework
- **google.golang.org/protobuf** for proto runtime
- **wire** for dependency injection

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

- [x] ~~Generate starter `aggregate.go`~~ (superseded: `On` method is now fully generated from proto annotations)
- [ ] Create annotations for auto-generating single aggregate projections (has legacy code examples to reference)
- [x] ~~Finish growing test coverage of memorystore~~ (100% coverage)
- [x] ~~Create a boltdb store with good test coverage~~ (84.2% coverage)
- [x] ~~Analyze dynamodbstore to ensure it still works with current framework changes~~ (rewritten to implement all framework interfaces in PR #11)
- [ ] Add capabilities to all stores to store the aggregate post-apply each time an apply runs
- [ ] Look deeper into multi-package projections and auto-generation possibilities
- [x] ~~Add plugin validation for `sets_state` references (verify enum value exists in file)~~
- [ ] Update sample and samplenosnapshot protos to use two-event pattern and `sets_state` if applicable
- [ ] Investigate TTL / event expiration: when a snapshot is recorded, events older than the most recent snapshot could age off via a background mechanism (e.g., DynamoDB TTL, scheduled cleanup for SQL/BoltDB stores)
- [x] ~~Create a protojsonserializer that uses proto JSON serialization~~
- [x] ~~Remove scenario package~~ (superseded: use memorystore + real serializer for testing)
- [ ] Build a showcase app: React frontend + Go backend demonstrating event sourcing and CQRS with a to-do list manager domain (multiple lists, items, reordering, etc.) — simple enough to understand, rich enough to show projections and state transitions. Explore GraphQL as the read-side query layer over CQRS projections (natural fit: projections map to graph types, subscriptions for real-time updates)
