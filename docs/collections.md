# Collections & Derived Fields

Aggregates can have collection fields (`map<string, Message>`) managed through ADD and REMOVE events. Derived fields (totals, counts) are computed via the `PostApplyHook` interface.

## Collection Fields

Collection fields must be `map<string, Message>` with string keys. The map key is a string field on the element message, identified by `key_field`.

### Element message

```protobuf
message LineItem {
  string item_id     = 1;  // map key
  string description = 2;
  int64  price_cents = 3;
  int32  quantity    = 4;
}
```

### Aggregate field

```protobuf
message Order {
  option (...).aggregate = {};
  // ... scalar fields ...
  map<string, LineItem> items = 14;
}
```

## Collection Events

An event either does collection work **or** scalar field copying -- not both. Each collection event has exactly one domain field (field 5+).

### ADD

The event carries an embedded message matching the map's value type:

```protobuf
message ItemAdded {
  option (...).event = {
    collection: { target: "items", action: COLLECTION_ACTION_ADD, key_field: "item_id" }
  };
  string   id      = 1;
  int64    version = 2;
  int64    at      = 3;
  string   actor   = 4;
  LineItem item    = 5;  // element to insert
}
```

Generated `On`:
```go
aggregate.Items[e.GetItem().GetItemId()] = e.GetItem()
```

Re-adding the same key overwrites (idempotent).

### REMOVE

The event carries a string field matching the `key_field`:

```protobuf
message ItemRemoved {
  option (...).event = {
    collection: { target: "items", action: COLLECTION_ACTION_REMOVE, key_field: "item_id" }
  };
  string id      = 1;
  int64  version = 2;
  int64  at      = 3;
  string actor   = 4;
  string item_id = 5;  // key identifying which element to delete
}
```

Generated `On`:
```go
delete(aggregate.Items, e.GetItemId())
```

REMOVE is not valid on creation events.

### Corresponding commands

Commands carry the same domain fields:

```protobuf
message AddItem {
  option (...).command = {
    produces_events: ["ItemAdded"]
    lifecycle: COMMAND_LIFECYCLE_MUTATION
    allowed_states: ["STATE_DRAFT"]
  };
  string   id    = 1;
  string   actor = 2;
  LineItem item  = 3;
}

message RemoveItem {
  option (...).command = {
    produces_events: ["ItemRemoved"]
    lifecycle: COMMAND_LIFECYCLE_MUTATION
    allowed_states: ["STATE_DRAFT"]
  };
  string id      = 1;
  string actor   = 2;
  string item_id = 3;
}
```

## Multiple Collections

An aggregate can have multiple independent collections, each with its own events:

```protobuf
message Order {
  option (...).aggregate = {};
  map<string, LineItem> items = 14;
  map<string, Tag>      tags  = 15;
}

// Tags use a separate pair of ADD/REMOVE events
message TagAdded {
  option (...).event = {
    collection: { target: "tags", action: COLLECTION_ACTION_ADD, key_field: "key" }
  };
  string id = 1; int64 version = 2; int64 at = 3; string actor = 4;
  Tag tag = 5;
}

message TagRemoved {
  option (...).event = {
    collection: { target: "tags", action: COLLECTION_ACTION_REMOVE, key_field: "key" }
  };
  string id = 1; int64 version = 2; int64 at = 3; string actor = 4;
  string key = 5;
}
```

## Derived Fields (PostApplyHook)

For computed fields that depend on collection state (totals, counts), implement `AfterOn()` on the aggregate. This is the `PostApplyHook` interface.

### When AfterOn is called

1. Once after **all events are replayed** during `Repository.Load`
2. Once after **all new events are applied** during materialization in `Repository.Apply`
3. In generated `EmitEvents` on the cloned aggregate before snapshot emission (only when a snapshot will actually be created)

It is **not** called per-event -- safe to iterate collections.

### Example

```go
// order_derived.go (hand-written, same package as generated code)
package orderv1

func (o *Order) AfterOn() {
    o.ItemCount = int32(len(o.Items))
    var total int64
    for _, item := range o.Items {
        total += item.GetPriceCents() * int64(item.GetQuantity())
    }
    o.TotalCents = total
}
```

The aggregate must declare the derived fields in proto:

```protobuf
message Order {
  // ... other fields ...
  int64 total_cents = 10;  // computed by AfterOn
  int32 item_count  = 11;  // computed by AfterOn
  map<string, LineItem> items = 14;
}
```

These fields are populated by `AfterOn`, not by events. No event should have `total_cents` or `item_count` -- they would be overwritten on next replay.

### Rules

- `AfterOn` is a **reserved method name** -- do not use it as a command or event message name
- Place the implementation in a separate hand-written file (e.g. `order_derived.go`) so `buf generate` doesn't overwrite it
- Derived fields are included in snapshots automatically (the snapshot captures the full aggregate state after `AfterOn` runs)

## CLI Generation

Commands with collection events (embedded message types like `LineItem item = 3`) produce a **stub** CLI manager (`*mgr/main.go`) instead of the interactive CLI. This is because non-scalar command fields can't be entered interactively. Use curl or the generated clients for testing these commands.

## Rules Summary

| Rule | Detail |
|------|--------|
| Map keys | `string` only |
| `key_field` | Required for both ADD and REMOVE; must name a string field on the element message |
| Domain fields | An event does collection OR scalar copying, not both |
| ADD event | Exactly one embedded field matching the map's value type |
| REMOVE event | A string field matching `key_field` |
| REMOVE on creation | Not valid |
| Multiple collections | Supported (each with independent events) |
