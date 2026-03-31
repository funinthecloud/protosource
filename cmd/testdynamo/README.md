# testdynamo

A SAM-deployed Lambda that serves the `testv1.Test` aggregate over API Gateway.

## Prerequisites

- AWS SAM CLI (`brew install aws-sam-cli`)
- AWS credentials configured (`aws configure` or SSO)
- Tables created via `testdynamo-setup` (see below)

## Table Setup

```bash
# Create tables (events + aggregates) with TTL, PITR, deletion protection
go run ./cmd/testdynamo-setup create

# Fix existing tables missing TTL or PITR
go run ./cmd/testdynamo-setup fix

# Check table configuration
go run ./cmd/testdynamo-setup status
```

Override table names or endpoint via environment:

```bash
EVENTS_TABLE=my-events AGGREGATES_TABLE=my-aggregates go run ./cmd/testdynamo-setup create
AWS_ENDPOINT_URL=http://localhost:8000 go run ./cmd/testdynamo-setup create  # DynamoDB Local
```

## Deploy

```bash
cd cmd/testdynamo

# First deploy (interactive — prompts for stack name, region, etc.)
sam build && sam deploy --guided

# Subsequent deploys (uses saved samconfig.toml)
sam build && sam deploy
```

## Tear Down

```bash
# Disable deletion protection first
go run ./cmd/testdynamo-setup disable-protection

# Delete tables
go run ./cmd/testdynamo-setup delete

# Delete the SAM stack
sam delete --stack-name <your-stack-name>
```
