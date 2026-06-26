# 0002. Multi-cookie response support via `[]*http.Cookie`

- Status: Accepted
- Date: 2026-06-26

## Context

`protosource.Response` modeled headers solely as `Headers map[string]string`. A
Go map holds one value per key, so a handler could emit at most one `Set-Cookie`
header per response, and both shipped adapters then rendered that map with
`w.Header().Set` / the API Gateway single-value `Headers` field — so even a
multi-value-capable transport never received more than one cookie.

This blocked the common "set one cookie **and** clear another in the same
response" pattern at the framework level: login/logout, session rotation, and
the OAuth2/OIDC PKCE `/oauth/callback` flow in protosource-auth (set the
`shadow` session cookie while clearing the consumed `shadow_oauth_state` cookie).
See GH#103.

Two sub-decisions were weighed: (a) how to express cookies on the transport-neutral
`Response`, and (b) how the awslambda adapter renders them, since both shipped
adapters target `events.APIGatewayProxyResponse` (REST / proxy v1).

## Decision

Add a `Cookies []*http.Cookie` field to `Response`. Each entry renders as its own
`Set-Cookie` header; the existing single-value `Headers` semantics are untouched,
so responses that leave `Cookies` nil behave exactly as before.

- **httpstandard:** after applying `Headers`, loop `http.SetCookie(w, c)` for each
  cookie (uses `Header().Add`, emitting multiple `Set-Cookie` lines).
- **awslambda:** render cookies into `MultiValueHeaders["Set-Cookie"]`, skipping
  any cookie whose `http.Cookie.String()` is empty (invalid). `MultiValueHeaders`
  stays nil when `Cookies` is empty.

## Rejected alternatives

- **Expose `MultiValueHeaders map[string][]string` on `Response`** — rejected
  because it leaks an AWS-/transport-specific shape into the cloud-agnostic core,
  pushes cookie encoding (attributes, expiry, escaping) onto every handler, and
  has no clean rendering for the httpstandard adapter.
- **Keep `Headers` and special-case a magic newline-joined `Set-Cookie` value** —
  rejected as a fragile encoding hack that breaks the stdlib cookie contract and
  surprises handler authors.
- **Target API Gateway HTTP API v2 (`APIGatewayV2HTTPResponse.Cookies []string`)**
  — rejected for now because both shipped adapters use the v1 proxy type
  (`APIGatewayProxyResponse`); there is no v2 handler in the repo. `MultiValueHeaders`
  is the correct mechanism for v1 REST APIs. A v2 adapter can map `Response.Cookies`
  to the native `Cookies` field if/when one is added — the `Response` shape already
  supports it.

## Consequences

- Handlers can set and clear multiple cookies in one response across both adapters;
  unblocks protosource-auth's explicit OAuth-state-cookie clearing and a proper
  federated `/oauth/logout`.
- Fully backward compatible: no change for handlers that don't set `Cookies`.
- `Response` now imports `net/http`, a stdlib-only dependency.
- A future v2 adapter must map `Cookies` to `APIGatewayV2HTTPResponse.Cookies`
  rather than `MultiValueHeaders`; the v1 path documents why.
