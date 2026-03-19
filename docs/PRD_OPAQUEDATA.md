# PRD: OpaqueData — Proto-to-DynamoDB Single-Table Storage Layer

## 1. Problem Statement

We need a Go library that enables storing **arbitrary protobuf messages** in a **single DynamoDB table** with support for up to **5 Global Secondary Indexes (GSIs)**. The proto message's body is stored as an opaque binary blob, while index keys are extracted and stored as top-level DynamoDB attributes. This allows multiple unrelated proto message types to coexist in one table, queried via shared GSI infrastructure.

## 2. Core Concept

```
┌─────────────────────────────────────────────────────────────────────────┐
│  DynamoDB Table: "globaldata"                                          │
├────────┬────────┬───────┬────────┬───────┬────────┬────────┬───────────┤
│ pk (S) │ sk (S) │ body  │ flags  │ ttl   │ gsi1pk │ gsi1sk │ ...gsi5sk│
│        │        │ (B)   │ (N)    │ (N)   │ (S)    │ (S)    │ (S)      │
├────────┼────────┼───────┼────────┼───────┼────────┼────────┼───────────┤
│type#   │type#   │<proto>│ 0x01   │ epoch │ ...    │ ...    │ ...      │
│fields  │fields  │bytes  │        │       │        │        │          │
└────────┴────────┴───────┴────────┴───────┴────────┴────────┴───────────┘
```

**Each proto message type** implements an interface that extracts its own PK, SK, and GSI keys from its field values. The keys follow a convention: `<package>#<messagename>#<fieldname>#<value>` — this ensures uniqueness across message types sharing the same table.

**The body** is the full `proto.Marshal()` of the business proto. Transformers (compression, etc.) can process the body before write and after read.

## 3. Requirements

### 3.1 Proto Definition: `OpaqueData`

The storage-layer message. This is what actually gets persisted to DynamoDB.

```protobuf
syntax = "proto3";
package opaquedata;
option go_package = "your/module/opaquedata";

message OpaqueData {
  string pk     = 1;   // Partition key
  string sk     = 2;   // Sort key
  int64  ttl    = 3;   // DynamoDB TTL (unix epoch seconds, 0 = no expiry)
  int64  version = 4;  // Reserved for optimistic locking (currently unused)
  uint32 flags  = 5;   // Bitmask for body transformations (e.g. bit 0 = compressed)
  bytes  body   = 6;   // Serialized proto (possibly compressed)
  string gsi1pk = 7;
  string gsi1sk = 8;
  string gsi2pk = 9;
  string gsi2sk = 10;
  string gsi3pk = 11;
  string gsi3sk = 12;
  string gsi4pk = 13;
  string gsi4sk = 14;
  string gsi5pk = 15;
  string gsi5sk = 16;
}
```

### 3.2 Interfaces

Business protos must implement these interfaces (typically via code generation):

```go
// AutoPKSK — implemented by each business proto to extract DynamoDB keys from its fields.
type AutoPKSK interface {
    proto.Message
    DynamoPK() string
    DynamoSK() string
    DynamoGSI1PK() string
    DynamoGSI1SK() string
    DynamoGSI2PK() string
    DynamoGSI2SK() string
    DynamoGSI3PK() string
    DynamoGSI3SK() string
    DynamoGSI4PK() string
    DynamoGSI4SK() string
    DynamoGSI5PK() string
    DynamoGSI5SK() string
}

// Hydrater — implemented by each business proto to deserialize itself from the opaque body.
type Hydrater interface {
    proto.Message
    Hydrate(body []byte) error
}
```

**Key format convention** (generated per-message):
```
PK:  "<package>#<message>#<pk_field1>#<value1>#<pk_field2>#<value2>"
SK:  "<package>#<message>#<sk_field>#<value>"
GSI: same pattern, "" or "NA" when unused
```

**Hydrate implementation** (generated per-message):
```go
func (m *MyMessage) Hydrate(input []byte) error {
    return proto.Unmarshal(input, m)
}
```

### 3.3 Serialize Path (Proto -> DynamoDB)

`NewOpaqueDataFromProto(input AutoPKSK) (*OpaqueData, error)`:

1. `proto.Marshal(input)` to get the body bytes
2. Populate `OpaqueData` with keys from the `AutoPKSK` interface methods
3. Run the body through the transformer pipeline (Serialize direction)
4. Return the populated `OpaqueData`

### 3.4 Deserialize Path (DynamoDB -> Proto)

`(*OpaqueData).ReHydrate(tgt Hydrater) error`:

1. Run the body through the transformer pipeline (Deserialize direction — reverse order)
2. Call `tgt.Hydrate(body)` which does `proto.Unmarshal`

### 3.5 Transformer Pipeline

A chain of `Transformer` implementations applied in order on serialize, and in **reverse order** on deserialize.

```go
type Transformer interface {
    Serialize(i *OpaqueDataBody) (*OpaqueDataBody, error)
    Deserialize(i *OpaqueDataBody) (*OpaqueDataBody, error)
}

type OpaqueDataBody struct {
    Flags uint32
    Body  []byte
}
```

Each transformer owns a bit in `Flags` to track whether its transformation has been applied:

| Bit | Flag             | Meaning          |
|-----|------------------|------------------|
| 0   | `FlagCompressed` | gzip compressed  |
| 1   | (reserved)       | (future use)     |

**Compress Transformer** (the one currently in use):
- **Serialize**: if body length >= threshold (default 300 bytes) and not already flagged, gzip the body and set the flag bit
- **Deserialize**: if flag bit is set, gunzip and clear the flag bit
- Threshold is configurable via env var `OPAQUEDATA_COMPRESS_THRESHOLD`

### 3.6 DynamoDB Attribute Helpers

Methods on `OpaqueData` to produce DynamoDB SDK attribute maps:

| Method | Returns | Purpose |
|--------|---------|---------|
| `GetKey()` | `map[string]AttributeValue` | `{pk, sk}` — for GetItem/DeleteItem/UpdateItem Key param |
| `GetItem()` | `map[string]AttributeValue` | All fields — for PutItem |
| `GetItems(names ...string)` | `map[string]AttributeValue` | Specified fields only — for selective PutItem |
| `GetExpressionValues(names ...string)` | `map[string]AttributeValue` | Specified fields with `:` prefix on keys — for UpdateItem ExpressionAttributeValues |
| `GetValue()` | `map[string]AttributeValue` | All non-key fields — for value-only operations |

### 3.7 Query Helper

```go
func QueryPKSK(ctx, client, tableName, gsiIndex, partitionValue, *SortCondition) ([]OpaqueData, error)
```

- Queries the table (or a GSI by index number 1-5)
- GSI index names follow convention: `gsi{N}pk-gsi{N}sk-index`
- Supports sort key conditions: `=`, `<`, `<=`, `>`, `>=`, `BETWEEN`, `begins_with`
- **Automatically filters expired TTL items** via FilterExpression: `attribute_not_exists(#ttl) or #ttl = :zero or #ttl > :now`
- **Automatically paginates** through all result pages
- Returns `[]OpaqueData` which callers then ReHydrate into business protos

### 3.8 TTL Support

- Functional option: `WithTTL(d time.Duration)`
- `GetTTL(*time.Duration) int64` converts a duration to a unix epoch timestamp (now + duration)
- TTL = 0 means no expiry
- DynamoDB's native TTL mechanism handles actual deletion

### 3.9 Sort Operators

```go
type SortOperator int
const (
    SortOperatorEqual      // =
    SortOperatorLt         // <
    SortOperatorLe         // <=
    SortOperatorGt         // >
    SortOperatorGe         // >=
    SortOperatorBetween    // BETWEEN (requires exactly 2 values)
    SortOperatorBeginsWith // begins_with (requires exactly 1 value)
)
```

## 4. Generated Client Pattern (for reference)

A code generator (protoc plugin) produces a typed client per proto package. The generated client uses OpaqueData internally but exposes a typed API to consumers. Here is the pattern for each CRUD operation:

### Add (PutItem)
```
1. Apply functional options (TTL, etc.)
2. Call businessProto.GetItem() -> map of DynamoDB attributes (calls NewOpaqueDataFromProto internally)
3. Set TTL attribute on the map
4. dynamodb.PutItem(item)
```

### BatchAdd
```
Same as Add but batches into groups of 25, retries unprocessed items.
```

### Get (GetItem)
```
1. Construct a minimal business proto with just the key fields populated
2. NewOpaqueDataFromProto(minimalProto) to get the OpaqueData (we only need the key)
3. od.GetKey() for the DynamoDB key
4. dynamodb.GetItem(key)
5. attributevalue.UnmarshalMap(result, &od) to populate the OpaqueData from the response
6. od.ReHydrate(&businessProto) to deserialize the body back into a typed proto
```

### Update (UpdateItem)
```
1. Apply functional options
2. NewOpaqueDataFromProto(businessProto) for the full OpaqueData
3. od.GetKey() for the key
4. od.GetExpressionValues("body", "flags", "gsi1pk", ...) for the update expression values
5. Build update expression: "set body = :body, flags = :flags, gsi1pk = :gsi1pk, ..."
6. Optionally add TTL to the expression
7. dynamodb.UpdateItem(key, expression, values)
```

### Delete (DeleteItem)
```
1. Construct minimal proto with key fields
2. NewOpaqueDataFromProto -> od.GetKey()
3. dynamodb.DeleteItem(key)
```

### Select/Query (Query with pagination)
```
1. Build the PK string using the same convention as DynamoPK()/DynamoGSI{N}PK()
2. Optionally build SortCondition for SK filtering
3. QueryPKSK(ctx, client, table, GSI{N}, pk, sortCondition) -> []OpaqueData
4. For each result: od.ReHydrate(&businessProto)
```

## 5. Bugs and Issues to Avoid in Reimplementation

### BUG 1: Swallowed marshal errors in `dynamo.pb.go`
```go
// CURRENT (broken): uses fmt.Errorf but discards the return value
fmt.Errorf("failed attributevalue.Marshal(%v): %w", k, err)
// FIX: must return the error
return nil, fmt.Errorf(...)
```
This affects `GetItem()`, `GetItems()`, `GetKey()`, `GetValue()`, and `GetExpressionValues()`. **Every marshal error is silently swallowed.** A nil AttributeValue will be written to DynamoDB, causing silent data corruption.

### BUG 2: GetItem for Get/Delete unnecessarily serializes the full proto
The `GetJunkData` and `DeleteJunkData` methods call `NewOpaqueDataFromProto()` (which does `proto.Marshal` + compression) just to extract PK/SK. This is wasteful — only the key methods are needed. Consider a lighter `NewOpaqueKeyFromProto()` that skips body serialization.

### ISSUE 3: Hardcoded `America/New_York` timezone for TTL
```go
var Now = func() time.Time {
    return time.Now().In(AmericaNewYorkLoc)
}
```
TTL is unix epoch seconds which is timezone-independent. The timezone conversion is pointless and confusing. Just use `time.Now().Unix()`.

### ISSUE 4: Global mutable `AllTransformers` initialized via `init()`
```go
var AllTransformers Transformer = func() Transformer { ... }()
```
This panics at startup if the env var config fails. It also makes testing harder. Consider dependency injection instead.

### ISSUE 5: `version` field is declared but never used
The `version` field in OpaqueData is always set to 0. If optimistic locking is intended, it should either be implemented or the field removed to avoid confusion.

### ISSUE 6: UpdateItem only updates a hardcoded subset of GSI keys
The generated `UpdateJunkData` only updates `gsi1pk, gsi1sk, gsi2pk, gsi2sk` but not `gsi3-5`. If a message uses GSI3-5, updates would leave stale index values. The generator should include all GSIs that the message declares.

### ISSUE 7: No consistent error sentinel for "not found"
`GetItem` returns `clients.ErrNoItem` but `QueryPKSK` returns an empty slice. Callers must handle "not found" differently depending on the access pattern.

### ISSUE 8: Compression error handling is inconsistent
In `Serialize`, if `compressor.Write` returns a short write, it falls through to return the *original uncompressed* body but with the compressed flag potentially already set on `ni`. The flag mutation happens before the write attempt.

### ISSUE 9: `deprecated github.com/golang/protobuf`
The v2 implementation still uses `github.com/golang/protobuf/proto` (the deprecated v1 API). Should use `google.golang.org/protobuf/proto`.

## 6. DynamoDB Table Schema (for Terraform/CloudFormation)

```
Table:
  PK: pk (S)
  SK: sk (S)
  TTL attribute: ttl

GSI1: gsi1pk-gsi1sk-index  (PK: gsi1pk, SK: gsi1sk, projection: ALL)
GSI2: gsi2pk-gsi2sk-index  (PK: gsi2pk, SK: gsi2sk, projection: ALL)
GSI3: gsi3pk-gsi3sk-index  (PK: gsi3pk, SK: gsi3sk, projection: ALL)
GSI4: gsi4pk-gsi4sk-index  (PK: gsi4pk, SK: gsi4sk, projection: ALL)
GSI5: gsi5pk-gsi5sk-index  (PK: gsi5pk, SK: gsi5sk, projection: ALL)
```

All GSI key attributes are type `S` (string). The `body` attribute is type `B` (binary). The `flags` attribute is type `N` (number). The `ttl` attribute is type `N` (number).

## 7. Package Structure (Recommended)

```
opaquedata/
├── opaquedata.proto          # Proto definition
├── opaquedata.pb.go          # Generated proto code
├── opaquedata_dynamo.go      # GetKey, GetItem, GetItems, GetExpressionValues, GetValue
├── helpers.go                # NewOpaqueDataFromProto, ReHydrate, AutoPKSK, Hydrater interfaces
├── options.go                # Option, OpaqueDataOptions, WithTTL, GetTTL
├── query.go                  # QueryPKSK, SortCondition, SortOperator, gsiIndex constants
└── transformer/
    ├── transformer.go        # Transformer interface, OpaqueDataTransformer chain
    ├── constants.go          # Flag bitmask constants
    └── compress.go           # CompressTransformer (gzip, threshold-based)
```

## 8. Dependencies

- `google.golang.org/protobuf/proto` (use v2, not deprecated v1)
- `github.com/aws/aws-sdk-go-v2/service/dynamodb`
- `github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue`
- `compress/gzip` (stdlib)

## 9. Acceptance Criteria

1. Any proto message implementing `AutoPKSK` + `Hydrater` can be stored and retrieved
2. Multiple unrelated message types coexist in the same table without key collisions
3. Compression is transparent — callers never see compressed bytes
4. TTL items are automatically filtered out of query results
5. Query pagination is automatic and complete
6. All marshal/unmarshal errors are properly returned (not swallowed)
7. Transformer pipeline is injectable (not global mutable state)
8. Round-trip fidelity: `Marshal -> Store -> Read -> Unmarshal` produces identical proto
