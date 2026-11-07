//go:build integration

// Package integration exercises every tool against a real OpenSearch cluster.
//
//	docker compose up -d opensearch
//	OPENSEARCH_URL=http://localhost:9200 go test -tags integration ./test/integration/
package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GypsyJR777/OpensearchMCP/internal/config"
	"github.com/GypsyJR777/OpensearchMCP/internal/opensearch"
	"github.com/GypsyJR777/OpensearchMCP/internal/policy"
	"github.com/GypsyJR777/OpensearchMCP/internal/server"
	"github.com/GypsyJR777/OpensearchMCP/internal/tools"
)

func session(t *testing.T, allowed []string) *mcp.ClientSession {
	t.Helper()
	addr := os.Getenv("OPENSEARCH_URL")
	if addr == "" {
		t.Skip("OPENSEARCH_URL is not set")
	}
	cfg := &config.Config{
		Addresses:        []string{addr},
		RequestTimeout:   10 * time.Second,
		MaxResponseBytes: 512 * 1024,
		MaxSearchSize:    100,
	}
	client, err := opensearch.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := server.New("test", &tools.Deps{
		Backend:       client,
		Policy:        policy.New(allowed),
		ReadOnly:      false,
		MaxSearchSize: cfg.MaxSearchSize,
		Logger:        logger,
	})
	srv := httptest.NewServer(server.Handler(cfg, s, client, logger))
	t.Cleanup(srv.Close)

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "it", Version: "test"}, nil).
		Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// call invokes a tool, fails the test on tool errors and decodes the JSON text.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) any {
	t.Helper()
	text, isErr := callRaw(t, cs, name, args)
	if isErr {
		t.Fatalf("%s(%v) failed: %s", name, args, text)
	}
	var v any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatalf("%s returned invalid JSON %q: %v", name, text, err)
	}
	return v
}

func callRaw(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res.Content[0].(*mcp.TextContent).Text, res.IsError
}

func field(v any, path ...string) any {
	for _, p := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[p]
	}
	return v
}

func TestAllTools(t *testing.T) {
	cs := session(t, nil)
	index := fmt.Sprintf("mcp-it-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		// Best effort: there is intentionally no delete_index tool.
		c, _ := opensearch.New(&config.Config{Addresses: []string{os.Getenv("OPENSEARCH_URL")}, RequestTimeout: 5 * time.Second, MaxResponseBytes: 1 << 20})
		_, _ = c.Do(t.Context(), opensearch.Request{Method: "DELETE", Path: "/" + index})
	})

	if v := call(t, cs, "cluster_info", nil); field(v, "version", "number") == nil {
		t.Errorf("cluster_info: %v", v)
	}
	if v := call(t, cs, "cluster_health", nil); field(v, "status") == nil {
		t.Errorf("cluster_health: %v", v)
	}
	if v := call(t, cs, "list_nodes", nil).([]any); len(v) == 0 || field(v[0], "cluster_manager") == nil {
		t.Errorf("list_nodes: %v", v)
	}

	call(t, cs, "create_index", map[string]any{
		"index": index,
		"body": map[string]any{
			"settings": map[string]any{"number_of_replicas": 0},
			"mappings": map[string]any{"properties": map[string]any{
				"level": map[string]any{"type": "keyword"},
				"msg":   map[string]any{"type": "text"},
			}},
			"aliases": map[string]any{index + "-alias": map[string]any{}},
		},
	})

	docs := []map[string]any{
		{"level": "error", "msg": "disk full"},
		{"level": "info", "msg": "started"},
		{"level": "error", "msg": "timeout"},
	}
	for i, doc := range docs {
		args := map[string]any{"index": index, "document": doc, "refresh": true}
		if i == 0 {
			args["id"] = "weird/id?1"
		}
		if v := call(t, cs, "index_document", args); field(v, "result") != "created" {
			t.Fatalf("index_document: %v", v)
		}
	}

	if v := call(t, cs, "get_document", map[string]any{"index": index, "id": "weird/id?1", "source_includes": []string{"msg"}}); field(v, "_source", "msg") != "disk full" || field(v, "_source", "level") != nil {
		t.Errorf("get_document: %v", v)
	}

	if v := call(t, cs, "list_indices", map[string]any{"pattern": "mcp-it-*"}).([]any); !containsRow(v, "index", index) {
		t.Errorf("list_indices: %v", v)
	}
	if v := call(t, cs, "get_mapping", map[string]any{"index": index}); field(v, index, "mappings", "properties", "level", "type") != "keyword" {
		t.Errorf("get_mapping: %v", v)
	}
	if v := call(t, cs, "get_index", map[string]any{"index": index}); field(v, index, "settings", "index.number_of_replicas") != "0" {
		t.Errorf("get_index: %v", v)
	}
	if v := call(t, cs, "list_aliases", map[string]any{"pattern": index + "-alias"}).([]any); !containsRow(v, "index", index) {
		t.Errorf("list_aliases: %v", v)
	}
	if v := call(t, cs, "list_shards", map[string]any{"index": index}).([]any); !containsRow(v, "state", "STARTED") {
		t.Errorf("list_shards: %v", v)
	}

	if v := call(t, cs, "count", map[string]any{"index": index, "query": map[string]any{"term": map[string]any{"level": "error"}}}); field(v, "count") != float64(2) {
		t.Errorf("count: %v", v)
	}

	v := call(t, cs, "search", map[string]any{"index": index + "-alias", "body": map[string]any{
		"query": map[string]any{"match": map[string]any{"msg": "timeout"}},
		"aggs":  map[string]any{"levels": map[string]any{"terms": map[string]any{"field": "level"}}},
	}})
	if hits := field(v, "hits", "hits").([]any); len(hits) != 1 || field(hits[0], "_source", "msg") != "timeout" {
		t.Errorf("search hits: %v", v)
	}
	if field(v, "aggregations", "levels", "buckets") == nil {
		t.Errorf("search aggs: %v", v)
	}

	if v := call(t, cs, "delete_document", map[string]any{"index": index, "id": "weird/id?1", "refresh": true}); field(v, "result") != "deleted" {
		t.Errorf("delete_document: %v", v)
	}
	if text, isErr := callRaw(t, cs, "get_document", map[string]any{"index": index, "id": "weird/id?1"}); !isErr || !strings.Contains(text, "404") {
		t.Errorf("deleted document still readable: %s", text)
	}
}

func TestAllowlistAgainstRealCluster(t *testing.T) {
	cs := session(t, []string{"mcp-allowed-*"})

	if v := call(t, cs, "list_indices", nil).([]any); len(v) != 0 {
		for _, row := range v {
			if name, _ := field(row, "index").(string); !strings.HasPrefix(name, "mcp-allowed-") {
				t.Errorf("list_indices leaked %q", name)
			}
		}
	}
	if text, isErr := callRaw(t, cs, "search", map[string]any{"index": "*"}); !isErr {
		t.Errorf("search on * should be denied: %s", text)
	}
}

func containsRow(rows []any, key, want string) bool {
	for _, r := range rows {
		if field(r, key) == want {
			return true
		}
	}
	return false
}
