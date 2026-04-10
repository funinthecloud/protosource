# Quickstart

Build an event-sourced Task manager with a Go backend and TypeScript client. By the end you'll have:

- A `Task` aggregate with Create, Complete, and Reopen commands
- Generated Go handlers serving HTTP
- A generated TypeScript client calling those endpoints

## Prerequisites

- Go 1.25+
- [buf CLI](https://buf.build/docs/installation)
- Node 20+ and npm (for TypeScript)
- clang-format (proto formatting)
- protoc-gen-go: `go install google.golang.org/protobuf/cmd/protoc-gen-go@latest`
- protoc-gen-es: `npm install -g @bufbuild/protoc-gen-es` (for TypeScript generation)

## 1. Install the plugins

```bash
git clone https://github.com/funinthecloud/protosource.git
cd protosource
go install ./cmd/protoc-gen-protosource
go install ./cmd/protoc-gen-protosource-ts
```

This puts `protoc-gen-protosource` and `protoc-gen-protosource-ts` in your `$GOPATH/bin`.

## 2. Create your project

```bash
mkdir task-app && cd task-app
go mod init github.com/yourorg/task-app
```

### buf.yaml

```yaml
version: v2
modules:
  - path: proto
    name: buf.build/yourorg/task-app
deps:
  - buf.build/funinthecloud/protosource
  - buf.build/bufbuild/protovalidate
```

### buf.gen.yaml (Go)

```yaml
version: v2
managed:
  enabled: true
  disable:
    - file_option: go_package
plugins:
  - local: protoc-gen-go
    out: .
    opt:
      - module=github.com/yourorg/task-app
  - local: protoc-gen-protosource
    out: .
    opt:
      - module=github.com/yourorg/task-app
```

### buf.gen.ts.yaml (TypeScript)

```yaml
version: v2
managed:
  enabled: true
plugins:
  - local: protoc-gen-es
    out: ts-gen
    opt:
      - target=ts
  - local: protoc-gen-protosource-ts
    out: ts-gen
    opt:
      - module=github.com/yourorg/task-app
```

Run `buf dep update` to fetch the dependencies.

## 3. Define your domain

Create `proto/task/v1/task_v1.proto`:

```protobuf
syntax = "proto3";

package task.v1;

import "buf/validate/validate.proto";
import "funinthecloud/protosource/options/v1/options_v1.proto";

option go_package = "github.com/yourorg/task-app/task/v1;taskv1";

option (funinthecloud.protosource.options.v1.protosource_file).enabled = true;

// -- Aggregate --

enum State {
  STATE_UNSPECIFIED = 0;
  STATE_OPEN        = 1;
  STATE_COMPLETED   = 2;
}

message Task {
  option (funinthecloud.protosource.options.v1.protosource_message_type).aggregate = {};

  string id         = 1;
  int64  version    = 2;
  int64  created_at = 3;
  int64  modified_at = 4;
  string created_by = 5;
  string modified_by = 6;
  State  state      = 7;
  string title      = 8;
}

// -- Commands --

message Create {
  option (funinthecloud.protosource.options.v1.protosource_message_type).command = {
    produces_events: ["Created"]
    lifecycle:       COMMAND_LIFECYCLE_CREATION
  };

  string id    = 1 [(buf.validate.field).string.min_len = 1];
  string actor = 2 [(buf.validate.field).string.min_len = 1];
  string title = 3 [(buf.validate.field).string.min_len = 1];
}

message Complete {
  option (funinthecloud.protosource.options.v1.protosource_message_type).command = {
    produces_events: ["Completed"]
    lifecycle:       COMMAND_LIFECYCLE_MUTATION
    allowed_states:  ["STATE_OPEN"]
  };

  string id    = 1;
  string actor = 2;
}

message Reopen {
  option (funinthecloud.protosource.options.v1.protosource_message_type).command = {
    produces_events: ["Reopened"]
    lifecycle:       COMMAND_LIFECYCLE_MUTATION
    allowed_states:  ["STATE_COMPLETED"]
  };

  string id    = 1;
  string actor = 2;
}

// -- Events --

message Created {
  option (funinthecloud.protosource.options.v1.protosource_message_type).event = {
    sets_state: "STATE_OPEN"
  };

  string id      = 1;
  int64  version = 2;
  int64  at      = 3;
  string actor   = 4;
  string title   = 5;
}

message Completed {
  option (funinthecloud.protosource.options.v1.protosource_message_type).event = {
    sets_state: "STATE_COMPLETED"
  };

  string id      = 1;
  int64  version = 2;
  int64  at      = 3;
  string actor   = 4;
}

message Reopened {
  option (funinthecloud.protosource.options.v1.protosource_message_type).event = {
    sets_state: "STATE_OPEN"
  };

  string id      = 1;
  int64  version = 2;
  int64  at      = 3;
  string actor   = 4;
}
```

Format it:

```bash
clang-format --style=file -i proto/task/v1/task_v1.proto
```

> **Note:** You'll need a `.clang-format` config. Copy the one from the protosource repo, or use `BasedOnStyle: Google` at minimum.

## 4. Generate code

```bash
buf generate                             # Go code
buf generate --template buf.gen.ts.yaml  # TypeScript client
```

This produces:

| File | Purpose |
|------|---------|
| `task/v1/task_v1.pb.go` | Proto message types (by protoc-gen-go) |
| `task/v1/task_v1.protosource.pb.go` | `On`, builders, event emission, validation, authorization |
| `task/v1/task_v1.protosource.lambda.pb.go` | HTTP handlers (Create, Complete, Reopen, Get, History) |
| `task/v1/task_v1.protosource.wire.pb.go` | Wire dependency injection providers |
| `task/v1/task_v1.protosource.client.pb.go` | Typed Go HTTP client |
| `task/v1/taskmgr/main.go` | CLI manager for interactive testing |
| `ts-gen/task/v1/task_v1_pb.ts` | TypeScript proto types (by protoc-gen-es) |
| `ts-gen/task/v1/task_v1.protosource.client.ts` | TypeScript HTTP client |

## 5. Wire up a Go backend

Add dependencies:

```bash
go get github.com/funinthecloud/protosource
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/funinthecloud/protosource"
	"github.com/funinthecloud/protosource/adapters/httpstandard"
	"github.com/funinthecloud/protosource/serializers/protojsonserializer"
	"github.com/funinthecloud/protosource/stores/memorystore"

	taskv1 "github.com/yourorg/task-app/task/v1"
)

func main() {
	store := memorystore.New()
	serializer := protojsonserializer.NewSerializer()

	repo := protosource.New(
		&taskv1.Task{},
		store,
		serializer,
	)

	handler := taskv1.NewHandler(repo, nil)
	router := protosource.NewRouter(handler)

	// HeaderExtractor reads actor identity from X-Actor header.
	// In production, use BearerTokenExtractor or a custom extractor.
	httpHandler := httpstandard.WrapRouter(router, httpstandard.HeaderExtractor("X-Actor"))

	fmt.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", httpHandler))
}
```

Run it:

```bash
go run main.go
```

> **Note:** We pass `nil` for the second argument to `NewHandler` because GSI queries require DynamoDB. Commands, Load, and History all work with memorystore. See [Deployment](deployment.md) for the DynamoDB setup.

## 6. Test with curl

```bash
# Create a task
curl -s -X POST http://localhost:8080/task/v1/create \
  -H "Content-Type: application/json" \
  -H "X-Actor: alice" \
  -d '{"id":"task-1","title":"Write documentation"}' | jq

# Load the task (event replay)
curl -s http://localhost:8080/task/v1/task-1 \
  -H "X-Actor: alice" | jq

# Complete it
curl -s -X POST http://localhost:8080/task/v1/complete \
  -H "Content-Type: application/json" \
  -H "X-Actor: alice" \
  -d '{"id":"task-1"}' | jq

# Try to complete again -- should fail with STATE_COMPLETED not in allowed_states
curl -s -X POST http://localhost:8080/task/v1/complete \
  -H "Content-Type: application/json" \
  -H "X-Actor: alice" \
  -d '{"id":"task-1"}' | jq

# Reopen it
curl -s -X POST http://localhost:8080/task/v1/reopen \
  -H "Content-Type: application/json" \
  -H "X-Actor: alice" \
  -d '{"id":"task-1"}' | jq

# View event history
curl -s http://localhost:8080/task/v1/task-1/history \
  -H "X-Actor: alice" | jq
```

## 7. Use the generated CLI

The code generator also produces an interactive CLI manager at `task/v1/taskmgr/main.go`:

```bash
go run ./task/v1/taskmgr/
```

This gives you an interactive prompt to send commands and inspect aggregate state without writing curl commands.

## 8. TypeScript client

Install the runtime package and protobuf dependency:

```bash
npm install @protosource/client @bufbuild/protobuf
```

The generated client at `ts-gen/task/v1/task_v1.protosource.client.ts` provides a typed API:

```typescript
import { TaskClient } from "./ts-gen/task/v1/task_v1.protosource.client.js";
import { BearerTokenAuth, NoAuth } from "@protosource/client";

// For development (no auth, actor identified by name)
const client = new TaskClient("http://localhost:8080", new NoAuth("alice"));

// Create a task
const result = await client.create({ id: "task-2", title: "Build frontend" });
console.log("version:", result.version);

// Load current state
const task = await client.load("task-2");
console.log("state:", task.state);

// Complete it
await client.complete({ id: "task-2" });

// View history
const history = await client.history("task-2");
console.log("events:", history.records.length);
```

For authenticated environments, use `BearerTokenAuth`:

```typescript
const client = new TaskClient(
  "https://api.example.com",
  new BearerTokenAuth("your-jwt-token", "alice"),
);
```

## 9. Write tests

Use `memorystore` with `protojsonserializer` for readable test output:

```go
package taskv1_test

import (
	"context"
	"errors"
	"testing"

	"github.com/funinthecloud/protosource"
	"github.com/funinthecloud/protosource/serializers/protojsonserializer"
	"github.com/funinthecloud/protosource/stores/memorystore"

	taskv1 "github.com/yourorg/task-app/task/v1"
)

func newTestRepo() *protosource.Repository {
	return protosource.New(
		&taskv1.Task{},
		memorystore.New(),
		protojsonserializer.NewSerializer(),
	)
}

func TestCreateAndComplete(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()

	_, err := repo.Apply(ctx, &taskv1.Create{Id: "t1", Actor: "alice", Title: "Test"})
	if err != nil {
		t.Fatal(err)
	}

	agg, _ := repo.Load(ctx, "t1")
	task := agg.(*taskv1.Task)
	if task.GetState() != taskv1.State_STATE_OPEN {
		t.Fatalf("expected STATE_OPEN, got %s", task.GetState())
	}

	_, err = repo.Apply(ctx, &taskv1.Complete{Id: "t1", Actor: "alice"})
	if err != nil {
		t.Fatal(err)
	}

	agg, _ = repo.Load(ctx, "t1")
	task = agg.(*taskv1.Task)
	if task.GetState() != taskv1.State_STATE_COMPLETED {
		t.Fatalf("expected STATE_COMPLETED, got %s", task.GetState())
	}
}

func TestCompleteRequiresOpenState(t *testing.T) {
	repo := newTestRepo()
	ctx := context.Background()

	repo.Apply(ctx, &taskv1.Create{Id: "t1", Actor: "alice", Title: "Test"})
	repo.Apply(ctx, &taskv1.Complete{Id: "t1", Actor: "alice"})

	_, err := repo.Apply(ctx, &taskv1.Complete{Id: "t1", Actor: "alice"})
	if !errors.Is(err, protosource.ErrStateNotAllowed) {
		t.Fatalf("expected ErrStateNotAllowed, got: %v", err)
	}
}
```

## Next steps

- [Proto Annotations Reference](proto-annotations.md) -- all available annotations
- [Collections & Derived Fields](collections.md) -- map fields, PostApplyHook
- [Command Processing Pipeline](pipeline.md) -- deep-dive into Apply
- [Deployment](deployment.md) -- DynamoDB, Lambda, Wire DI
