// Package main implements a CLI for managing the Cosmos DB database and
// containers used by the protosource event store. Mirrors the Dynamo-side
// cmd/testdynamo-setup tool but speaks the Cosmos NoSQL API.
//
// Quickstart against the local emulator (Linux/Windows host):
//
//	docker run --rm -p 8081:8081 -p 10250-10255:10250-10255 \
//	  --name cosmos-emulator \
//	  mcr.microsoft.com/cosmosdb/linux/azure-cosmos-emulator
//
//	export COSMOS_ENDPOINT=https://localhost:8081
//	export COSMOS_KEY='C2y6yDjf5/R+ob0N8A7Cgv30VRDJIWEHLM+4QDU5DE2nQ9nDuVTqobD4b8mGGyPMbIZnqyMsEcaGQy67XIw/Jw=='
//	export COSMOS_INSECURE=1   # emulator uses a self-signed cert
//	testcosmos-setup create
//
// Against a real Azure account, prefer Managed Identity (set
// COSMOS_USE_DEFAULT_CREDENTIAL=1) and omit COSMOS_KEY.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"

	"github.com/funinthecloud/protosource/stores/cosmosdbstore"
)

const usage = `Usage: testcosmos-setup <command>

Commands:
  create  Create the Cosmos database and events/aggregates containers
  delete  Delete the events/aggregates containers (database is left intact)
  status  Print database + container status

Environment variables:
  COSMOS_ENDPOINT                  Account endpoint URL (required)
  COSMOS_KEY                       Primary key for shared-key auth
  COSMOS_USE_DEFAULT_CREDENTIAL    Use DefaultAzureCredential (Managed Identity, az login, etc) when set to 1
  COSMOS_DATABASE                  Database id (default: protosource)
  EVENTS_CONTAINER                 Events container id (default: events)
  AGGREGATES_CONTAINER             Aggregates container id (default: aggregates)
  COSMOS_INSECURE                  Skip TLS verification (emulator only)`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(1)
	}

	ctx := context.Background()
	client, err := newClient()
	if err != nil {
		fatal("cosmos client: %v", err)
	}

	dbID := envOrDefault("COSMOS_DATABASE", "protosource")
	eventsID := envOrDefault("EVENTS_CONTAINER", cosmosdbstore.DefaultEventsContainer)
	aggregatesID := envOrDefault("AGGREGATES_CONTAINER", cosmosdbstore.DefaultAggregatesContainer)

	switch os.Args[1] {
	case "create":
		db, err := cosmosdbstore.EnsureDatabase(ctx, client, dbID)
		if err != nil {
			fatal("ensure database %q: %v", dbID, err)
		}
		if err := cosmosdbstore.EnsureContainers(ctx, db, eventsID, aggregatesID); err != nil {
			fatal("ensure containers: %v", err)
		}
		fmt.Printf("  database %q: ready\n", dbID)
		fmt.Printf("  container %q: ready\n", eventsID)
		fmt.Printf("  container %q: ready\n", aggregatesID)

	case "delete":
		db, err := client.NewDatabase(dbID)
		if err != nil {
			fatal("handle database: %v", err)
		}
		for _, id := range []string{eventsID, aggregatesID} {
			c, err := db.NewContainer(id)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  %s: handle error: %v\n", id, err)
				continue
			}
			if _, err := c.Delete(ctx, nil); err != nil {
				fmt.Fprintf(os.Stderr, "  %s: delete error: %v\n", id, err)
				continue
			}
			fmt.Printf("  container %q: deleted\n", id)
		}

	case "status":
		db, err := client.NewDatabase(dbID)
		if err != nil {
			fatal("handle database: %v", err)
		}
		if _, err := db.Read(ctx, nil); err != nil {
			fmt.Fprintf(os.Stderr, "  database %q: %v\n", dbID, err)
		} else {
			fmt.Printf("  database %q: present\n", dbID)
		}
		for _, id := range []string{eventsID, aggregatesID} {
			c, err := db.NewContainer(id)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  %s: handle error: %v\n", id, err)
				continue
			}
			resp, err := c.Read(ctx, nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  container %q: %v\n", id, err)
				continue
			}
			pkPaths := resp.ContainerProperties.PartitionKeyDefinition.Paths
			ttl := "off"
			if resp.ContainerProperties.DefaultTimeToLive != nil {
				ttl = fmt.Sprintf("%d", *resp.ContainerProperties.DefaultTimeToLive)
			}
			fmt.Printf("  container %q: partition_key=%v default_ttl=%s\n", id, pkPaths, ttl)
		}

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n%s\n", os.Args[1], usage)
		os.Exit(1)
	}
}

// newClient builds an *azcosmos.Client from environment configuration.
// Auth priority: explicit COSMOS_KEY, then DefaultAzureCredential when
// COSMOS_USE_DEFAULT_CREDENTIAL=1. Falls back to an error so misconfigured
// setups fail loud rather than silently using the wrong account.
func newClient() (*azcosmos.Client, error) {
	endpoint := os.Getenv("COSMOS_ENDPOINT")
	if endpoint == "" {
		return nil, errors.New("COSMOS_ENDPOINT must be set")
	}

	clientOpts := &azcosmos.ClientOptions{}
	if os.Getenv("COSMOS_INSECURE") == "1" {
		clientOpts.ClientOptions = azcore.ClientOptions{
			Transport: insecureTransport(),
		}
	}

	if key := os.Getenv("COSMOS_KEY"); key != "" {
		cred, err := azcosmos.NewKeyCredential(key)
		if err != nil {
			return nil, fmt.Errorf("key credential: %w", err)
		}
		return azcosmos.NewClientWithKey(endpoint, cred, clientOpts)
	}

	if os.Getenv("COSMOS_USE_DEFAULT_CREDENTIAL") == "1" {
		cred, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("default credential: %w", err)
		}
		return azcosmos.NewClient(endpoint, cred, clientOpts)
	}

	return nil, errors.New("no auth configured: set COSMOS_KEY or COSMOS_USE_DEFAULT_CREDENTIAL=1")
}

// insecureTransport returns a policy.Transporter that skips TLS verification.
// Required for the Cosmos emulator's self-signed cert; should never be used
// against a real Cosmos account.
func insecureTransport() policy.Transporter {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // emulator only
		},
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
