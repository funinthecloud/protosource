# Framework Lift Candidates

> **Historical input (2026-05-21).** This document contains the raw analysis
> and "would the second service re-invent this?" filtering performed while
> building the first substantial consumer (`protosource-auth`).
>
> **Decided plan:** See [docs/adr/0001-framework-infrastructure-lifts.md](docs/adr/0001-framework-infrastructure-lifts.md)
> for the accepted decisions, scope boundaries, package names, and closed
> questions. Tracking lives in [TODO.md](TODO.md) under "Framework Infrastructure Lifts".

Input for a planning session on **protosource**: which pieces currently in
**protosource-auth** are actually generic infrastructure that belong upstream,
and which only look generic.

The guiding question for every item: *would the second protosource-based
service re-invent this byte-for-byte?* If yes → lift. If it would re-invent
something with the same shape but different content → lift a contract, keep
the implementation. If it would build something genuinely different →
keep it here.

---

## Already upstream (boundary is correct)

These show that the existing line is working. Listed so the planning session
doesn't accidentally re-lift them.

- `protosource/authz/authorizer.go` — `Authorizer` interface,
  `ErrUnauthenticated`, `ErrForbidden`, `WithUserID`, `WithJWT` context
  helpers, `allowall` default binding. Only the **implementations**
  (`authz/httpauthz`, `authz/directauthz`) live downstream, which is right.
- `protosource/stores/{memorystore,dynamodbstore,cosmosstore}` — backend
  primitives. The dispatch *over* them is what's missing upstream (see §2).
- `protosource/deploy/modules/{container-app-service,cosmos-eventstore}`
  — Azure terraform building blocks. The AWS counterpart is missing (see §6).
- `protosource.Router`, `protosource.CORSConfig`, `protosource.RouteRegistrar`
  — HTTP plumbing. The *composition helper* on top is what's missing (see §4).

---

## 1. Lift — `keyproviders/`

**Source:** `protosource-auth/keyproviders/{keyprovider.go,local/,awskms/,azurekeyvault/}`
**Target:** `protosource/crypto/keyproviders/`

The `KeyProvider` interface is pure envelope-encryption plumbing with zero
auth-domain knowledge:

```go
type KeyProvider interface {
    Name() string
    Encrypt(ctx, masterKeyRef, plaintext) (wrapped []byte, err error)
    Decrypt(ctx, masterKeyRef, wrapped)   (plaintext []byte, err error)
}
```

`masterKeyRef` is an opaque string (KMS ARN, Key Vault URL, file path);
`Name()` is persisted alongside wrapped blobs so a future loader knows
which provider to route through. The three implementations (`local`
XChaCha20-Poly1305, `awskms` direct-encrypt, `azurekeyvault` RSA-OAEP-256)
are all "translate Encrypt/Decrypt to this cloud's KMS primitives" and
nothing else.

Any second protosource service that signs webhooks, encrypts row-level
secrets, or wraps PII at rest will rebuild this exact contract.

**Open question for planning:** does the package belong under `crypto/` or
should it sit next to `stores/` as a peer ("infrastructure adapters")?

**Verdict:** Lift the interface + all three implementations as-is. Auth
keeps only the *policy* of how it's used (per-day signing keys via
`keys.Resolver`).

---

## 2. Lift — service harness for storage wiring

**Source:** `protosource-auth/app/{config.go,backend.go,backend_dynamodb.go,backend_cosmosdb.go,keyprovider.go}`
**Target:** new package `protosource/serviceharness` (name TBD)

The shape of `app.NewBundle`:

```go
switch cfg.Backend {
case BackendMemory:   return newMemoryBundle()
case BackendDynamoDB: return newDynamoDBBundle(ctx, cfg)
case BackendCosmosDB: return newCosmosDBBundle(ctx, cfg)
}
```

…is 90% generic boilerplate. Every protosource service that wants to be
deployable to AWS or Azure (or local dev) will reproduce it. What's
service-specific is the *set of aggregates* the bundle binds — that's a
generic type parameter or a callback over a `Repos` slice.

**Concrete lift:**

- `harness.BaseConfig` carrying: `ListenAddr`, `Backend`, `EventsTable`,
  `AggregatesTable`, `AWSEndpoint`, `AWSRegion`, `CosmosEndpoint`,
  `CosmosKey`, `CosmosUseDefaultCredential`, `CosmosDatabase`,
  `CosmosInsecureTLS`, `CORSOrigin`, `KeyProvider`, `MasterKey`,
  `MasterKeyRef`. Embed in service-specific `Config`.
- `harness.LoadBaseConfig()` that handles the `PROTOSOURCE_*` envs +
  `PORT` mapping + base64 master key + the
  `EnvCosmosContainer`-wins-over-`EnvEventsTable` aliasing already in
  `config.go:249`.
- `harness.NewStore(ctx, BaseConfig) (EventStore, OpaqueStore, io.Closer, error)`
  doing the backend switch.
- `harness.Bundle.CloseFn` lifecycle pattern — `app/backend.go:50-62`.
- The `dynamodbstore.EnsureTables` + `EnsureCosmosStorage` invocations
  belong in `harness.EnsureStorage(ctx, BaseConfig) error`.

**Do NOT lift:** `app.Run` itself (composes issuers + signers +
bootstrap — auth-specific), `app/router.go` (wires the auth-specific
service object set into a router).

**Risk to discuss in planning:** scope. The temptation will be to lift
handler composition next. Stop at storage + lifecycle + config envelope.
If the harness starts owning "what an HTTP service looks like", it will
fight gRPC/NATS later.

---

## 3. Lift — operational CLI skeleton

**Source:** `protosource-auth/cmd/protosource-authmgr/main.go`
**Target:** new package `protosource/cmd/protosource-admin` or library
`protosource/admincli`

The *shape* — "an operational CLI that talks to repos directly, bypassing
HTTP, so it works when the service is down or before it's ever run" — is
universal. Every protosource service needs it.

**Lift:**

- The subcommand dispatch + `cliFlags` parser at `main.go:271-310`.
- `ensure-tables` subcommand verbatim — backend-agnostic, just needs
  `harness.EnsureStorage`.
- `loadConfigForMgr` pattern: reuse the running binary's config loader
  so envs stay in sync.
- The `--force` / `EnvSeedSecret` guard pattern for destructive ops.

**Keep in auth:**

- `bootstrap` and `recover-admin` (aggregate-specific: they invoke
  `app.Bootstrap` which knows about Issuer + Role + User aggregates).
- `diagnose-user` (auth-domain diagnostic).

**Open question for planning:** do we want the admin CLI to be a library
(`admincli.Register("ensure-tables", ...)`) that services link into
their own `cmd/foomgr`, or a standalone binary that grows plugins? The
library path matches Go culture; the binary path matches `kubectl`-style
ecosystems. The library is easier to retrofit.

---

## 4. Lift — HTTP composition helper

**Source:** `protosource-auth/app/router.go:38-80`
**Target:** `protosource` root or `protosource/httpharness`

Every service will write the lines from `app/router.go:70-78`:

```go
r := protosource.NewRouter(registrars...)
if origins := splitOrigins(cfg.CORSOrigin); len(origins) > 0 {
    r.SetCORS(protosource.CORSConfig{
        AllowOrigins:     origins,
        AllowMethods:     "GET,POST,OPTIONS",
        AllowHeaders:     "Content-Type,Accept",
        AllowCredentials: true,
    })
}
```

**Lift** a small `protosource.MountRouter(registrars []RouteRegistrar, HTTPOptions{CORSOrigin string, …}) *Router`
helper, plus the `splitOrigins` utility. Saves ~10 lines per service and
keeps the defaults consistent (the `AllowCredentials: true` choice is a
*framework* opinion driven by shadow-cookie auth — should not be
re-decided per service).

---

## 5. Lift narrowly — eTLD+1 cookie scoping helper

**Source:** `protosource-auth/loginpage/` (the publicsuffix usage)
**Target:** new package `protosource/web/cookiescope`

The login page itself **stays in auth** — login HTML, shadow-cookie
semantics, CSRF Origin/Referer validation are all auth-protocol
decisions. But the ~20-line "given a Host header, return the eTLD+1
cookie domain" helper is reusable for any service setting cookies
(CSRF tokens, session cookies, feature-flag cohorts) and isolates the
`publicsuffix` dependency to one place.

Lifting this preempts each future service vendoring `publicsuffix`
independently and reaching subtly different answers about whether
`localhost`, IP literals, or single-label hosts qualify.

---

## 6. Split — terraform modules

**Source:** `protosource-auth/tofu/aws/{lambda.tf,apigw.tf,main.tf,admin.tf,…}`
**Target:** new `protosource/deploy/modules/lambda-eventstore`

There's already a `cosmos-eventstore` module upstream that `tofu/azure/`
consumes — that boundary is correct. The AWS side is hand-rolled per
service today (DynamoDB tables + Lambda + API Gateway + custom domain +
Route53). That's exactly the shape `cosmos-eventstore` covers for
Azure.

**Lift** a `protosource/deploy/modules/lambda-eventstore` module: input
variables for `function_name`, `image_uri` or `zip_path`, `kms_key_arn`,
`events_table_name`, `aggregates_table_name`, `domain_name`,
`certificate_arn`, `hosted_zone_id`. Outputs the API Gateway URL +
table ARNs.

**Keep** in auth: `tofu/aws/admin.tf` (S3 + CloudFront for the admin
SPA — service-specific), the `tfvars`, and any auth-specific IAM
attachments.

This is the single biggest reason a new protosource service today is
*not* a copy-paste-able starting point on AWS.

---

## 7. Lift — first-run bootstrap hook

**Source:** the *pattern* in `app/bootstrap.go` + `app.RegisterDefaultIssuer`
**Target:** harness lifecycle API

Every service has *some* "if missing, create it" first-run dance. The
auth service does it for Issuer + Role + User; another service will do
it for tenants, default workspaces, default plans, etc.

**Lift** a hook on the harness:

```go
type FirstRunHook func(ctx context.Context, bundle Bundle) error
harness.Run(ctx, cfg, RunOptions{OnFirstRun: ...})
```

The harness doesn't know *what* to bootstrap — it just owns the calling
convention and the idempotence story (ignoring `ErrAlreadyCreated` from
generated commands). Services plug in their own logic.

**Do NOT lift** the bootstrap implementation itself. It's
aggregate-specific.

---

## Keep in auth (look generic, aren't)

Listed so the planning session doesn't get distracted by them.

| Component | Why it stays |
|---|---|
| `credentials/` (argon2id) | Password hashing is authn-only. No other protosource service hashes user passwords. |
| `signers/` (Signer + ed25519) | JWT minting is authn-only. Other services *consume* JWTs via the authz layer; they don't sign. |
| `functions/` (wildcard matcher) | This is the auth product's policy language. Lifting would prejudice future authorizers (ABAC, ReBAC, OPA, Cedar). |
| `keys/` (per-day Resolver) | Auth-specific policy. The generic primitive is `keyproviders` (which is being lifted). |
| `service/MapDirectory`, `UserDirectory` | Pure auth concept. |
| `loginpage/` HTML + handler | Login UX is the product. Only the cookie-scope helper (§5) is generic. |
| `authz/{httpauthz,directauthz}` | These are *implementations* of an interface that's already upstream. They belong with the consumer because the framework can't pick a wire format for everyone. |
| `app.Bootstrap`, `app.RegisterDefaultIssuer` | Aggregate-specific. The harness lifts the *hook*, not the body. |

---

## Suggested ordering for the lift

1. **`keyproviders/`** — smallest blast radius, no consumer change
   beyond import path. Validates the upstream package layout.
2. **`harness.BaseConfig` + `harness.NewStore` + `harness.EnsureStorage`**
   — large refactor in auth, but mechanical. Collapses ~150 lines of
   `app/backend_*.go` into thin adapters.
3. **`admincli` library + `ensure-tables`** — unblocks the next
   service's mgr CLI. Cheap once §2 lands.
4. **`MountRouter` + `cookiescope`** — small quality-of-life lifts.
   Can be batched.
5. **`lambda-eventstore` terraform module** — unblocks AWS deployment
   of any future service. Highest leverage of the deploy lifts, but
   largest standalone module to write/test.
6. **First-run hook** — last, because the harness API needs to be
   stable before adding lifecycle hooks to it.

---

## Things the planning session should explicitly decide

- **Harness naming.** `serviceharness`, `runtime`, `host`, `app`?
  Whatever it is, it's a top-level package consumers see in every
  `main.go`, so the name matters.
- **Keyprovider package location.** `crypto/keyproviders` vs. peer of
  `stores/`. The latter signals "infrastructure adapter"; the former
  groups by concern.
- **Admin CLI: library or binary?** See §3.
- **How aggressive to be about removing `app/` from protosource-auth.**
  Some teams will want `app.Config` to remain a thin wrapper around
  `harness.BaseConfig` for one release; others will want a hard cutover.
- **Terraform module versioning.** Existing modules are pinned at
  v0.4.0. The new `lambda-eventstore` should land at the next minor and
  the auth repo should pin from day one.
- **Where does the cookie-scope helper live if we don't otherwise want
  a `web/` package upstream?** Possibly `protosource/httpharness/cookiescope`
  alongside `MountRouter`.

---

## What is explicitly NOT proposed

- No lift of HTTP middleware composition beyond CORS + router-mount.
  The harness must not own "what an HTTP service looks like."
- No lift of any aggregate-specific code (Issuer, Role, User, Key,
  Token). Those are the auth product.
- No lift of `cmd/protosource-auth-lambda` wire DI — that's an
  example of how to use the harness, not the harness itself. Once
  the harness exists, the lambda main shrinks to ~30 lines and stays
  in auth.
