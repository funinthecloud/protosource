# Proto Annotations Reference

All annotations are defined in `funinthecloud/protosource/options/v1/options_v1.proto`.

```protobuf
import "funinthecloud/protosource/options/v1/options_v1.proto";
```

## File-Level

### `protosource_file`

Enables code generation for this file.

```protobuf
option (funinthecloud.protosource.options.v1.protosource_file).enabled = true;
```

Without this, the file is ignored by `protoc-gen-protosource`.

## Message-Level

### `protosource_message_type`

A discriminated union -- each message has exactly one role.

| Role | Purpose |
|------|---------|
| `aggregate` | Domain entity reconstituted from events |
| `command` | Intent to change state |
| `event` | Immutable fact of a state change |
| `snapshot` | Point-in-time aggregate state for replay optimization |
| `projection` | Read-side view model |

---

## Aggregate

```protobuf
message Task {
  option (funinthecloud.protosource.options.v1.protosource_message_type).aggregate = {};
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `event_ttl_seconds` | `int64` | 0 (no expiry) | TTL for all events in this aggregate's stream. For ephemeral aggregates only. |

```protobuf
// Ephemeral aggregate -- events expire after 24h
message TempSession {
  option (...).aggregate = { event_ttl_seconds: 86400 };
}
```

### Required aggregate fields

Aggregates must have these fields (names matter, field numbers don't):

| Field | Type | Set by |
|-------|------|--------|
| `id` | `string` | Every event |
| `version` | `int64` | Every event |
| `created_at` | `int64` | Creation events (Unix microseconds) |
| `modified_at` | `int64` | Every event (Unix microseconds) |
| `created_by` | `string` | Creation events |
| `modified_by` | `string` | Every event |
| `state` | `State` enum | Events with `sets_state` |

Domain fields start after the framework fields. Collection fields use `map<string, Message>`.

---

## Command

```protobuf
message Create {
  option (funinthecloud.protosource.options.v1.protosource_message_type).command = {
    produces_events: ["Created"]
    lifecycle: COMMAND_LIFECYCLE_CREATION
  };

  string id    = 1;
  string actor = 2;
  string title = 3;
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `produces_events` | `repeated string` | required | Event message names this command emits. Typically one. |
| `lifecycle` | `CommandLifecycle` | `UNSPECIFIED` | When the command is valid. |
| `allowed_states` | `repeated string` | empty (any state) | Aggregate states in which this command is allowed. Generates `Authorize`. |

### Lifecycle values

| Value | Version check | Error on violation |
|-------|--------------|-------------------|
| `COMMAND_LIFECYCLE_UNSPECIFIED` | None | -- |
| `COMMAND_LIFECYCLE_CREATION` | `version == 0` | `ErrAlreadyCreated` |
| `COMMAND_LIFECYCLE_MUTATION` | `version > 0` | `ErrNotCreatedYet` |

### Required command fields

| Field | Number | Type | Purpose |
|-------|--------|------|---------|
| `id` | 1 | `string` | Aggregate ID |
| `actor` | 2 | `string` | Who is issuing the command |

Domain fields start at 3.

### allowed_states

Generates an `Authorize(aggregate)` method with a state-machine check:

```protobuf
message Complete {
  option (...).command = {
    produces_events: ["Completed"]
    lifecycle: COMMAND_LIFECYCLE_MUTATION
    allowed_states: ["STATE_OPEN"]
  };
}
```

For complex authorization (ownership, roles, time-based), implement `Authorize` by hand on the command type in a separate file.

---

## Event

```protobuf
message Created {
  option (funinthecloud.protosource.options.v1.protosource_message_type).event = {
    sets_state: "STATE_OPEN"
  };

  string id      = 1;
  int64  version = 2;
  int64  at      = 3;
  string actor   = 4;
  string title   = 5;  // domain field
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `sets_state` | `string` | empty | Enum value name to assign to `aggregate.State` in `On`. |
| `collection` | `CollectionMapping` | empty | Declares this event modifies a collection field. |

### Required event fields

| Field | Number | Type | Purpose |
|-------|--------|------|---------|
| `id` | 1 | `string` | Aggregate ID |
| `version` | 2 | `int64` | Position in the event stream |
| `at` | 3 | `int64` | Unix microseconds timestamp |
| `actor` | 4 | `string` | Responsible actor |

Domain fields start at 5.

### sets_state

The generated `On` method assigns the named enum value to `aggregate.State`:

```protobuf
message Locked {
  option (...).event = { sets_state: "STATE_LOCKED" };
  // fields 1-4 ...
}
```

Generates: `aggregate.State = State_STATE_LOCKED`

### collection

Declares that this event modifies a `map<string, Message>` field instead of copying scalar fields. An event does **either** collection work **or** scalar copying -- not both.

```protobuf
message CollectionMapping {
  string           target    = 1;  // map field name on aggregate (e.g. "items")
  CollectionAction action    = 2;  // ADD or REMOVE
  string           key_field = 3;  // string field used as map key (e.g. "item_id")
}
```

| Action | Event field | Generated On behavior |
|--------|------------|----------------------|
| `COLLECTION_ACTION_ADD` | Embedded message matching the map's value type | `aggregate.Items[e.GetItem().GetItemId()] = e.GetItem()` |
| `COLLECTION_ACTION_REMOVE` | String field matching `key_field` | `delete(aggregate.Items, e.GetItemId())` |

```protobuf
message ItemAdded {
  option (...).event = {
    collection: { target: "items", action: COLLECTION_ACTION_ADD, key_field: "item_id" }
  };
  string id = 1; int64 version = 2; int64 at = 3; string actor = 4;
  LineItem item = 5;
}

message ItemRemoved {
  option (...).event = {
    collection: { target: "items", action: COLLECTION_ACTION_REMOVE, key_field: "item_id" }
  };
  string id = 1; int64 version = 2; int64 at = 3; string actor = 4;
  string item_id = 5;
}
```

See [Collections](collections.md) for full details.

---

## Snapshot

```protobuf
message Snapshot {
  option (funinthecloud.protosource.options.v1.protosource_message_type).snapshot = {
    every_n_events: 50
  };
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `every_n_events` | `uint32` | 0 (runtime default) | Write a snapshot every N events. Also the replay lookback window. |
| `disabled` | `bool` | false | Explicit opt-out for low-velocity aggregates. |

The Snapshot message must mirror the aggregate's fields. Generated code handles `Snapshot()` and `RestoreSnapshot()` methods.

```protobuf
// Use runtime default interval
option (...).snapshot = {};

// Explicit interval
option (...).snapshot = { every_n_events: 50 };

// Opt out entirely
option (...).snapshot = { disabled: true };
```

---

## Projection

```protobuf
message TaskSummary {
  option (funinthecloud.protosource.options.v1.protosource_message_type).projection = {};
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `source_aggregates` | `repeated string` | empty | Aggregate types this projection subscribes to (for multi-aggregate projections). |

Projections are read-side view models. The aggregate implements `Projector` (generated), which returns the projection messages to persist alongside the aggregate.

---

## Field-Level: OpaqueData (GSI)

For DynamoDB single-table design. Annotate aggregate fields to declare their role in PK/SK/GSI keys.

```protobuf
extend google.protobuf.FieldOptions {
  OpaqueFieldOptions protosource_opaque_field = 60104;
}
```

### Key types

`OPAQUE_KEY_TYPE_PK`, `OPAQUE_KEY_TYPE_SK`, and `OPAQUE_KEY_TYPE_GSI1PK` through `OPAQUE_KEY_TYPE_GSI20SK`.

### Usage

```protobuf
message Task {
  option (...).aggregate = {};

  string id = 1 [(funinthecloud.protosource.options.v1.protosource_opaque_field) = {
    attributes: [{ type: OPAQUE_KEY_TYPE_PK, order: 1 }]
  }];

  string created_by = 5 [(funinthecloud.protosource.options.v1.protosource_opaque_field) = {
    attributes: [{ type: OPAQUE_KEY_TYPE_GSI1PK, order: 1 }]
  }];

  int64 created_at = 3 [(funinthecloud.protosource.options.v1.protosource_opaque_field) = {
    attributes: [
      { type: OPAQUE_KEY_TYPE_GSI1SK },
      { type: OPAQUE_KEY_TYPE_GSI4SK }
    ]
  }];
}
```

- `order` is 1-based for composite keys; 0 for single-field keys
- A field can participate in multiple GSI keys (multiple entries in `attributes`)
- The plugin generates `AutoPKSK`, typed GSI query methods, and HTTP query handlers
- See [Deployment](deployment.md) for DynamoDB table setup with GSIs
