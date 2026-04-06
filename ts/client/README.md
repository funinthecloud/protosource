# @protosource/client

TypeScript HTTP client runtime for [protosource](https://github.com/funinthecloud/protosource) event sourcing framework.

Generated client classes (from `protoc-gen-protosource-ts`) extend `ProtosourceClient` to provide typed methods for each aggregate's commands, queries, and state loading.

## Install

```bash
npm install @protosource/client @bufbuild/protobuf
```

## Usage

```typescript
import { TaskClient } from "./generated/task_v1.protosource.client.js";
import { NoAuth, BearerTokenAuth } from "@protosource/client";

// Development (no auth)
const client = new TaskClient("http://localhost:8080", new NoAuth("alice"));

// Production (JWT)
const client = new TaskClient("https://api.example.com", new BearerTokenAuth("token", "alice"));

await client.create({ id: "task-1", title: "Ship it" });
const task = await client.load("task-1");
const history = await client.history("task-1");
```

## API

### `ProtosourceClient`

Base class used by generated clients.

| Method | Description |
|--------|-------------|
| `apply(route, schema, data)` | Send a command |
| `load(route, id, schema)` | Load aggregate state via event replay |
| `get(route, id, schema)` | Load from materialized store |
| `history(route, id)` | Get full event history |
| `query(route, queryPath, params, schema)` | Query via GSI index |

### Auth providers

| Class | Constructor | Description |
|-------|------------|-------------|
| `NoAuth` | `new NoAuth(actor)` | No auth headers, actor identity only |
| `BearerTokenAuth` | `new BearerTokenAuth(token, actor)` | `Authorization: Bearer <token>` |

Implement `AuthProvider` for custom auth schemes.

### `APIError`

Thrown on non-2xx responses. Fields: `statusCode`, `code`, `message`, `detail`.

### `ClientOptions`

| Option | Default | Description |
|--------|---------|-------------|
| `useJSON` | `false` | Use JSON instead of protobuf binary for requests |
