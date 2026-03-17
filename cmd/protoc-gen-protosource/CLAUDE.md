# protoc-gen-protosource

> Part of the [protosource](../../CLAUDE.md) framework.

A buf/protoc plugin built with [protoc-gen-star](https://github.com/lyft/protoc-gen-star) (v2) that reads custom protobuf options and generates `.protosource.pb.go` files with event sourcing boilerplate.

## Build

```bash
go build ./cmd/protoc-gen-protosource
```

The binary must be on `$PATH` (or in `$GOBIN`) for `buf generate` to find it as a local plugin.

## How It Works

1. `main.go` — registers the module with protoc-gen-star
2. `protosourceify.go` — reads `protosource_file` and `protosource_message_type` extensions, validates field contracts, then renders templates
3. `content/protosource.gotext` — Go text template that produces the generated code

## What Gets Generated

For each proto file with `protosource_file.enabled = true`:

- `Builder` struct with `NewBuilder(id, version)` for constructing events
- Per-command: `CommandName()`, `ValidateVersion()`, `EmitEvents()`
- Per-event: `IncrementVersion()`, `EventName()`, builder method
- Per-aggregate: `SetCreated()`, `SetModified()`, and if snapshots are present: `Snapshot()`, `RestoreSnapshot()`, `maybeSnapshot()`

## Validation (compile-time via plugin)

- Commands must have `id` (field 1, string) and `actor` (field 2, string)
- Events must have `id` (field 1), `version` (field 2, int64), `at` (field 3, int64), `actor` (field 4, string)
- `produces_events` entries must reference existing event messages in the same file

## Key Import

The plugin imports the generated options package:

```go
optionsv1 "github.com/funinthecloud/protosource/options/v1"
```

If the options proto changes, run `buf generate` before rebuilding the plugin.
