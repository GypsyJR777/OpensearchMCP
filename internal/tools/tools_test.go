package tools

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GypsyJR777/OpensearchMCP/internal/opensearch"
	"github.com/GypsyJR777/OpensearchMCP/internal/policy"
)

// fakeBackend records requests and replies with a canned response.
type fakeBackend struct {
	reqs     []opensearch.Request
	response string
}

func (f *fakeBackend) Do(_ context.Context, r opensearch.Request) (json.RawMessage, error) {
	f.reqs = append(f.reqs, r)
	if f.response == "" {
		return json.RawMessage(`{}`), nil
	}
	return json.RawMessage(f.response), nil
}

func (f *fakeBackend) last(t *testing.T) opensearch.Request {
	t.Helper()
	if len(f.reqs) == 0 {
		t.Fatal("no request was sent to the backend")
	}
	return f.reqs[len(f.reqs)-1]
}

func connect(t *testing.T, d *Deps) *mcp.ClientSession {
	t.Helper()
	if d.Policy == nil {
		d.Policy = policy.New(nil)
	}
	if d.MaxSearchSize == 0 {
		d.MaxSearchSize = 100
	}
	d.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	s := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	Register(s, d)

	ctx := t.Context()
	serverT, clientT := mcp.NewInMemoryTransports()
	if _, err := s.Connect(ctx, serverT, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "test"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	var text strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	return text.String(), res.IsError
}

func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

func TestReadOnlyHidesWriteTools(t *testing.T) {
	ro := toolNames(t, connect(t, &Deps{Backend: &fakeBackend{}, ReadOnly: true}))
	want := []string{"cluster_health", "cluster_info", "count", "get_document", "get_index", "get_mapping",
		"list_aliases", "list_indices", "list_nodes", "list_shards", "search"}
	if !slices.Equal(ro, want) {
		t.Errorf("read-only tools = %v, want %v", ro, want)
	}

	rw := toolNames(t, connect(t, &Deps{Backend: &fakeBackend{}, ReadOnly: false}))
	for _, name := range []string{"index_document", "delete_document", "create_index"} {
		if !slices.Contains(rw, name) {
			t.Errorf("write tool %s missing when read-only is disabled", name)
		}
	}
}

func TestSearchAppliesDefaultSize(t *testing.T) {
	b := &fakeBackend{response: `{"hits":{"hits":[]}}`}
	cs := connect(t, &Deps{Backend: b, ReadOnly: true})

	out, isErr := call(t, cs, "search", map[string]any{
		"index": "logs-*",
		"body":  map[string]any{"query": map[string]any{"match_all": map[string]any{}}},
	})
	if isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	if out != `{"hits":{"hits":[]}}` {
		t.Errorf("output = %s", out)
	}
	req := b.last(t)
	if req.Method != "POST" || req.Path != "/logs-*/_search" {
		t.Errorf("request = %s %s", req.Method, req.Path)
	}
	if body := req.Body.(map[string]any); body["size"] != defaultSearchSize {
		t.Errorf("size = %v, want %d", body["size"], defaultSearchSize)
	}
}

func TestSearchRejectsLargeSize(t *testing.T) {
	b := &fakeBackend{}
	cs := connect(t, &Deps{Backend: b, ReadOnly: true, MaxSearchSize: 50})

	out, isErr := call(t, cs, "search", map[string]any{"index": "logs", "body": map[string]any{"size": 51}})
	if !isErr || !strings.Contains(out, "between 0 and 50") {
		t.Errorf("expected size error, got isErr=%v out=%s", isErr, out)
	}
	if len(b.reqs) != 0 {
		t.Error("request must not reach the backend")
	}
}

func TestAllowlistEnforced(t *testing.T) {
	b := &fakeBackend{}
	cs := connect(t, &Deps{Backend: b, ReadOnly: true, Policy: policy.New([]string{"logs-*"})})

	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"search", map[string]any{"index": "*"}},
		{"search", map[string]any{"index": "secret"}},
		{"count", map[string]any{"index": "logs-a,secret"}},
		{"get_mapping", map[string]any{"index": "secret"}},
		{"get_document", map[string]any{"index": "secret", "id": "1"}},
		{"cluster_health", map[string]any{"level": "indices"}},
	} {
		if out, isErr := call(t, cs, tc.tool, tc.args); !isErr {
			t.Errorf("%s(%v) should be denied, got %s", tc.tool, tc.args, out)
		}
	}
	if len(b.reqs) != 0 {
		t.Errorf("denied calls reached the backend: %+v", b.reqs)
	}

	if out, isErr := call(t, cs, "search", map[string]any{"index": "logs-2024*"}); isErr {
		t.Errorf("covered wildcard should be allowed: %s", out)
	}
}

func TestListIndicesFiltersRows(t *testing.T) {
	b := &fakeBackend{response: `[{"index":"logs-1"},{"index":"secret"},{"index":"logs-2"}]`}
	cs := connect(t, &Deps{Backend: b, ReadOnly: true, Policy: policy.New([]string{"logs-*"})})

	out, isErr := call(t, cs, "list_indices", nil)
	if isErr {
		t.Fatal(out)
	}
	if out != `[{"index":"logs-1"},{"index":"logs-2"}]` {
		t.Errorf("output = %s", out)
	}
	req := b.last(t)
	if req.Path != "/_cat/indices" || req.Query.Get("format") != "json" || req.Query.Get("expand_wildcards") != "open,closed" {
		t.Errorf("request = %s?%s", req.Path, req.Query.Encode())
	}
}

func TestGetDocumentEscapesID(t *testing.T) {
	b := &fakeBackend{}
	cs := connect(t, &Deps{Backend: b, ReadOnly: true})

	if out, isErr := call(t, cs, "get_document", map[string]any{
		"index": "logs", "id": "a/b?c", "source_includes": []string{"msg", "ts"},
	}); isErr {
		t.Fatal(out)
	}
	req := b.last(t)
	if req.Path != "/logs/_doc/a%2Fb%3Fc" {
		t.Errorf("path = %s", req.Path)
	}
	if req.Query.Get("_source_includes") != "msg,ts" {
		t.Errorf("query = %s", req.Query.Encode())
	}
}

func TestIndexDocument(t *testing.T) {
	b := &fakeBackend{}
	cs := connect(t, &Deps{Backend: b, ReadOnly: false})

	doc := map[string]any{"msg": "hi"}
	if out, isErr := call(t, cs, "index_document", map[string]any{"index": "logs", "document": doc}); isErr {
		t.Fatal(out)
	}
	if req := b.last(t); req.Method != "POST" || req.Path != "/logs/_doc" || req.Query != nil {
		t.Errorf("auto-id request = %s %s?%s", req.Method, req.Path, req.Query.Encode())
	}

	if out, isErr := call(t, cs, "index_document", map[string]any{"index": "logs", "id": "7", "document": doc, "refresh": true}); isErr {
		t.Fatal(out)
	}
	if req := b.last(t); req.Method != "PUT" || req.Path != "/logs/_doc/7" || req.Query.Get("refresh") != "wait_for" {
		t.Errorf("explicit-id request = %s %s?%s", req.Method, req.Path, req.Query.Encode())
	}

	if _, isErr := call(t, cs, "index_document", map[string]any{"index": "logs-*", "document": doc}); !isErr {
		t.Error("wildcard target should be rejected")
	}
}

func TestSearchBodyIsNotMutated(t *testing.T) {
	in := map[string]any{"query": map[string]any{}}
	if _, err := searchBody(in, 100); err != nil {
		t.Fatal(err)
	}
	if _, ok := in["size"]; ok {
		t.Error("searchBody mutated its input")
	}
	if _, err := searchBody(map[string]any{"scroll": "1m"}, 100); err == nil {
		t.Error("scroll should be rejected")
	}
	if _, err := searchBody(map[string]any{"size": 1.5}, 100); err == nil {
		t.Error("fractional size should be rejected")
	}
}
