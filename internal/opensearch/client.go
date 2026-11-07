// Package opensearch wraps opensearch-go with a raw-JSON request API suited to
// MCP tools: responses are passed through to the model as-is, bounded in size.
package opensearch

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	opensearchgo "github.com/opensearch-project/opensearch-go/v4"

	"github.com/GypsyJR777/OpensearchMCP/internal/config"
)

// ErrResponseTooLarge is returned when a response exceeds the configured limit.
var ErrResponseTooLarge = errors.New("response too large")

// Client performs raw JSON requests against OpenSearch.
type Client struct {
	os               *opensearchgo.Client
	maxResponseBytes int64
}

// Request describes a single REST call.
type Request struct {
	Method string
	Path   string
	Query  url.Values
	Body   any // marshaled to JSON when non-nil
}

// StatusError is returned for non-2xx responses.
type StatusError struct {
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("opensearch returned HTTP %d: %s", e.StatusCode, e.Body)
}

// New creates a client from configuration.
func New(cfg *config.Config) (*Client, error) {
	osCfg := opensearchgo.Config{
		Addresses:          cfg.Addresses,
		Username:           cfg.Username,
		Password:           cfg.Password,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
		RequestTimeout:     cfg.RequestTimeout,
		MaxRetries:         cfg.MaxRetries,
		DisableRetry:       cfg.MaxRetries == 0,
	}

	if cfg.CACertFile != "" {
		ca, err := os.ReadFile(cfg.CACertFile)
		if err != nil {
			return nil, fmt.Errorf("reading OPENSEARCH_CA_CERT: %w", err)
		}
		osCfg.CACert = ca
	}

	if cfg.ClientCertFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.ClientCertFile, cfg.ClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("loading client certificate: %w", err)
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
		}
		osCfg.Transport = transport
	}

	c, err := opensearchgo.NewClient(osCfg)
	if err != nil {
		return nil, fmt.Errorf("creating opensearch client: %w", err)
	}
	return &Client{os: c, maxResponseBytes: cfg.MaxResponseBytes}, nil
}

// Do executes the request and returns the raw JSON response body.
func (c *Client) Do(ctx context.Context, r Request) (json.RawMessage, error) {
	var body io.Reader
	if r.Body != nil {
		b, err := json.Marshal(r.Body)
		if err != nil {
			return nil, fmt.Errorf("encoding request body: %w", err)
		}
		body = bytes.NewReader(b)
	}

	target := r.Path
	if len(r.Query) > 0 {
		target += "?" + r.Query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, target, body)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.os.Stream(req)
	if resp == nil {
		return nil, fmt.Errorf("opensearch request failed: %w", err)
	}
	defer resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("opensearch request failed: %w", err)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, c.maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading opensearch response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &StatusError{StatusCode: resp.StatusCode, Body: errorSummary(data)}
	}
	if int64(len(data)) > c.maxResponseBytes {
		return nil, fmt.Errorf("%w: exceeds %d bytes; narrow the request (smaller size, _source filtering, more specific index)",
			ErrResponseTooLarge, c.maxResponseBytes)
	}
	if len(data) == 0 {
		return json.RawMessage("{}"), nil
	}
	// Some endpoints pretty-print by default; whitespace only costs tokens.
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err == nil {
		return compact.Bytes(), nil
	}
	return data, nil
}

// errorSummary keeps error bodies short enough to be useful to the model.
func errorSummary(body []byte) string {
	const limit = 4096
	s := strings.TrimSpace(string(body))
	if len(s) > limit {
		s = s[:limit] + "...(truncated)"
	}
	if s == "" {
		s = "(empty body)"
	}
	return s
}

// Close releases background resources held by the transport.
func (c *Client) Close() error {
	return c.os.Close()
}
