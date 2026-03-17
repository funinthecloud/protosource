# Command Processing Pipeline

When `Repository.Apply(ctx, command)` is called, the command passes through a series of
validation and processing steps before events are persisted. Most steps are
**auto-generated** by `protoc-gen-protosource` from proto annotations, while the remaining steps are either runtime (persist, materialize) or an optional
hand-written extension point (CommandEvaluator) for custom business logic.

## Pipeline Steps

```
Command ──►  1. VersionValidator   (generated from lifecycle annotation)
        ──►  2. ProtoValidater    (generated — reads buf.validate annotations)
        ──►  3. CommandAuthorizer  (generated from allowed_states, or hand-written)
        ──►  4. EventEmitter check (fail fast — verify command can emit events)
        ──►  5. CommandEvaluator   (hand-written, optional — custom business logic)
        ──►  6. EventEmitter       (generated from produces_events)
        ──►  7. Persist            (runtime)
        ──►  8. Materialize        (runtime, optional — if store implements AggregateStore)
```

### 1. VersionValidator (generated)

Enforces lifecycle constraints based on the command's `lifecycle` annotation:

- **CREATION** commands (`lifecycle: COMMAND_LIFECYCLE_CREATION`) require `version == 0`.
  If the aggregate already exists, the command is rejected with `ErrAlreadyCreated`.
- **MUTATION** commands (`lifecycle: COMMAND_LIFECYCLE_MUTATION`) require `version > 0`.
  If the aggregate does not exist yet, the command is rejected with `ErrNotCreatedYet`.

```protobuf
message Create {
  option (protosource_message_type).command = {
    produces_events: ["Created", "Unlocked"]
    lifecycle: COMMAND_LIFECYCLE_CREATION  // generates version == 0 check
  };
}
```

### 2. ProtoValidater (generated)

Every command gets a generated `ProtoValidate()` method that delegates to the
`buf.build/go/protovalidate` library at runtime. This validates field constraints
declared in the proto schema using `buf.validate` annotations:

```protobuf
message Create {
  string id    = 1 [(buf.validate.field).string.min_len = 1];
  string actor = 2 [(buf.validate.field).string.min_len = 1];
  string body  = 3 [(buf.validate.field).string.min_len = 1];
}
```

Cross-field validations can be expressed using CEL expressions:

```protobuf
message Transfer {
  option (buf.validate.message).cel = {
    id: "accounts_differ"
    message: "from_account and to_account must be different"
    expression: "this.from_account != this.to_account"
  };
  string id           = 1;
  string actor        = 2;
  string from_account = 3;
  string to_account   = 4;
}
```

The `protovalidate.Validator` is cached via `sync.Once` — it is created once per
package, not per call. Calling `ProtoValidate()` on a message with no annotations
returns nil (success).

### 3. CommandAuthorizer (generated or hand-written, optional)

If your command implements `CommandAuthorizer`, the `Authorize(aggregate)` method is
called to validate the command against the aggregate's current state.

**Auto-generated from annotations**: When a command has `allowed_states`, the plugin
generates an `Authorize` method that checks the aggregate's `State` field:

```protobuf
message Lock {
  option (protosource_message_type).command = {
    produces_events: ["Locked"]
    lifecycle: COMMAND_LIFECYCLE_MUTATION
    allowed_states: ["STATE_UNLOCKED"]  // generates state check
  };
}
```

This generates:

```go
func (m *Lock) Authorize(aggregate protosource.Aggregate) error {
    a := aggregate.(*MyAggregate)
    switch a.GetState() {
    case State_STATE_UNLOCKED:
        return nil
    default:
        return fmt.Errorf("command Lock not allowed in state %s: %w",
            a.GetState(), protosource.ErrUnauthorized)
    }
}
```

**Hand-written for complex cases**: For authorization logic beyond simple state checks
(ownership, roles, time-based rules), implement `Authorize` by hand on the command type.
Since commands are generated proto messages, add the method in a separate file in the
same package.

### 4–5. EventEmitter check + CommandEvaluator (hand-written, optional)

Before running any custom evaluation logic, the pipeline verifies the command
implements `EventEmitter`. This fail-fast ensures a malformed command that only
implements `CommandEvaluator` cannot silently succeed via `ErrSkip`.

If your command implements `CommandEvaluator`, the `Evaluate(aggregate)` method is
called to inspect the current aggregate state before events are emitted. This is the
extension point for custom business logic such as duplicate detection, idempotency
checks, or conditional no-ops.

**Return values:**
- `nil` — proceed with event emission
- `ErrSkip` (or an error wrapping it) — silently skip event emission; the command
  succeeds with no events persisted and the current version is returned
- Any other error — abort the command

Since this step is never auto-generated, always implement it by hand on the command
type in a separate file in the same package:

```go
// In a hand-written file (e.g., update_evaluate.go)
func (m *AddTag) Evaluate(aggregate protosource.Aggregate) error {
    a := aggregate.(*MyAggregate)
    if existing, ok := a.Tags[m.GetKey()]; ok && existing == m.GetValue() {
        return fmt.Errorf("tag %s already set to %s: %w", m.GetKey(), m.GetValue(), protosource.ErrSkip)
    }
    return nil
}
```

### 6. EventEmitter (generated)

The generated `EmitEvents(aggregate)` method constructs events from the command using
the `Builder` pattern. Each event gets an auto-incremented version, a timestamp
(`NowMicros`), and the aggregate ID. If the aggregate implements `Snapshoter` and the
version hits the snapshot interval, a `Snapshot` event is automatically appended.

The builder method for each event takes only that event's non-internal fields as
parameters, mapped from the command's matching getters.

### 7. Persist (runtime)

The repository serializes the events and saves them to the store. This is handled
entirely by the runtime — no generated code involved.

### 8. Materialize (runtime, optional)

If the store implements `AggregateStore`, the repository applies the newly emitted
events to the in-memory aggregate (via `On`) and persists its serialized state. This
is best-effort: event persistence (step 7) is the source of truth. A materialization
failure does not cause `Apply` to return an error.

## The Generated `On` Method

The `On` method is **fully generated** from the proto schema. It applies events to the
aggregate to rebuild state. It is called:

- During `Repository.Load` — to reconstruct the aggregate from stored events
- During snapshot restoration — to apply the snapshot state

### How It Works

For each event type defined in the proto file, the generated `On` method:

1. Sets `Id` and `Version` from the event (always)
2. Calls `setCreated(e)` if the event is exclusively produced by a CREATION command
3. Calls `setModified(e)` for all events
4. Copies each event field to the matching aggregate field (by name)
5. Sets `aggregate.State` if the event has a `sets_state` annotation

### State Transitions via `sets_state`

State-transition events use the `sets_state` annotation to declare what state the
aggregate should transition to when the event is applied:

```protobuf
message Locked {
  option (protosource_message_type).event = {
    sets_state: "STATE_LOCKED"  // On will set aggregate.State = State_STATE_LOCKED
  };
  string id      = 1;
  int64  version = 2;
  int64  at      = 3;
  string actor   = 4;
}
```

### Two-Event Pattern for Initial State

Instead of hardcoding initial state in the `On` method, CREATION commands can emit
multiple events to establish initial state explicitly:

```protobuf
message Create {
  option (protosource_message_type).command = {
    produces_events: ["Created", "Unlocked"]  // two events
    lifecycle: COMMAND_LIFECYCLE_CREATION
  };
}
```

This means every state transition — including the initial state — is an explicit event
in the stream. The aggregate's state is always the result of replaying events, with no
special-case initialization logic.

### Generated Example

For the test domain proto, the generated `On` method looks like:

```go
func (aggregate *Test) On(event protosource.Event) error {
    aggregate.Id = event.GetId()
    aggregate.Version = event.GetVersion()

    switch e := event.(type) {
    case *Created:
        aggregate.setCreated(e)
        aggregate.setModified(e)
        aggregate.Body = e.GetBody()
    case *Updated:
        aggregate.setModified(e)
        aggregate.Body = e.GetBody()
    case *Locked:
        aggregate.setModified(e)
        aggregate.State = State_STATE_LOCKED
    case *Unlocked:
        aggregate.setModified(e)
        aggregate.State = State_STATE_UNLOCKED
    case *Snapshot:
        aggregate.RestoreSnapshot(e)
    default:
        return fmt.Errorf("%T: %w", e, protosource.ErrUnhandledEvent)
    }

    return nil
}
```

## Testing

Use `memorystore` with a real serializer (`protobinaryserializer` or `protojsonserializer`)
to test your domain through the full `Repository.Apply` pipeline — including serialization
round-tripping and store behavior. The `protojsonserializer` is recommended for tests
because its output is human-readable JSON, making failures easier to debug.

```go
func newTestRepo() *protosource.Repository {
    return protosource.New(
        &MyAggregate{},
        protosource.WithStore(memorystore.New()),
        protosource.WithSerializer(protojsonserializer.NewSerializer()),
    )
}

func Test_Create(t *testing.T) {
    repo := newTestRepo()
    ctx := context.Background()

    version, err := repo.Apply(ctx, &Create{Id: "abc", Actor: "alice", Body: "hello"})
    if err != nil {
        t.Fatalf("create failed: %v", err)
    }
    if version != 2 { // Created(v1) + Unlocked(v2)
        t.Fatalf("expected version 2, got %d", version)
    }

    // Verify aggregate state after applying
    agg, _ := repo.Load(ctx, "abc")
    order := agg.(*MyAggregate)
    if order.GetBody() != "hello" {
        t.Errorf("expected body 'hello', got %q", order.GetBody())
    }
    if order.GetState() != State_STATE_UNLOCKED {
        t.Errorf("expected STATE_UNLOCKED, got %s", order.GetState())
    }
}

func Test_ValidationError(t *testing.T) {
    repo := newTestRepo()
    _, err := repo.Apply(context.Background(), &Create{Id: "abc", Actor: "alice", Body: ""})
    if !errors.Is(err, protosource.ErrValidationFailed) {
        t.Fatalf("expected ErrValidationFailed, got: %v", err)
    }
}

func Test_StateAuthorization(t *testing.T) {
    repo := newTestRepo()
    ctx := context.Background()
    repo.Apply(ctx, &Create{Id: "abc", Actor: "alice", Body: "hello"})
    repo.Apply(ctx, &Lock{Id: "abc", Actor: "alice"})

    _, err := repo.Apply(ctx, &Update{Id: "abc", Actor: "alice", Body: "nope"})
    if !errors.Is(err, protosource.ErrUnauthorized) {
        t.Fatalf("expected ErrUnauthorized, got: %v", err)
    }
}
```

### Testing CommandEvaluator

For commands that implement `CommandEvaluator`, test all three return paths:

```go
func Test_EvaluateSkip(t *testing.T) {
    repo := newTestRepo()
    ctx := context.Background()
    repo.Apply(ctx, &Create{Id: "abc", Actor: "alice", Body: "hello"})

    // Adding the same tag twice — second call should be a no-op via ErrSkip.
    repo.Apply(ctx, &AddTag{Id: "abc", Actor: "alice", Key: "env", Value: "prod"})
    version, err := repo.Apply(ctx, &AddTag{Id: "abc", Actor: "alice", Key: "env", Value: "prod"})
    if err != nil {
        t.Fatalf("expected no error (ErrSkip is silent), got: %v", err)
    }
    // Version unchanged — no new events were emitted.
}
```

## Generated vs Hand-Written Summary

| Component | Generated? | Purpose |
|-----------|-----------|---------|
| `*.protosource.pb.go` | Always regenerated | On, Builder, CommandName, ValidateVersion, ProtoValidate, Authorize (from allowed_states), EmitEvents, EventName, setCreated, setModified, Snapshot, RestoreSnapshot |
| `*.pb.go` | Always regenerated (by protoc-gen-go) | Proto message types, getters |
| Custom Authorize | Hand-written (optional) | Complex authorization beyond state checks |
| Custom Evaluate | Hand-written (optional) | Duplicate detection, idempotency, conditional no-ops |
