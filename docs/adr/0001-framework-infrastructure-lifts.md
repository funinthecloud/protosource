# ADR-0001: Lifting reusable infrastructure from protosource-auth into core framework

**Status:** Accepted  
**Date:** 2026-05-21  
**Deciders:** Planning session following experience report in `FRAMEWORK_LIFT_CANDIDATES.md`  
**Related:**  
- [FRAMEWORK_LIFT_CANDIDATES.md](../../FRAMEWORK_LIFT_CANDIDATES.md) (primary source input and raw notes)  
- [TODO.md](../../TODO.md) (implementation tracking)  
- `protosource-auth` (first real consumer that surfaced these patterns)

---

## Context

Building the first substantial protosource-based service (`protosource-auth`, centered on authentication/authorization) required implementing several pieces of cross-cutting infrastructure:

- Envelope encryption key provider abstraction + three concrete backends (local, AWS KMS, Azure Key Vault)
- Multi-backend storage + keyprovider wiring ("the big switch" on `Backend` + `KeyProvider` plus all the `PROTOSOURCE_*` env handling, aliasing, and `Ensure*` calls)
- Operational admin CLI skeleton (cross-aggregate commands that talk directly to repositories, bypassing HTTP)
- Small HTTP composition helpers (`MountRouter` + CORS + eTLD+1 cookie domain scoping via `publicsuffix`)
- AWS deployment module parity with the existing Azure modules (`cosmos-eventstore` + `container-app-service`)
- A first-run bootstrap hook pattern

Some of these are genuinely reusable (the second protosource service will re-implement them almost byte-for-byte). Others only look generic or are auth-product-specific.

The guiding question throughout: *Would the second (and third) protosource service re-invent this?* If yes → lift. The source document `FRAMEWORK_LIFT_CANDIDATES.md` performed the detailed analysis and produced a recommended set of lifts plus explicit "keep in auth" guidance.

This ADR records the decisions from the planning review of that document.

---

## Decision

We will lift the following reusable infrastructure into `github.com/funinthecloud/protosource`. The lifts are scoped narrowly. The harness must **not** become "the way to write an HTTP service."

### 1. Lift `keyproviders/` (interface + all three implementations)

- New top-level package: `protosource/keyproviders/`
- Subpackages: `local/`, `awskms/`, `azurekeyvault/`
- Interface remains exactly as defined in auth (tiny `Name() / Encrypt / Decrypt` contract for envelope encryption).
- Auth keeps only its policy layer (`keys.Resolver` for per-day signing keys).

**Placement rationale:** Sibling to `stores/` (both are "infrastructure adapters" for cross-cloud concerns). Not under `crypto/` (framework deliberately does not own general crypto primitives such as password hashing or JWT signing — see the explicit "keep" decisions below).

### 2. Lift storage + keyprovider harness (`host` package)

New package: **`protosource/host`**

Core types (initial sketch — exact names and structure to be refined in implementation):

```go
type BaseConfig struct {
    ListenAddr   string
    Backend      string // memory | dynamodb | cosmosdb
    // EventsTable, AggregatesTable, AWSEndpoint, AWSRegion, ...
    // Cosmos* fields
    // KeyProvider, MasterKey, MasterKeyRef
    CORSOrigin   string
    // ... (full set from auth's BaseConfig proposal)
}

type Host struct { /* holds the wired stores + keyprovider + lifecycle */ }

func New(ctx context.Context, cfg BaseConfig) (*Host, error)
func (h *Host) Close() error

func EnsureStorage(ctx context.Context, cfg BaseConfig) error

// First-run hook (see §7)
type FirstRunHook func(ctx context.Context, h *Host) error
```

- `host.LoadBaseConfig()` (or equivalent) handles the `PROTOSOURCE_*` + `PORT` mapping + base64 master key + Cosmos/Dynamo env aliasing.
- `host.NewStore(...)` and keyprovider construction live here.
- The `Bundle` / `CloseFn` lifecycle pattern from `auth/app/backend.go` is generalized.
- `dynamodbstore.EnsureTables` and Cosmos equivalents are invoked from `EnsureStorage`.

**Explicit scope limits (non-negotiable):**
- The harness owns **storage + keyprovider wiring + basic lifecycle + first-run hook dispatch**.
- It does **NOT** own HTTP handler composition, middleware stacks, router registration beyond a minimal `MountRouter` helper, or transport choice (Lambda vs Container App vs anything else).
- Aggregate-specific bootstrap logic (Issuer/Role/User creation, etc.) stays in the consuming service via the `FirstRunHook` callback.

**Chosen package name:** `host` (short, appears in every service's `main.go` and `cmd/*mgr`, communicates "the thing that hosts your repositories and their infrastructure adapters").

### 3. Lift operational admin CLI skeleton (as a library)

- New package: `protosource/admin` (or `protosource/host/admin` — final location decided at implementation time).
- Library model: services link it into their own `cmd/foomgr` binary.
- Lifts: subcommand dispatch pattern, `ensure-tables` (now powered by the harness), `loadConfigForMgr` parity with the running service, `--force` / seed-secret guard.
- Does **not** lift auth-specific commands (`bootstrap`, `recover-admin`, `diagnose-user`).

The generated per-aggregate `*mgr` CLIs continue to exist; the admin library provides the cross-aggregate orchestrator + universal ops.

### 4. Lift small HTTP composition helpers

- New (or expanded) package: `protosource/httputil`
- `MountRouter(registrars []protosource.RouteRegistrar, opts HTTPOptions) *Router`
- `cookiescope` helper (the ~20-line eTLD+1 logic that uses `publicsuffix`).
- Keeps the framework opinion on `AllowCredentials: true` when CORS is enabled (driven by shadow-cookie auth requirements).

This centralizes the `publicsuffix` dependency and prevents subtle divergence on localhost/IP/single-label hosts across services.

### 5. Lift AWS deployment module parity

- New module: `deploy/modules/lambda-eventstore/`
- Provides the same shape as the existing `cosmos-eventstore` + `container-app-service` for Azure: DynamoDB tables + Lambda + API Gateway + custom domain + Route53.
- Inputs include function image/zip, KMS key, table names, domain/cert/zone.
- Outputs: API Gateway URL + table ARNs.
- Versioning: Land at the next minor (auth and future services pin from day one).

Auth keeps its service-specific bits (`tofu/aws/admin.tf` for the SPA, IAM attachments, tfvars).

### 6. Lift first-run bootstrap hook

Add to the `host` package:

```go
type RunOptions struct {
    OnFirstRun FirstRunHook
    // ...
}

func (h *Host) Run(ctx context.Context, opts RunOptions) error
```

The hook is responsible for idempotent "if missing, create it" work. The harness owns the calling convention and `ErrAlreadyCreated` swallowing. Services supply the body.

### Explicit non-lifts (confirmed)

All items listed in the "Keep in auth (look generic, aren't)" table of `FRAMEWORK_LIFT_CANDIDATES.md` remain downstream:
- `credentials/` (argon2id)
- `signers/` (ed25519 JWT minting)
- `functions/` (wildcard policy matcher)
- `keys/` (per-day Resolver policy)
- `loginpage/` (the actual HTML + handler)
- `authz/{httpauthz,directauthz}` (implementations of the already-upstream `Authorizer` interface)
- Any aggregate-specific code (Issuer, Role, User, Key, Token, etc.)

---

## Consequences

### Positive
- Second (and Nth) protosource service becomes dramatically faster to stand up on both AWS and Azure.
- Eliminates the largest source of copy-paste and subtle divergence (especially the storage wiring and AWS IaC).
- `keyproviders/` becomes a reusable primitive for any future service that needs envelope encryption (webhook signing, row-level secrets, PII at rest).
- The admin CLI pattern + `ensure-tables` becomes standard, reducing operational toil.
- Clear precedent and guardrails for future "is this framework or product?" questions.

### Risks / Trade-offs
- `host` package becomes a high-visibility, frequently-imported piece of API surface. Naming and scope discipline are critical (hence the explicit non-goals above).
- Refactoring cost in `protosource-auth` is real (large mechanical change to `app/`), but the end state is much thinner.
- Introducing `httputil` and `admin` adds a few more top-level packages. Acceptable given the value.
- Terraform module authoring + testing for `lambda-eventstore` is the largest single piece of new code.

### Migration / Adoption
- No existing external consumers of protosource are affected (the lifts are additive).
- `protosource-auth` will adopt incrementally:
  1. Depend on new `keyproviders` (import path change only).
  2. Replace most of `app/backend*.go` + config wiring with `host`.
  3. Thin `app/` wrapper allowed for one release if desired.
  4. Later cut over fully and delete the duplicated code.
- Generated code and Wire providers are unaffected (they continue to consume `protosource.Store`).

### Open questions closed by this ADR
- **Harness name**: `host` (see rationale above).
- **Keyproviders location**: top-level `keyproviders/` (infrastructure adapter peer to `stores/`).
- **Admin CLI model**: library first (services embed into their own `cmd/*mgr`).
- **Cookie helper location**: `protosource/httputil`.
- **Terraform versioning**: next minor; consumers pin immediately.
- **Harness scope**: storage + keyproviders + lifecycle + first-run hook dispatch. No HTTP service shape ownership.

---

## Implementation Notes & Sequencing (for TODO tracking)

Adopt the ordering from the source document, with emphasis on the two highest-leverage items:

1. `keyproviders/` (smallest blast radius, validates layout).
2. `host` package + `EnsureStorage` + `New` + config loading (largest refactor in the first consumer; unblocks almost everything else).
3. `admin` library + `ensure-tables`.
4. `httputil` (MountRouter + cookiescope) — can batch with small items.
5. `lambda-eventstore` terraform module (highest practical impact on new service bootstrap on AWS).
6. First-run hook on `host.Run` (last, once the Host API is stable).

## Initial Host Package Sketch (for implementers)

This is a **starting point only** — names, exact boundaries, and error handling will be refined during implementation. The goal is to give the first PR author a concrete shape rather than a blank page.

```go
// Package host provides the cross-cloud service harness: storage backend
// selection (memory / DynamoDB / Cosmos DB), key provider wiring for
// envelope encryption, lifecycle management, and a first-run hook for
// idempotent bootstrap work.
package host

import (
	"context"
	"io"

	"github.com/funinthecloud/protosource"
)

// BaseConfig is the framework-owned subset of configuration that every
// protosource service needs for storage + keyprovider + basic HTTP plumbing.
// Service-specific Config types should embed this (or contain it) and add
// only domain fields (IssuerIss, TokenTTL, bootstrap credentials, etc.).
type BaseConfig struct {
	ListenAddr string

	Backend      string // "memory" | "dynamodb" | "cosmosdb"
	EventsTable  string
	AggregatesTable string

	// AWS
	AWSEndpoint string
	AWSRegion   string

	// Cosmos DB
	CosmosEndpoint             string
	CosmosKey                  string
	CosmosUseDefaultCredential bool
	CosmosDatabase             string
	CosmosInsecureTLS          bool

	// Envelope encryption (for any service that needs to protect secrets at rest)
	KeyProvider  string // "local" | "awskms" | "azurekeyvault"
	MasterKey    []byte // for local provider (base64-decoded by Load)
	MasterKeyRef string // ARN / Key Vault URL / etc. for cloud providers

	CORSOrigin string
}

// LoadBaseConfig populates a BaseConfig from the standard PROTOSOURCE_*
// (and PORT) environment variables, applying Cosmos-vs-Dynamo aliasing
// and base64 decoding for the local master key. Services call this (or
// their own wrapper) from main and from their mgr CLI.
func LoadBaseConfig() (BaseConfig, error) { ... }

// EnsureStorage creates tables/containers if they do not exist (idempotent).
// It delegates to the concrete store's Ensure* helpers.
func EnsureStorage(ctx context.Context, cfg BaseConfig) error { ... }

// Host holds the wired infrastructure adapters and provides lifecycle.
type Host struct {
	// Store is the event Store passed to protosource.New.
	Store protosource.Store

	// OpaqueStore is the materialized aggregate store (if the backend supports it).
	OpaqueStore opaquedata.Store // or interface extracted if needed

	// KeyProvider is the envelope-encryption provider selected by cfg.KeyProvider.
	// Services that need one (e.g. for signing keys) obtain it here.
	KeyProvider keyproviders.KeyProvider

	closer io.Closer // for stores that need explicit shutdown
}

// New constructs a Host from BaseConfig, performing the backend switch
// and constructing the chosen KeyProvider. It does not start serving.
func New(ctx context.Context, cfg BaseConfig) (*Host, error) { ... }

// Close releases any resources held by the Host (DynamoDB clients, etc.).
func (h *Host) Close() error { ... }

// FirstRunHook is the extension point for service-specific "create defaults
// if missing" logic. The hook is called once during startup (or on explicit
// admin trigger). It must be idempotent; the harness swallows
// ErrAlreadyCreated from generated command types.
type FirstRunHook func(ctx context.Context, h *Host) error

// RunOptions controls lifecycle behavior.
type RunOptions struct {
	OnFirstRun FirstRunHook
	// Future: readiness hooks, background worker registration, etc.
	// Deliberately small to avoid turning Host into a full service framework.
}

// Run is the optional convenience entrypoint that wires everything and
// blocks until shutdown. Many services will still write their own main
// loop (especially Lambda handlers), but this is useful for Container Apps
// / plain binaries.
func (h *Host) Run(ctx context.Context, opts RunOptions) error { ... }
```

**Design constraints called out during planning:**
- `BaseConfig` must be embeddable; services add their own fields on top.
- No HTTP `http.Handler` or router construction in `Host` (use the small `httputil.MountRouter` helper instead).
- KeyProvider construction lives alongside Store construction because both are "cross-cloud infrastructure adapters" that almost every service will need.
- The sketch above is intentionally incomplete on exact interfaces for `opaquedata.Store` visibility — that can be adjusted once the first integration happens.

---

## References

- Source analysis: [FRAMEWORK_LIFT_CANDIDATES.md](../../FRAMEWORK_LIFT_CANDIDATES.md)
- Current Azure deployment modules: `deploy/modules/{cosmos-eventstore,container-app-service}`
- Runtime core: `protosource.go` (Store, AggregateStore, SnapshotTailStore interfaces)
- Existing stores: `stores/{memorystore,dynamodbstore,cosmosdbstore,...}`
- Authz interface precedent: `authz/authorizer.go` + `authz/allowall/`

**Next action:** Create detailed tracking items in [TODO.md](../../TODO.md) under a new "Framework Infrastructure Lifts" section.
