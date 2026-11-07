package tools

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GypsyJR777/OpensearchMCP/internal/opensearch"
)

type getDocumentInput struct {
	Index          string   `json:"index" jsonschema:"Concrete index name or alias"`
	ID             string   `json:"id" jsonschema:"Document ID"`
	SourceIncludes []string `json:"source_includes,omitempty" jsonschema:"Optional list of _source fields to return, wildcards allowed"`
}

type indexDocumentInput struct {
	Index    string         `json:"index" jsonschema:"Concrete index name or alias with a write index"`
	ID       string         `json:"id,omitempty" jsonschema:"Optional document ID. If omitted OpenSearch generates one; if set, an existing document is replaced"`
	Document map[string]any `json:"document" jsonschema:"Document source as a JSON object"`
	Refresh  bool           `json:"refresh,omitempty" jsonschema:"Wait until the change is visible to search before returning"`
}

type deleteDocumentInput struct {
	Index   string `json:"index" jsonschema:"Concrete index name"`
	ID      string `json:"id" jsonschema:"Document ID"`
	Refresh bool   `json:"refresh,omitempty" jsonschema:"Wait until the change is visible to search before returning"`
}

type createIndexInput struct {
	Index string         `json:"index" jsonschema:"New index name (lowercase)"`
	Body  map[string]any `json:"body,omitempty" jsonschema:"Optional index definition with settings, mappings and aliases"`
}

func registerDocuments(s *mcp.Server, d *Deps) {
	addTool(s, d, &mcp.Tool{
		Name:        "get_document",
		Description: "Get a single document by index and ID.",
		Annotations: readOnly("Get document"),
	}, func(ctx context.Context, in getDocumentInput) (any, error) {
		index, err := d.Policy.Concrete(in.Index)
		if err != nil {
			return nil, err
		}
		path, err := docPath(index, in.ID)
		if err != nil {
			return nil, err
		}
		q := url.Values{}
		if len(in.SourceIncludes) > 0 {
			q.Set("_source_includes", strings.Join(in.SourceIncludes, ","))
		}
		return d.Backend.Do(ctx, opensearch.Request{Method: http.MethodGet, Path: path, Query: q})
	})
}

func registerWrite(s *mcp.Server, d *Deps) {
	addTool(s, d, &mcp.Tool{
		Name:        "index_document",
		Description: "Create or replace a document. Without an ID a new document is created with a generated ID.",
		Annotations: mutating("Index document", true, false),
	}, func(ctx context.Context, in indexDocumentInput) (any, error) {
		index, err := d.Policy.Concrete(in.Index)
		if err != nil {
			return nil, err
		}
		if in.Document == nil {
			return nil, errors.New("document must not be empty")
		}
		req := opensearch.Request{Method: http.MethodPost, Path: "/" + index + "/_doc", Query: refreshQuery(in.Refresh), Body: in.Document}
		if in.ID != "" {
			if req.Path, err = docPath(index, in.ID); err != nil {
				return nil, err
			}
			req.Method = http.MethodPut
		}
		return d.Backend.Do(ctx, req)
	})

	addTool(s, d, &mcp.Tool{
		Name:        "delete_document",
		Description: "Delete a single document by index and ID.",
		Annotations: mutating("Delete document", true, true),
	}, func(ctx context.Context, in deleteDocumentInput) (any, error) {
		index, err := d.Policy.Concrete(in.Index)
		if err != nil {
			return nil, err
		}
		path, err := docPath(index, in.ID)
		if err != nil {
			return nil, err
		}
		return d.Backend.Do(ctx, opensearch.Request{Method: http.MethodDelete, Path: path, Query: refreshQuery(in.Refresh)})
	})

	addTool(s, d, &mcp.Tool{
		Name:        "create_index",
		Description: "Create a new index with optional settings, mappings and aliases. Fails if the index already exists.",
		Annotations: mutating("Create index", false, false),
	}, func(ctx context.Context, in createIndexInput) (any, error) {
		index, err := d.Policy.Concrete(in.Index)
		if err != nil {
			return nil, err
		}
		var body any
		if in.Body != nil {
			body = in.Body
		}
		return d.Backend.Do(ctx, opensearch.Request{Method: http.MethodPut, Path: "/" + index, Body: body})
	})
}

// docPath builds /{index}/_doc/{id}, escaping the ID since it may contain
// any character, including '/'.
func docPath(index, id string) (string, error) {
	if id == "" {
		return "", errors.New("id must not be empty")
	}
	return "/" + index + "/_doc/" + url.PathEscape(id), nil
}

func refreshQuery(refresh bool) url.Values {
	if !refresh {
		return nil
	}
	return url.Values{"refresh": {"wait_for"}}
}
