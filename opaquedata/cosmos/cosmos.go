// Package cosmos implements opaquedata.OpaqueStore against Azure Cosmos DB
// (NoSQL / Core SQL API). It mirrors the DynamoDB adapter in opaquedata/dynamo:
// a single aggregates container holds materialized aggregate documents keyed
// by (pk, sk), and the 20 GSI slot pairs (gsi1pk/gsi1sk … gsi20pk/gsi20sk)
// support secondary access patterns via cross-partition queries against the
// same container — Cosmos does not need a separate index object per GSI.
//
// Document shape (JSON):
//
//	{
//	  "id":      "<sk>",   // Cosmos doc id, unique within the partition
//	  "pk":      "<pk>",   // logical partition key value
//	  "sk":      "<sk>",   // sort key (also stored separately for queries)
//	  "body":    "<b64>",  // opaque proto bytes, base64-encoded
//	  "t":       0,         // absolute epoch seconds; 0 means no expiry
//	  "ttl":     N,         // Cosmos auto-purge seconds; written only when t > 0
//	  "version": 0,
//	  "gsi1pk":  "...",     // omitted when empty
//	  "gsi1sk":  "...",
//	  ...
//	}
package cosmos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	stdsort "sort"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/funinthecloud/protosource/azure/cosmosclient"
	"github.com/funinthecloud/protosource/opaquedata"
	opaquedatav1 "github.com/funinthecloud/protosource/opaquedata/v1"
)

// Store implements opaquedata.OpaqueStore backed by a single Cosmos container.
type Store struct {
	client cosmosclient.ContainerClient
}

// New creates a new Cosmos-backed OpaqueStore. The container is implicit in
// the supplied client — callers wire one Store per Cosmos container.
func New(client cosmosclient.ContainerClient) *Store {
	return &Store{client: client}
}

func (s *Store) Put(ctx context.Context, od *opaquedatav1.OpaqueData) error {
	item, err := marshalItem(od)
	if err != nil {
		return fmt.Errorf("cosmos.Store.Put: %w", err)
	}
	pk := azcosmos.NewPartitionKeyString(od.GetPk())
	if _, err := s.client.UpsertItem(ctx, pk, item, nil); err != nil {
		return fmt.Errorf("cosmos.Store.Put: %w", err)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, pk, sk string) (*opaquedatav1.OpaqueData, error) {
	resp, err := s.client.ReadItem(ctx, azcosmos.NewPartitionKeyString(pk), sk, nil)
	if err != nil {
		if isNotFound(err) {
			return nil, opaquedata.ErrNotFound
		}
		return nil, fmt.Errorf("cosmos.Store.Get: %w", err)
	}
	od, err := unmarshalItem(resp.Value)
	if err != nil {
		return nil, fmt.Errorf("cosmos.Store.Get: unmarshal: %w", err)
	}
	return od, nil
}

func (s *Store) Delete(ctx context.Context, pk, sk string) error {
	_, err := s.client.DeleteItem(ctx, azcosmos.NewPartitionKeyString(pk), sk, nil)
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("cosmos.Store.Delete: %w", err)
	}
	return nil
}

func (s *Store) Query(ctx context.Context, pkAttr, pkValue, skAttr string, sort *opaquedata.SortCondition, opts ...opaquedata.QueryOption) ([]*opaquedatav1.OpaqueData, error) {
	qo := opaquedata.ApplyQueryOptions(opts)
	if qo.GSIIndex < 0 || qo.GSIIndex > 20 {
		return nil, fmt.Errorf("opaquedata: GSI index %d out of range [0,20]", qo.GSIIndex)
	}
	// Cosmos SQL cannot parameterize identifiers, so pkAttr/skAttr are
	// interpolated into the query string. Restrict to the opaquedata schema
	// (pk/sk plus gsi{1..20}{pk,sk}) so a caller cannot smuggle arbitrary
	// SQL through the attribute names.
	if !isValidOpaqueAttr(pkAttr) {
		return nil, fmt.Errorf("opaquedata: invalid pk attribute %q", pkAttr)
	}
	if !isValidOpaqueAttr(skAttr) {
		return nil, fmt.Errorf("opaquedata: invalid sk attribute %q", skAttr)
	}

	params := []azcosmos.QueryParameter{
		{Name: "@pk", Value: pkValue},
		{Name: "@now", Value: time.Now().Unix()},
	}

	var sb strings.Builder
	sb.WriteString("SELECT * FROM c WHERE c.")
	sb.WriteString(pkAttr)
	sb.WriteString(" = @pk")

	if sort != nil {
		params = append(params, azcosmos.QueryParameter{Name: "@sk", Value: sort.Value})
		switch sort.Operator {
		case opaquedata.Equal:
			fmt.Fprintf(&sb, " AND c.%s = @sk", skAttr)
		case opaquedata.Lt:
			fmt.Fprintf(&sb, " AND c.%s < @sk", skAttr)
		case opaquedata.Le:
			fmt.Fprintf(&sb, " AND c.%s <= @sk", skAttr)
		case opaquedata.Gt:
			fmt.Fprintf(&sb, " AND c.%s > @sk", skAttr)
		case opaquedata.Ge:
			fmt.Fprintf(&sb, " AND c.%s >= @sk", skAttr)
		case opaquedata.Between:
			params = append(params, azcosmos.QueryParameter{Name: "@sk2", Value: sort.Value2})
			fmt.Fprintf(&sb, " AND c.%s BETWEEN @sk AND @sk2", skAttr)
		case opaquedata.BeginsWith:
			fmt.Fprintf(&sb, " AND STARTSWITH(c.%s, @sk)", skAttr)
		default:
			return nil, fmt.Errorf("opaquedata: unknown sort operator %d", sort.Operator)
		}
	}

	// TTL filter mirrors opaquedata/dynamo: skip rows whose epoch-second TTL has passed.
	sb.WriteString(" AND (NOT IS_DEFINED(c.t) OR c.t = 0 OR c.t > @now)")

	// Deterministic ordering on the sort key for predictable client-side iteration.
	// Skip for cross-partition (GSI) queries: the Cosmos gateway rejects ORDER BY
	// on cross-partition queries with a 400 ("can not be directly served by the
	// gateway"), so we sort client-side after fetch instead.
	if qo.GSIIndex == 0 {
		fmt.Fprintf(&sb, " ORDER BY c.%s ASC", skAttr)
	}

	// GSI queries are cross-partition; main-partition queries pin to the pkValue partition.
	pk := azcosmos.NewPartitionKey()
	if qo.GSIIndex == 0 {
		pk = azcosmos.NewPartitionKeyString(pkValue)
	}

	items, err := s.client.QueryItems(ctx, sb.String(), pk, &azcosmos.QueryOptions{QueryParameters: params})
	if err != nil {
		return nil, fmt.Errorf("opaquedata: query: %w", err)
	}

	results := make([]*opaquedatav1.OpaqueData, 0, len(items))
	for _, raw := range items {
		od, err := unmarshalItem(raw)
		if err != nil {
			return nil, fmt.Errorf("opaquedata: unmarshal item: %w", err)
		}
		results = append(results, od)
	}
	if len(results) == 0 {
		return nil, nil
	}
	// Cross-partition queries skipped ORDER BY at the gateway; sort here to
	// preserve the deterministic ascending-by-sk contract callers rely on.
	if qo.GSIIndex > 0 {
		extract := skValueExtractor(skAttr)
		stdsort.SliceStable(results, func(i, j int) bool { return extract(results[i]) < extract(results[j]) })
	}
	return results, nil
}

// skValueExtractor returns a getter for the validated opaquedata sk attribute
// name so client-side sorting can read the same field the SQL ORDER BY would
// have used. skAttr is guaranteed to be one of {sk, gsi{1..20}sk} by
// isValidOpaqueAttr; any other input returns a no-op extractor.
func skValueExtractor(skAttr string) func(*opaquedatav1.OpaqueData) string {
	switch skAttr {
	case "sk":
		return (*opaquedatav1.OpaqueData).GetSk
	case "gsi1sk":
		return (*opaquedatav1.OpaqueData).GetGsi1Sk
	case "gsi2sk":
		return (*opaquedatav1.OpaqueData).GetGsi2Sk
	case "gsi3sk":
		return (*opaquedatav1.OpaqueData).GetGsi3Sk
	case "gsi4sk":
		return (*opaquedatav1.OpaqueData).GetGsi4Sk
	case "gsi5sk":
		return (*opaquedatav1.OpaqueData).GetGsi5Sk
	case "gsi6sk":
		return (*opaquedatav1.OpaqueData).GetGsi6Sk
	case "gsi7sk":
		return (*opaquedatav1.OpaqueData).GetGsi7Sk
	case "gsi8sk":
		return (*opaquedatav1.OpaqueData).GetGsi8Sk
	case "gsi9sk":
		return (*opaquedatav1.OpaqueData).GetGsi9Sk
	case "gsi10sk":
		return (*opaquedatav1.OpaqueData).GetGsi10Sk
	case "gsi11sk":
		return (*opaquedatav1.OpaqueData).GetGsi11Sk
	case "gsi12sk":
		return (*opaquedatav1.OpaqueData).GetGsi12Sk
	case "gsi13sk":
		return (*opaquedatav1.OpaqueData).GetGsi13Sk
	case "gsi14sk":
		return (*opaquedatav1.OpaqueData).GetGsi14Sk
	case "gsi15sk":
		return (*opaquedatav1.OpaqueData).GetGsi15Sk
	case "gsi16sk":
		return (*opaquedatav1.OpaqueData).GetGsi16Sk
	case "gsi17sk":
		return (*opaquedatav1.OpaqueData).GetGsi17Sk
	case "gsi18sk":
		return (*opaquedatav1.OpaqueData).GetGsi18Sk
	case "gsi19sk":
		return (*opaquedatav1.OpaqueData).GetGsi19Sk
	case "gsi20sk":
		return (*opaquedatav1.OpaqueData).GetGsi20Sk
	}
	return func(*opaquedatav1.OpaqueData) string { return "" }
}

// isValidOpaqueAttr reports whether name is one of the opaquedata schema
// attributes that may be interpolated into a Cosmos SQL query: pk, sk, or
// gsi{1..20}{pk,sk}. Used as a guard against SQL injection through the
// pkAttr / skAttr query parameters.
func isValidOpaqueAttr(name string) bool {
	if name == "pk" || name == "sk" {
		return true
	}
	const prefix = "gsi"
	if len(name) < len(prefix)+3 || name[:len(prefix)] != prefix {
		return false
	}
	suffix := ""
	switch {
	case strings.HasSuffix(name, "pk"):
		suffix = "pk"
	case strings.HasSuffix(name, "sk"):
		suffix = "sk"
	default:
		return false
	}
	digits := name[len(prefix) : len(name)-len(suffix)]
	if digits == "" {
		return false
	}
	n := 0
	for _, c := range digits {
		if c < '0' || c > '9' {
			return false
		}
		n = n*10 + int(c-'0')
		if n > 20 {
			return false
		}
	}
	return n >= 1 && n <= 20
}

// document is the on-wire JSON shape of an OpaqueData row in Cosmos. Fields
// with `omitempty` are stripped when empty so unused GSI slots cost zero bytes.
type document struct {
	ID      string `json:"id"`
	Pk      string `json:"pk"`
	Sk      string `json:"sk"`
	Body    []byte `json:"body,omitempty"` // encoding/json base64-encodes []byte
	T       int64  `json:"t,omitempty"`
	TTL     int64  `json:"ttl,omitempty"` // Cosmos auto-purge; relative seconds
	Version int64  `json:"version,omitempty"`

	Gsi1Pk  string `json:"gsi1pk,omitempty"`
	Gsi1Sk  string `json:"gsi1sk,omitempty"`
	Gsi2Pk  string `json:"gsi2pk,omitempty"`
	Gsi2Sk  string `json:"gsi2sk,omitempty"`
	Gsi3Pk  string `json:"gsi3pk,omitempty"`
	Gsi3Sk  string `json:"gsi3sk,omitempty"`
	Gsi4Pk  string `json:"gsi4pk,omitempty"`
	Gsi4Sk  string `json:"gsi4sk,omitempty"`
	Gsi5Pk  string `json:"gsi5pk,omitempty"`
	Gsi5Sk  string `json:"gsi5sk,omitempty"`
	Gsi6Pk  string `json:"gsi6pk,omitempty"`
	Gsi6Sk  string `json:"gsi6sk,omitempty"`
	Gsi7Pk  string `json:"gsi7pk,omitempty"`
	Gsi7Sk  string `json:"gsi7sk,omitempty"`
	Gsi8Pk  string `json:"gsi8pk,omitempty"`
	Gsi8Sk  string `json:"gsi8sk,omitempty"`
	Gsi9Pk  string `json:"gsi9pk,omitempty"`
	Gsi9Sk  string `json:"gsi9sk,omitempty"`
	Gsi10Pk string `json:"gsi10pk,omitempty"`
	Gsi10Sk string `json:"gsi10sk,omitempty"`
	Gsi11Pk string `json:"gsi11pk,omitempty"`
	Gsi11Sk string `json:"gsi11sk,omitempty"`
	Gsi12Pk string `json:"gsi12pk,omitempty"`
	Gsi12Sk string `json:"gsi12sk,omitempty"`
	Gsi13Pk string `json:"gsi13pk,omitempty"`
	Gsi13Sk string `json:"gsi13sk,omitempty"`
	Gsi14Pk string `json:"gsi14pk,omitempty"`
	Gsi14Sk string `json:"gsi14sk,omitempty"`
	Gsi15Pk string `json:"gsi15pk,omitempty"`
	Gsi15Sk string `json:"gsi15sk,omitempty"`
	Gsi16Pk string `json:"gsi16pk,omitempty"`
	Gsi16Sk string `json:"gsi16sk,omitempty"`
	Gsi17Pk string `json:"gsi17pk,omitempty"`
	Gsi17Sk string `json:"gsi17sk,omitempty"`
	Gsi18Pk string `json:"gsi18pk,omitempty"`
	Gsi18Sk string `json:"gsi18sk,omitempty"`
	Gsi19Pk string `json:"gsi19pk,omitempty"`
	Gsi19Sk string `json:"gsi19sk,omitempty"`
	Gsi20Pk string `json:"gsi20pk,omitempty"`
	Gsi20Sk string `json:"gsi20sk,omitempty"`
}

type gsiSlot struct {
	pkAttr, skAttr string
	pkVal, skVal   string
	pkDst, skDst   *string
}

// gsiSlots maps OpaqueData GSI getters to the document GSI fields, ordered 1..20.
// Returning pointer destinations lets marshalItem set them in a single loop
// with the same "project SK when PK is present, coerce empty SK to NA" rule
// the DynamoDB adapter uses.
func gsiSlots(od *opaquedatav1.OpaqueData, doc *document) []gsiSlot {
	return []gsiSlot{
		{"gsi1pk", "gsi1sk", od.GetGsi1Pk(), od.GetGsi1Sk(), &doc.Gsi1Pk, &doc.Gsi1Sk},
		{"gsi2pk", "gsi2sk", od.GetGsi2Pk(), od.GetGsi2Sk(), &doc.Gsi2Pk, &doc.Gsi2Sk},
		{"gsi3pk", "gsi3sk", od.GetGsi3Pk(), od.GetGsi3Sk(), &doc.Gsi3Pk, &doc.Gsi3Sk},
		{"gsi4pk", "gsi4sk", od.GetGsi4Pk(), od.GetGsi4Sk(), &doc.Gsi4Pk, &doc.Gsi4Sk},
		{"gsi5pk", "gsi5sk", od.GetGsi5Pk(), od.GetGsi5Sk(), &doc.Gsi5Pk, &doc.Gsi5Sk},
		{"gsi6pk", "gsi6sk", od.GetGsi6Pk(), od.GetGsi6Sk(), &doc.Gsi6Pk, &doc.Gsi6Sk},
		{"gsi7pk", "gsi7sk", od.GetGsi7Pk(), od.GetGsi7Sk(), &doc.Gsi7Pk, &doc.Gsi7Sk},
		{"gsi8pk", "gsi8sk", od.GetGsi8Pk(), od.GetGsi8Sk(), &doc.Gsi8Pk, &doc.Gsi8Sk},
		{"gsi9pk", "gsi9sk", od.GetGsi9Pk(), od.GetGsi9Sk(), &doc.Gsi9Pk, &doc.Gsi9Sk},
		{"gsi10pk", "gsi10sk", od.GetGsi10Pk(), od.GetGsi10Sk(), &doc.Gsi10Pk, &doc.Gsi10Sk},
		{"gsi11pk", "gsi11sk", od.GetGsi11Pk(), od.GetGsi11Sk(), &doc.Gsi11Pk, &doc.Gsi11Sk},
		{"gsi12pk", "gsi12sk", od.GetGsi12Pk(), od.GetGsi12Sk(), &doc.Gsi12Pk, &doc.Gsi12Sk},
		{"gsi13pk", "gsi13sk", od.GetGsi13Pk(), od.GetGsi13Sk(), &doc.Gsi13Pk, &doc.Gsi13Sk},
		{"gsi14pk", "gsi14sk", od.GetGsi14Pk(), od.GetGsi14Sk(), &doc.Gsi14Pk, &doc.Gsi14Sk},
		{"gsi15pk", "gsi15sk", od.GetGsi15Pk(), od.GetGsi15Sk(), &doc.Gsi15Pk, &doc.Gsi15Sk},
		{"gsi16pk", "gsi16sk", od.GetGsi16Pk(), od.GetGsi16Sk(), &doc.Gsi16Pk, &doc.Gsi16Sk},
		{"gsi17pk", "gsi17sk", od.GetGsi17Pk(), od.GetGsi17Sk(), &doc.Gsi17Pk, &doc.Gsi17Sk},
		{"gsi18pk", "gsi18sk", od.GetGsi18Pk(), od.GetGsi18Sk(), &doc.Gsi18Pk, &doc.Gsi18Sk},
		{"gsi19pk", "gsi19sk", od.GetGsi19Pk(), od.GetGsi19Sk(), &doc.Gsi19Pk, &doc.Gsi19Sk},
		{"gsi20pk", "gsi20sk", od.GetGsi20Pk(), od.GetGsi20Sk(), &doc.Gsi20Pk, &doc.Gsi20Sk},
	}
}

func marshalItem(od *opaquedatav1.OpaqueData) ([]byte, error) {
	doc := document{
		ID:      od.GetSk(),
		Pk:      od.GetPk(),
		Sk:      od.GetSk(),
		Body:    od.GetBody(),
		T:       od.GetT(),
		Version: od.GetVersion(),
	}
	if t := od.GetT(); t > 0 {
		// Cosmos TTL is relative seconds from write time. Convert absolute
		// epoch to remaining seconds; clamp to a minimum of 1 since Cosmos
		// rejects 0/negative TTL on items.
		remaining := t - time.Now().Unix()
		if remaining < 1 {
			remaining = 1
		}
		doc.TTL = remaining
	}
	for _, g := range gsiSlots(od, &doc) {
		if g.pkVal != "" && g.pkVal != "NA" {
			*g.pkDst = g.pkVal
			skVal := g.skVal
			if skVal == "" {
				skVal = "NA"
			}
			*g.skDst = skVal
		} else if g.skVal != "" && g.skVal != "NA" {
			*g.skDst = g.skVal
		}
	}
	return json.Marshal(doc)
}

func unmarshalItem(raw []byte) (*opaquedatav1.OpaqueData, error) {
	var doc document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	od := &opaquedatav1.OpaqueData{
		Pk:      doc.Pk,
		Sk:      doc.Sk,
		Body:    doc.Body,
		T:       doc.T,
		Version: doc.Version,
		Gsi1Pk:  doc.Gsi1Pk, Gsi1Sk: doc.Gsi1Sk,
		Gsi2Pk: doc.Gsi2Pk, Gsi2Sk: doc.Gsi2Sk,
		Gsi3Pk: doc.Gsi3Pk, Gsi3Sk: doc.Gsi3Sk,
		Gsi4Pk: doc.Gsi4Pk, Gsi4Sk: doc.Gsi4Sk,
		Gsi5Pk: doc.Gsi5Pk, Gsi5Sk: doc.Gsi5Sk,
		Gsi6Pk: doc.Gsi6Pk, Gsi6Sk: doc.Gsi6Sk,
		Gsi7Pk: doc.Gsi7Pk, Gsi7Sk: doc.Gsi7Sk,
		Gsi8Pk: doc.Gsi8Pk, Gsi8Sk: doc.Gsi8Sk,
		Gsi9Pk: doc.Gsi9Pk, Gsi9Sk: doc.Gsi9Sk,
		Gsi10Pk: doc.Gsi10Pk, Gsi10Sk: doc.Gsi10Sk,
		Gsi11Pk: doc.Gsi11Pk, Gsi11Sk: doc.Gsi11Sk,
		Gsi12Pk: doc.Gsi12Pk, Gsi12Sk: doc.Gsi12Sk,
		Gsi13Pk: doc.Gsi13Pk, Gsi13Sk: doc.Gsi13Sk,
		Gsi14Pk: doc.Gsi14Pk, Gsi14Sk: doc.Gsi14Sk,
		Gsi15Pk: doc.Gsi15Pk, Gsi15Sk: doc.Gsi15Sk,
		Gsi16Pk: doc.Gsi16Pk, Gsi16Sk: doc.Gsi16Sk,
		Gsi17Pk: doc.Gsi17Pk, Gsi17Sk: doc.Gsi17Sk,
		Gsi18Pk: doc.Gsi18Pk, Gsi18Sk: doc.Gsi18Sk,
		Gsi19Pk: doc.Gsi19Pk, Gsi19Sk: doc.Gsi19Sk,
		Gsi20Pk: doc.Gsi20Pk, Gsi20Sk: doc.Gsi20Sk,
	}
	return od, nil
}

// isNotFound returns true when the underlying Cosmos request returned 404.
// The SDK reports this via *azcore.ResponseError.
func isNotFound(err error) bool {
	var rerr *azcore.ResponseError
	if errors.As(err, &rerr) {
		return rerr.StatusCode == http.StatusNotFound
	}
	return false
}

