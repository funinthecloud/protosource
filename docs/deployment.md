# Deployment

Production setup with DynamoDB, Lambda or HTTP, and Wire dependency injection.

## DynamoDB Tables

Two tables: **events** (event stream) and **aggregates** (materialized state + GSI queries).

### Events Table

| Attribute | Key | Type | Description |
|-----------|-----|------|-------------|
| `a` | Partition | S | Aggregate ID |
| `v` | Sort | N | Version number |
| `d` | -- | B | Serialized event data |
| `t` | -- | N | TTL epoch seconds (optional) |

```yaml
EventsTable:
  Type: AWS::DynamoDB::Table
  Properties:
    TableName: events
    KeySchema:
      - AttributeName: a
        KeyType: HASH
      - AttributeName: v
        KeyType: RANGE
    AttributeDefinitions:
      - AttributeName: a
        AttributeType: S
      - AttributeName: v
        AttributeType: N
    BillingMode: PAY_PER_REQUEST
    TimeToLiveSpecification:
      AttributeName: t
      Enabled: true
```

### Aggregates Table

Single-table design via the opaquedata layer. PK/SK plus 20 GSI pairs.

| Attribute | Key | Type | Description |
|-----------|-----|------|-------------|
| `pk` | Partition | S | `package_v1#Aggregate#id` |
| `sk` | Sort | S | `"AGG"` for aggregates, `"PROJ#Name"` for projections |
| `gsi1pk`..`gsi20pk` | GSI partition | S | Query access patterns |
| `gsi1sk`..`gsi20sk` | GSI sort | S | Query sort keys |
| `body` | -- | B | Serialized aggregate/projection |
| `version` | -- | N | Aggregate version |
| `t` | -- | N | TTL epoch seconds (optional) |

> Empty GSIs cost nothing with PAY_PER_REQUEST billing. Always create all 20 pairs.

A reference CloudFormation template is at `stores/dynamodbstore/ddl/cloudformation.yaml`. For the full aggregates table with GSIs, generate it or use the pattern:

```yaml
AggregatesTable:
  Type: AWS::DynamoDB::Table
  Properties:
    TableName: aggregates
    KeySchema:
      - AttributeName: pk
        KeyType: HASH
      - AttributeName: sk
        KeyType: RANGE
    AttributeDefinitions:
      - AttributeName: pk
        AttributeType: S
      - AttributeName: sk
        AttributeType: S
      # ... gsi1pk through gsi20sk (all S)
    BillingMode: PAY_PER_REQUEST
    TimeToLiveSpecification:
      AttributeName: t
      Enabled: true
    GlobalSecondaryIndexes:
      - IndexName: gsi1
        KeySchema:
          - AttributeName: gsi1pk
            KeyType: HASH
          - AttributeName: gsi1sk
            KeyType: RANGE
        Projection:
          ProjectionType: ALL
      # ... repeat for gsi2 through gsi20
```

### Attribute naming

All attribute names use single characters (`a`, `v`, `d`, `t`) or short names (`pk`, `sk`, `gsi1pk`) to minimize per-item byte costs. DynamoDB charges per byte for reads/writes, and attribute names are included in every item.

## DynamoDB Store Setup

```go
import (
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"

    "github.com/funinthecloud/protosource"
    "github.com/funinthecloud/protosource/serializers/protobinaryserializer"
    "github.com/funinthecloud/protosource/stores/dynamodbstore"
    opaquedynamo "github.com/funinthecloud/protosource/opaquedata/dynamo"
)

// Create AWS client
cfg, _ := config.LoadDefaultConfig(ctx)
client := dynamodb.NewFromConfig(cfg)

// Create opaquedata store (for materialized aggregates + GSI queries)
opaqueStore := opaquedynamo.New(client, "aggregates")

// Create event store with materialization support
store, _ := dynamodbstore.New(
    client,
    dynamodbstore.WithEventsTable("events"),
    dynamodbstore.WithOpaqueStore(opaqueStore),
)

// Create repository
repo := protosource.New(
    &taskv1.Task{},
    store,
    protobinaryserializer.NewSerializer(),
)
```

Use `protobinaryserializer` in production (compact, fast). Use `protojsonserializer` only in development/tests for readable output.

## Lambda Deployment

The `adapters/awslambda` package converts API Gateway proxy requests to protosource's `Request`/`Response`.

```go
import (
    "github.com/aws/aws-lambda-go/events"
    "github.com/aws/aws-lambda-go/lambda"
    "github.com/funinthecloud/protosource"
    "github.com/funinthecloud/protosource/adapters/awslambda"
)

func main() {
    // ... create store, repo, handler as above ...

    handler := taskv1.NewHandler(repo, taskv1.NewTaskClient(opaqueStore))
    router := protosource.NewRouter(handler)

    // Extract actor from Authorization header (JWT sub claim, etc.)
    extractor := func(req events.APIGatewayProxyRequest) string {
        return req.RequestContext.Authorizer["principalId"].(string)
    }

    lambda.Start(awslambda.WrapRouter(router, extractor))
}
```

### API Gateway configuration

- Use REST API (v1) with proxy integration (`{proxy+}`)
- The router handles all path matching internally
- CORS headers are included in generated responses

## HTTP Standard Deployment

For non-Lambda deployments (ECS, Cloud Run, bare metal):

```go
import (
    "net/http"
    "github.com/funinthecloud/protosource/adapters/httpstandard"
)

handler := taskv1.NewHandler(repo, taskv1.NewTaskClient(opaqueStore))
router := protosource.NewRouter(handler)

// BearerTokenExtractor reads the Authorization header
httpHandler := httpstandard.WrapRouter(router, httpstandard.BearerTokenExtractor)

http.ListenAndServe(":8080", httpHandler)
```

### Actor extractors

| Extractor | Source | Use case |
|-----------|--------|----------|
| `BearerTokenExtractor` | `Authorization: Bearer <token>` | JWT-based auth |
| `HeaderExtractor("X-Actor")` | Custom header | Development, service-to-service |
| Custom `func(*http.Request) string` | Any | Custom auth schemes |

## Wire Dependency Injection

Generated code includes Wire provider sets for each aggregate. The pattern:

### Generated providers (per aggregate)

Each aggregate gets a `*.protosource.wire.pb.go` in its base package:

```go
// task/v1/task_v1.protosource.wire.pb.go (generated)
package taskv1

type Repository struct{ *protosource.Repository }

func ProvideRepository(store protosource.Store, serializer protosource.Serializer) *Repository {
    return &Repository{NewRepository(store, serializer)}
}

var ProviderSet = wire.NewSet(
    ProvideRepository,
    wire.Bind(new(Repo), new(*Repository)),
)
```

### Shared infrastructure providers

`dynamodbstore/providers.go` provides Wire-compatible constructors:

```go
// Named types for Wire to distinguish table name strings
type EventsTableName string
type AggregatesTableName string

func ProvideOpaqueStore(client dynamoclient.Client, table AggregatesTableName) *opaquedynamo.Store
func ProvideStore(client dynamoclient.Client, opaqueStore *opaquedynamo.Store, table EventsTableName) (*DynamoDBStore, error)
```

### Wiring it together

```go
// wire.go
//go:build wireinject

package main

import (
    "github.com/goforj/wire"
    "github.com/funinthecloud/protosource/aws/dynamoclient"
    "github.com/funinthecloud/protosource/stores/dynamodbstore"
    taskv1 "github.com/yourorg/task-app/task/v1"
)

func InitializeApp(
    client dynamoclient.Client,
    eventsTable dynamodbstore.EventsTableName,
    aggregatesTable dynamodbstore.AggregatesTableName,
) (*App, error) {
    wire.Build(
        dynamodbstore.ProvideOpaqueStore,
        dynamodbstore.ProvideStore,
        wire.Bind(new(protosource.Store), new(*dynamodbstore.DynamoDBStore)),
        taskv1.ProviderSet,
        // ... handler, router providers ...
        NewApp,
    )
    return nil, nil
}
```

The `dynamoclient.Client` interface accepts the real `*dynamodb.Client` from the AWS SDK, keeping Wire graphs testable.

## Event TTL

For ephemeral aggregates, set `event_ttl_seconds` in the proto annotation:

```protobuf
message TempSession {
  option (...).aggregate = { event_ttl_seconds: 86400 };
}
```

The repository stamps each event record with a TTL before persisting. DynamoDB deletes expired items asynchronously (typically within 48 hours of expiry). The events table must have TTL enabled on the `t` attribute.

## Snapshots in Production

For high-event-volume aggregates, add a Snapshot message:

```protobuf
message Snapshot {
  option (...).snapshot = { every_n_events: 50 };
  string id       = 1;
  int64  version  = 2;
  int64  at       = 3;
  string actor    = 4;
  Task   snapshot = 5;
}
```

With snapshots enabled, `Load` uses `LoadTail` (last N events) instead of loading the full history. The DynamoDBStore implements `SnapshotTailStore` via a reverse query with `Limit`.
