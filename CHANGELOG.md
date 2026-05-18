# Changelog

All notable changes to this project are documented here. Versions
correspond to git tags (`vX.Y.Z`) which release both the proto module
(`buf.build/funinthecloud/protosource`) and the npm package
(`@protosource/client`).

## v0.3.8

### Added
- `@protosource/client` now exports `nowMicros()` and `fromMicros(us)` —
  typed conversions between Unix-microsecond timestamps (as stamped by
  the framework's `NowMicros` on `create_at`/`modify_at`) and JS `Date`.
  Removes the "guess the unit" failure mode in downstream TS consumers.
  `fromMicros` accepts `bigint | number`. Sub-millisecond precision is
  truncated (Date's resolution); compare bigints directly when full
  precision matters.

## v0.3.7

### Fixed
- ts-plugin: import every enum referenced by any rendered method
  signature (not just GSI PK/SK), so commands with non-`State` enum
  parameters no longer emit unresolved type references.
- ts-plugin: drop `// @ts-nocheck` from the generated client header.
  Generated output now type-checks cleanly under
  `--strict --noUnusedLocals --noUnusedParameters`.
- ts-plugin: prune type imports to only those actually referenced
  (aggregate type + embedded command-field types).

## v0.3.6

### Fixed
- ts-plugin: suppress lint/typecheck on generated client files via
  `/* eslint-disable */` + `// @ts-nocheck` (the latter is removed
  again in v0.3.7).

## v0.3.5

### Fixed
- ts-plugin: escape TS reserved-word command fields (e.g. `function`)
  in parameter names while preserving the wire name in object-literal
  keys.
- ts-plugin: co-locate the generated client with `*_pb.ts` siblings by
  deriving the output path from the proto package (not the Go import
  path), so `./<stem>_pb.js` imports resolve.

## v0.3.4

### Fixed
- ts-plugin: derive `routePath` from the proto package so generated
  clients align with the server's registered routes regardless of
  downstream `module=` configuration.
