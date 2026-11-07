// Package tools defines the MCP tools exposed by the server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GypsyJR777/OpensearchMCP/internal/opensearch"
	"github.com/GypsyJR777/OpensearchMCP/internal/policy"
)

// Backend is the subset of the OpenSearch client the tools depend on.
type Backend interface {
	Do(ctx context.Context, r opensearch.Request) (json.RawMessage, error)
}

// Deps carries everything tool handlers need.
type Deps struct {
	Backend       Backend
	Policy        *policy.Policy
	ReadOnly      bool
	MaxSearchSize int
	Logger        *slog.Logger
}

// Register adds all tools to the server. Write tools are added only when
// read-only mode is disabled.
func Register(s *mcp.Server, d *Deps) {
	registerCluster(s, d)
	registerIndices(s, d)
	registerSearch(s, d)
	registerDocuments(s, d)
	if !d.ReadOnly {
		registerWrite(s, d)
	}
}

// handlerFunc is the shape of every tool implementation: it returns a value
// that is serialized to JSON text for the model.
type handlerFunc[In any] func(ctx context.Context, in In) (any, error)

// addTool registers a typed tool and wraps it with logging and JSON encoding.
func addTool[In any](s *mcp.Server, d *Deps, t *mcp.Tool, h handlerFunc[In]) {
	mcp.AddTool(s, t, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		start := time.Now()
		var text string
		out, err := h(ctx, in)
		if err == nil {
			text, err = encode(out)
		}
		log := d.Logger.With("tool", t.Name, "duration", time.Since(start))
		if err != nil {
			log.Warn("tool call failed", "error", err)
			return nil, nil, err
		}
		log.Debug("tool call succeeded")
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: text}},
		}, nil, nil
	})
}

func encode(v any) (string, error) {
	switch v := v.(type) {
	case json.RawMessage:
		return string(v), nil
	case string:
		return v, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encoding result: %w", err)
	}
	return string(b), nil
}

// readOnly returns annotations for tools that never modify the cluster.
func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		Title:          title,
		ReadOnlyHint:   true,
		IdempotentHint: true,
		OpenWorldHint:  ptr(false),
	}
}

// mutating returns annotations for tools that modify the cluster.
func mutating(title string, destructive, idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		Title:           title,
		DestructiveHint: ptr(destructive),
		IdempotentHint:  idempotent,
		OpenWorldHint:   ptr(false),
	}
}

func ptr[T any](v T) *T { return &v }

// filterRows keeps only _cat rows whose key column names an allowed index.
func filterRows(raw json.RawMessage, key string, p *policy.Policy) (any, error) {
	if !p.Restricted() {
		return raw, nil
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("decoding _cat response: %w", err)
	}
	kept := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if name, _ := row[key].(string); p.AllowsName(name) {
			kept = append(kept, row)
		}
	}
	return kept, nil
}
