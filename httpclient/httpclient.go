// Package httpclient provides a generic HTTP client for protosource aggregates
// with protobuf-first content negotiation.
package httpclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	historyv1 "github.com/funinthecloud/protosource/history/v1"
	responsev1 "github.com/funinthecloud/protosource/response/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Doer is the interface that generated per-aggregate clients depend on.
// *Client satisfies it. Consumers can mock this for testing.
type Doer interface {
	Apply(ctx context.Context, routePath string, cmd proto.Message) (*responsev1.CommandResponse, error)
	Load(ctx context.Context, routePath string, id string, target proto.Message) error
	Get(ctx context.Context, routePath string, id string, target proto.Message) error
	History(ctx context.Context, routePath string, id string) (*historyv1.History, error)
	Query(ctx context.Context, routePath string, queryPath string, params map[string]string, target proto.Message) error
}

// AuthProvider decorates outgoing HTTP requests with authentication.
// Implementations can add bearer tokens, API keys, signed headers, etc.
// Also provides the actor identity for command attribution.
type AuthProvider interface {
	Authenticate(req *http.Request) error
	Actor() string
}

// Client is a generic protosource HTTP client with content negotiation.
type Client struct {
	httpClient *http.Client
	baseURL    string
	useJSON    bool
	auth       AuthProvider
}

// Option configures a Client.
type Option func(*Client)

// New creates a new Client. Panics if auth is nil.
func New(baseURL string, auth AuthProvider, opts ...Option) *Client {
	if auth == nil {
		panic("httpclient.New: auth must not be nil (use httpclient.NewNoAuth for no authentication)")
	}
	c := &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    strings.TrimRight(baseURL, "/"),
		auth:       auth,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// WithHTTPClient sets a custom http.Client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithJSON uses JSON serialization instead of protobuf.
func WithJSON() Option {
	return func(c *Client) { c.useJSON = true }
}

// Apply sends a command to the server and returns the result.
// The actor field is set from the AuthProvider before serialization.
func (c *Client) Apply(ctx context.Context, routePath string, cmd proto.Message) (*responsev1.CommandResponse, error) {
	// Set actor via proto reflection.
	setActorField(cmd, c.auth.Actor())

	cmdName := strings.ToLower(string(cmd.ProtoReflect().Descriptor().Name()))
	url := c.baseURL + "/" + routePath + "/" + cmdName

	body, contentType, accept, err := c.marshal(cmd)
	if err != nil {
		return nil, fmt.Errorf("httpclient: marshal command: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("httpclient: create request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", accept)

	if err := c.auth.Authenticate(req); err != nil {
		return nil, fmt.Errorf("httpclient: authenticate: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("httpclient: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("httpclient: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, parseAPIError(resp.StatusCode, respBody)
	}

	result := &responsev1.CommandResponse{}
	if err := c.unmarshal(respBody, resp.Header.Get("Content-Type"), result); err != nil {
		return nil, err
	}
	return result, nil
}

// Load retrieves an aggregate by ID via event replay, unmarshaling into the provided message.
func (c *Client) Load(ctx context.Context, routePath string, id string, target proto.Message) error {
	reqURL := c.baseURL + "/" + routePath + "/" + url.PathEscape(id)
	return c.getInto(ctx, reqURL, target)
}

// Get retrieves a materialized aggregate by ID from the aggregate store.
func (c *Client) Get(ctx context.Context, routePath string, id string, target proto.Message) error {
	reqURL := c.baseURL + "/" + routePath + "/get/" + url.PathEscape(id)
	return c.getInto(ctx, reqURL, target)
}

// History retrieves the event history for an aggregate.
func (c *Client) History(ctx context.Context, routePath string, id string) (*historyv1.History, error) {
	reqURL := c.baseURL + "/" + routePath + "/" + url.PathEscape(id) + "/history"
	history := &historyv1.History{}
	if err := c.getInto(ctx, reqURL, history); err != nil {
		return nil, err
	}
	return history, nil
}

// Query sends a GET request to a query endpoint and unmarshals the response
// into the target proto.Message (typically an {Aggregate}List).
// The queryPath is appended to {baseURL}/{routePath}/query/{queryPath}.
// Params are sent as URL query parameters.
func (c *Client) Query(ctx context.Context, routePath string, queryPath string, params map[string]string, target proto.Message) error {
	reqURL := c.baseURL + "/" + routePath + "/query/" + queryPath
	if len(params) > 0 {
		v := url.Values{}
		for k, val := range params {
			v.Set(k, val)
		}
		reqURL += "?" + v.Encode()
	}
	return c.getInto(ctx, reqURL, target)
}

// getInto sends a GET request with content negotiation and unmarshals the response.
func (c *Client) getInto(ctx context.Context, reqURL string, target proto.Message) error {
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return fmt.Errorf("httpclient: create request: %w", err)
	}
	req.Header.Set("Accept", c.acceptHeader())

	if err := c.auth.Authenticate(req); err != nil {
		return fmt.Errorf("httpclient: authenticate: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("httpclient: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("httpclient: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return parseAPIError(resp.StatusCode, respBody)
	}

	return c.unmarshal(respBody, resp.Header.Get("Content-Type"), target)
}

func (c *Client) marshal(msg proto.Message) (body []byte, contentType, accept string, err error) {
	if c.useJSON {
		b, err := protojson.Marshal(msg)
		if err != nil {
			return nil, "", "", err
		}
		return b, "application/json", "application/json", nil
	}
	b, err := proto.Marshal(msg)
	if err != nil {
		return nil, "", "", err
	}
	return b, "application/protobuf", "application/protobuf", nil
}

func (c *Client) unmarshal(body []byte, contentType string, target proto.Message) error {
	if strings.Contains(contentType, "json") {
		if err := protojson.Unmarshal(body, target); err != nil {
			return fmt.Errorf("httpclient: unmarshal json: %w", err)
		}
		return nil
	}
	if err := proto.Unmarshal(body, target); err != nil {
		return fmt.Errorf("httpclient: unmarshal protobuf: %w", err)
	}
	return nil
}

func (c *Client) acceptHeader() string {
	if c.useJSON {
		return "application/json"
	}
	return "application/protobuf"
}

// setActorField sets the "actor" field (field number 2) on a command message
// via proto reflection.
func setActorField(msg proto.Message, actor string) {
	md := msg.ProtoReflect().Descriptor()
	fd := md.Fields().ByNumber(2) // actor is always field 2 per convention
	if fd != nil && fd.Kind() == protoreflect.StringKind {
		msg.ProtoReflect().Set(fd, protoreflect.ValueOfString(actor))
	}
}
