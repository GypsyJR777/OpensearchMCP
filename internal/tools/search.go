package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GypsyJR777/OpensearchMCP/internal/opensearch"
)

const defaultSearchSize = 10

type searchInput struct {
	Index string         `json:"index" jsonschema:"Index name, alias or pattern to search, e.g. logs-*"`
	Body  map[string]any `json:"body,omitempty" jsonschema:"Search request body in OpenSearch Query DSL: query, aggs, sort, _source, size, from, highlight, etc. Omit to match all documents"`
}

type countInput struct {
	Index string         `json:"index" jsonschema:"Index name, alias or pattern, e.g. logs-*"`
	Query map[string]any `json:"query,omitempty" jsonschema:"Optional Query DSL query object, e.g. {\"term\": {\"status\": \"error\"}}. Omit to count all documents"`
}

func registerSearch(s *mcp.Server, d *Deps) {
	addTool(s, d, &mcp.Tool{
		Name: "search",
		Description: fmt.Sprintf("Search documents with OpenSearch Query DSL. Returns hits and aggregations. "+
			"size defaults to %d and must not exceed %d; use aggregations for statistics and _source filtering to keep responses small.",
			defaultSearchSize, d.MaxSearchSize),
		Annotations: readOnly("Search"),
	}, func(ctx context.Context, in searchInput) (any, error) {
		expr, err := d.Policy.Expression(in.Index)
		if err != nil {
			return nil, err
		}
		body, err := searchBody(in.Body, d.MaxSearchSize)
		if err != nil {
			return nil, err
		}
		return d.Backend.Do(ctx, opensearch.Request{Method: http.MethodPost, Path: "/" + expr + "/_search", Body: body})
	})

	addTool(s, d, &mcp.Tool{
		Name:        "count",
		Description: "Count documents matching an optional Query DSL query.",
		Annotations: readOnly("Count"),
	}, func(ctx context.Context, in countInput) (any, error) {
		expr, err := d.Policy.Expression(in.Index)
		if err != nil {
			return nil, err
		}
		var body any
		if in.Query != nil {
			body = map[string]any{"query": in.Query}
		}
		return d.Backend.Do(ctx, opensearch.Request{Method: http.MethodPost, Path: "/" + expr + "/_count", Body: body})
	})
}

// searchBody applies the default size and enforces the configured maximum.
// The input map is copied so the caller's value is never mutated.
func searchBody(in map[string]any, maxSize int) (map[string]any, error) {
	body := maps.Clone(in)
	if body == nil {
		body = map[string]any{}
	}
	for _, key := range []string{"scroll", "pit"} {
		if _, ok := body[key]; ok {
			return nil, fmt.Errorf("%q is not supported; paginate with from/size or search_after", key)
		}
	}

	raw, ok := body["size"]
	if !ok {
		body["size"] = defaultSearchSize
		return body, nil
	}
	size, err := toInt(raw)
	if err != nil {
		return nil, fmt.Errorf("size: %w", err)
	}
	if size < 0 || size > maxSize {
		return nil, fmt.Errorf("size must be between 0 and %d, got %d; use aggregations or pagination instead", maxSize, size)
	}
	return body, nil
}

func toInt(v any) (int, error) {
	switch n := v.(type) {
	case float64:
		if n != float64(int(n)) {
			return 0, fmt.Errorf("must be an integer, got %v", n)
		}
		return int(n), nil
	case int:
		return n, nil
	case json.Number:
		i, err := n.Int64()
		return int(i), err
	default:
		return 0, fmt.Errorf("must be an integer, got %T", v)
	}
}
