# CloudFormation/SAM → OpenTofu Migration Plan

Status: draft. Paused before execution.

## Decisions

1. **OpenTofu** (not Terraform). MPL license, Linux Foundation stewardship — safer long-term given HashiCorp's BSL move. Tooling parity is fine today.
2. **Clean-slate cutover, not import.** Tear down the existing CFN stack (`cloudformation.yaml`) and SAM-deployed Lambdas (`cmd/testdynamo/template.yaml`, and the sister `protosource-auth` SAM stack). Re-create everything fresh in Tofu. No `terraform import` dance.

## Current AWS provisioning footprint

1. `stores/dynamodbstore/ddl/cloudformation.yaml` — events + aggregates tables. **Stale**: missing `sk` and the 20 GSIs documented in CLAUDE.md.
2. `cmd/testdynamo/template.yaml` — SAM: Lambda (`provided.al2023`/arm64) + API Gateway + custom domain + Route53.
3. `stores/dynamodbstore/tables.go` `EnsureTables` — runtime DDL via Go SDK. **Keep** — library affordance for consumers; Tofu only manages this repo's own deploys.
4. Sister repo `funinthecloud/protosource-auth` — its own SAM stack (auth service Lambda). Migrate in lockstep.

## Strategy

OpenTofu, one root module per deployable, shared modules in `deploy/modules/`. Tofu is canonical for the framework's *own* deployments; not prescribed for downstream users.

## Layout

```
deploy/
  bootstrap/              # S3 state bucket + DynamoDB lock table (one-shot)
  modules/
    dynamodb-tables/      # events + aggregates w/ 20 GSIs + TTL
    protosource-lambda/   # Lambda + APIGW + custom domain + Route53
  envs/
    dev/                  # root: backend.tf + wiring
    prod/
  apps/
    testdynamo/           # consumes protosource-lambda module
```

## Steps

1. **Bootstrap** — manual one-shot apply of `deploy/bootstrap/` creating S3 state bucket (encrypted, versioned) + DynamoDB lock table in `us-east-1`. Self-referential after first apply.
2. **Port tables module** — derive schema from `stores/dynamodbstore/tables.go` (authoritative), not the stale CFN. Includes `pk`/`sk` and all 20 GSIs with TTL on `t`.
3. **Port lambda module** — drop SAM `BuildMethod: go1.x`. Build `bootstrap` via `GOOS=linux GOARCH=arm64 go build`, zip, feed `source_code_hash`. Use `terraform-aws-modules/lambda` or a `null_resource` build step.
4. **Tear down old stacks** — once Tofu modules are reviewed and `tofu plan` is clean:
   - `aws cloudformation delete-stack` for the testdynamo SAM stack.
   - `aws cloudformation delete-stack` for the events/aggregates CFN stack.
   - Same for the `protosource-auth` SAM stack.
   - Route53 records and ACM cert: delete only if recreating; otherwise let Tofu adopt by re-declaration (cert/zone aren't stack-owned in practice — verify before delete).
5. **Apply Tofu** — `dev` first, smoke test the testdynamo Lambda end-to-end, then `prod`.
6. **CI** — add `tofu fmt -check && tofu validate` job; plan-on-PR, apply-on-tag, mirroring `.github/workflows/release.yml`.
7. **Delete dead files** — `stores/dynamodbstore/ddl/cloudformation.yaml` and `cmd/testdynamo/template.yaml`. Keep one release cycle as rollback, then drop.
8. **Docs** — short `deploy/README.md`. Update `TODO.md` to track the `protosource-auth` Tofu port.

## Risks / things to watch

- **Data loss on table delete.** The events/aggregates tables hold real data. Before deleting the CFN stack, snapshot via point-in-time recovery or `aws dynamodb create-backup`, then restore into the Tofu-created tables. Or: rename the new tables and dual-write/cutover. Confirm policy before step 4.
- **Custom domain downtime.** Deleting the SAM stack drops the API Gateway custom domain mapping; DNS will fail until Tofu re-creates it. Plan a maintenance window or sequence: create new APIGW + domain in Tofu first (under a temp domain), flip Route53, then delete old.
- **ACM cert.** Don't delete; re-reference by ARN in Tofu.
- **Schema drift fix.** The new tables module will be the first time `pk`/`sk` + 20 GSIs actually exist in deployed infra. Verify `EnsureTables` and Tofu produce identical schemas (write a comparison test or run `EnsureTables` against a Tofu-created table and assert no diffs).

## Open questions

- Backup/restore plan for live event data before stack teardown?
- Single Tofu repo for both `protosource` and `protosource-auth`, or one per repo?
- Who owns `dev` vs `prod` AWS accounts — same account, separate accounts?
