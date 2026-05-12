// Package main is the Azure Container Apps runtime smoke binary for the
// protosource framework: it wires the example domain handlers
// (test/order/sample) against the Cosmos DB store and serves them via plain
// net/http using adapters/httpstandard. This is the Cosmos counterpart of
// cmd/testdynamo, which serves the same handlers via AWS Lambda.
//
// The handler layer is provider-agnostic — only main() and the wire bindings
// differ between clouds. That is the cross-cloud parity guarantee the
// framework promises.
//
// Run against the emulator:
//
//	export COSMOS_ENDPOINT=https://localhost:8081
//	export COSMOS_KEY='C2y6yDjf5/R+ob0N8A7Cgv30VRDJIWEHLM+4QDU5DE2nQ9nDuVTqobD4b8mGGyPMbIZnqyMsEcaGQy67XIw/Jw=='
//	export COSMOS_INSECURE=1
//	testcosmos-setup create
//	testcosmos        # listens on :8080
package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"

	"github.com/funinthecloud/protosource/adapters/httpstandard"
	"github.com/funinthecloud/protosource/azure/cosmosclient"
	"github.com/funinthecloud/protosource/stores/cosmosdbstore"
)

func main() {
	client, err := newClient()
	if err != nil {
		fatal("cosmos client: %v", err)
	}

	dbID := envOrDefault("COSMOS_DATABASE", "protosource")
	eventsID := envOrDefault("EVENTS_CONTAINER", cosmosdbstore.DefaultEventsContainer)
	aggregatesID := envOrDefault("AGGREGATES_CONTAINER", cosmosdbstore.DefaultAggregatesContainer)

	db, err := client.NewDatabase(dbID)
	if err != nil {
		fatal("database handle: %v", err)
	}
	eventsContainer, err := db.NewContainer(eventsID)
	if err != nil {
		fatal("events container handle: %v", err)
	}
	aggregatesContainer, err := db.NewContainer(aggregatesID)
	if err != nil {
		fatal("aggregates container handle: %v", err)
	}

	events := cosmosdbstore.EventsContainerClient(cosmosclient.Wrap(eventsContainer))
	aggregates := cosmosdbstore.AggregatesContainerClient(cosmosclient.Wrap(aggregatesContainer))

	router, err := InitializeRouter(events, aggregates)
	if err != nil {
		fatal("wire: %v", err)
	}

	port := envOrDefault("PORT", "8080")
	addr := ":" + port
	fmt.Fprintf(os.Stdout, "listening on %s\n", addr)
	if err := http.ListenAndServe(addr, httpstandard.WrapRouter(router, httpstandard.HeaderExtractor("X-Actor"))); err != nil {
		fatal("http.ListenAndServe: %v", err)
	}
}

func newClient() (*azcosmos.Client, error) {
	endpoint := os.Getenv("COSMOS_ENDPOINT")
	if endpoint == "" {
		return nil, errors.New("COSMOS_ENDPOINT must be set")
	}

	clientOpts := &azcosmos.ClientOptions{}
	if os.Getenv("COSMOS_INSECURE") == "1" {
		clientOpts.ClientOptions = azcore.ClientOptions{Transport: insecureTransport()}
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

