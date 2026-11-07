package tools

import (
	"context"
	"net/http"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GypsyJR777/OpensearchMCP/internal/opensearch"
	"github.com/GypsyJR777/OpensearchMCP/internal/policy"
)

type listIndicesInput struct {
	Pattern       string `json:"pattern,omitempty" jsonschema:"Optional index pattern, e.g. logs-*. Defaults to all indices"`
	IncludeHidden bool   `json:"include_hidden,omitempty" jsonschema:"Include hidden and system indices (names starting with a dot)"`
}

type indexInput struct {
	Index string `json:"index" jsonschema:"Index name, alias or pattern, e.g. logs-* or a,b"`
}

type listAliasesInput struct {
	Pattern string `json:"pattern,omitempty" jsonschema:"Optional alias name pattern. Defaults to all aliases"`
}

type listShardsInput struct {
	Index string `json:"index,omitempty" jsonschema:"Optional index pattern. Defaults to all indices"`
}

func registerIndices(s *mcp.Server, d *Deps) {
	addTool(s, d, &mcp.Tool{
		Name:        "list_indices",
		Description: "List indices with health, status, primary/replica counts, document count and store size.",
		Annotations: readOnly("List indices"),
	}, func(ctx context.Context, in listIndicesInput) (any, error) {
		path, err := catPath("/_cat/indices", in.Pattern)
		if err != nil {
			return nil, err
		}
		expand := "open,closed"
		if in.IncludeHidden {
			expand = "all"
		}
		raw, err := d.Backend.Do(ctx, opensearch.Request{
			Method: http.MethodGet,
			Path:   path,
			Query: url.Values{
				"format":           {"json"},
				"h":                {"health,status,index,pri,rep,docs.count,store.size"},
				"s":                {"index"},
				"expand_wildcards": {expand},
			},
		})
		if err != nil {
			return nil, err
		}
		return filterRows(raw, "index", d.Policy)
	})

	addTool(s, d, &mcp.Tool{
		Name:        "get_mapping",
		Description: "Get field mappings (field names and types) of an index. Use it before writing queries to learn the schema.",
		Annotations: readOnly("Get mapping"),
	}, func(ctx context.Context, in indexInput) (any, error) {
		expr, err := d.Policy.Expression(in.Index)
		if err != nil {
			return nil, err
		}
		return d.Backend.Do(ctx, opensearch.Request{Method: http.MethodGet, Path: "/" + expr + "/_mapping"})
	})

	addTool(s, d, &mcp.Tool{
		Name:        "get_index",
		Description: "Get full index definition: aliases, mappings and settings (flattened).",
		Annotations: readOnly("Get index"),
	}, func(ctx context.Context, in indexInput) (any, error) {
		expr, err := d.Policy.Expression(in.Index)
		if err != nil {
			return nil, err
		}
		return d.Backend.Do(ctx, opensearch.Request{
			Method: http.MethodGet,
			Path:   "/" + expr,
			Query:  url.Values{"flat_settings": {"true"}},
		})
	})

	addTool(s, d, &mcp.Tool{
		Name:        "list_aliases",
		Description: "List index aliases with the indices they point to, filters and write-index flags.",
		Annotations: readOnly("List aliases"),
	}, func(ctx context.Context, in listAliasesInput) (any, error) {
		path, err := catPath("/_cat/aliases", in.Pattern)
		if err != nil {
			return nil, err
		}
		raw, err := d.Backend.Do(ctx, opensearch.Request{
			Method: http.MethodGet,
			Path:   path,
			Query: url.Values{
				"format": {"json"},
				"h":      {"alias,index,filter,routing.index,routing.search,is_write_index"},
				"s":      {"alias"},
			},
		})
		if err != nil {
			return nil, err
		}
		return filterRows(raw, "index", d.Policy)
	})

	addTool(s, d, &mcp.Tool{
		Name:        "list_shards",
		Description: "List shards with state, primary/replica role, document count, size, node and unassigned reason. Useful for diagnosing yellow/red health.",
		Annotations: readOnly("List shards"),
	}, func(ctx context.Context, in listShardsInput) (any, error) {
		path, err := catPath("/_cat/shards", in.Index)
		if err != nil {
			return nil, err
		}
		raw, err := d.Backend.Do(ctx, opensearch.Request{
			Method: http.MethodGet,
			Path:   path,
			Query: url.Values{
				"format": {"json"},
				"h":      {"index,shard,prirep,state,docs,store,node,unassigned.reason"},
				"s":      {"index,shard,prirep"},
			},
		})
		if err != nil {
			return nil, err
		}
		return filterRows(raw, "index", d.Policy)
	})
}

// catPath appends an optional pattern to a _cat endpoint. The allowlist is not
// applied here: _cat results are filtered row by row instead, so broad
// patterns like "*" still work for restricted users.
func catPath(base, pattern string) (string, error) {
	if pattern == "" {
		return base, nil
	}
	expr, err := policy.Syntax(pattern)
	if err != nil {
		return "", err
	}
	return base + "/" + expr, nil
}
