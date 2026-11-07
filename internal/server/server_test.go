package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GypsyJR777/OpensearchMCP/internal/config"
	"github.com/GypsyJR777/OpensearchMCP/internal/opensearch"
	"github.com/GypsyJR777/OpensearchMCP/internal/policy"
	"github.com/GypsyJR777/OpensearchMCP/internal/tools"
)

type stubBackend struct{ err error }

func (s stubBackend) Do(context.Context, opensearch.Request) (json.RawMessage, error) {
	return json.RawMessage(`{"cluster_name":"test"}`), s.err
}

func newTestServer(t *testing.T, token string, backend tools.Backend) *httptest.Server {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New("test", &tools.Deps{
		Backend:       backend,
		Policy:        policy.New(nil),
		ReadOnly:      true,
		MaxSearchSize: 100,
		Logger:        logger,
	})
	srv := httptest.NewServer(Handler(&config.Config{HTTPAuthToken: token}, s, backend, logger))
	t.Cleanup(srv.Close)
	return srv
}

func TestHealthEndpoints(t *testing.T) {
	ok := newTestServer(t, "", stubBackend{})
	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := http.Get(ok.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s = %d", path, resp.StatusCode)
		}
	}

	down := newTestServer(t, "", stubBackend{err: errors.New("down")})
	resp, err := http.Get(down.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("/readyz with backend down = %d", resp.StatusCode)
	}
}

func TestMCPOverHTTPRequiresToken(t *testing.T) {
	srv := newTestServer(t, "secret", stubBackend{})

	resp, err := http.Post(srv.URL+"/mcp", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d", resp.StatusCode)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "test"}, nil)
	cs, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:   srv.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearer{"secret"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "cluster_info"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || res.Content[0].(*mcp.TextContent).Text != `{"cluster_name":"test"}` {
		t.Errorf("unexpected result: %+v", res)
	}
}

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}
