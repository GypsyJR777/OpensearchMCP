package tools

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GypsyJR777/OpensearchMCP/internal/opensearch"
)

type clusterHealthInput struct {
	Index string `json:"index,omitempty" jsonschema:"Optional index expression to limit health to, e.g. logs-* or a,b"`
	Level string `json:"level,omitempty" jsonschema:"Detail level: cluster (default), indices or shards"`
}

func registerCluster(s *mcp.Server, d *Deps) {
	addTool(s, d, &mcp.Tool{
		Name:        "cluster_info",
		Description: "Get basic cluster information: cluster name, OpenSearch version and distribution.",
		Annotations: readOnly("Cluster info"),
	}, func(ctx context.Context, _ struct{}) (any, error) {
		return d.Backend.Do(ctx, opensearch.Request{Method: http.MethodGet, Path: "/"})
	})

	addTool(s, d, &mcp.Tool{
		Name:        "cluster_health",
		Description: "Get cluster health: status (green/yellow/red), node counts and shard allocation summary.",
		Annotations: readOnly("Cluster health"),
	}, func(ctx context.Context, in clusterHealthInput) (any, error) {
		path := "/_cluster/health"
		if in.Index != "" {
			expr, err := d.Policy.Expression(in.Index)
			if err != nil {
				return nil, err
			}
			path += "/" + expr
		}

		q := url.Values{}
		switch in.Level {
		case "", "cluster":
		case "indices", "shards":
			// Per-index detail would reveal index names outside the allowlist.
			if in.Index == "" && d.Policy.Restricted() {
				return nil, errors.New("level indices/shards requires an index when OPENSEARCH_ALLOWED_INDICES is set")
			}
			q.Set("level", in.Level)
		default:
			return nil, errors.New("level must be one of: cluster, indices, shards")
		}
		return d.Backend.Do(ctx, opensearch.Request{Method: http.MethodGet, Path: path, Query: q})
	})

	addTool(s, d, &mcp.Tool{
		Name:        "list_nodes",
		Description: "List cluster nodes with roles, cluster-manager flag, heap/RAM/CPU/disk usage and version.",
		Annotations: readOnly("List nodes"),
	}, func(ctx context.Context, _ struct{}) (any, error) {
		return d.Backend.Do(ctx, opensearch.Request{
			Method: http.MethodGet,
			Path:   "/_cat/nodes",
			Query: url.Values{
				"format": {"json"},
				"h":      {"name,ip,node.role,cluster_manager,heap.percent,ram.percent,cpu,load_1m,disk.used_percent,version"},
				"s":      {"name"},
			},
		})
	})
}
