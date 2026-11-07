package opensearch

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/GypsyJR777/OpensearchMCP/internal/config"
)

func newTestClient(t *testing.T, h http.HandlerFunc, maxBytes int64) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(&config.Config{
		Addresses:        []string{srv.URL},
		Username:         "user",
		Password:         "pass",
		RequestTimeout:   5 * time.Second,
		MaxResponseBytes: maxBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestDoSendsRequest(t *testing.T) {
	var got struct {
		method, rawPath, query, auth, contentType string
		body                                      map[string]any
	}
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got.method = r.Method
		got.rawPath = r.URL.EscapedPath()
		got.query = r.URL.RawQuery
		got.auth = r.Header.Get("Authorization")
		got.contentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}, 1024)

	out, err := c.Do(t.Context(), Request{
		Method: http.MethodPost,
		Path:   "/logs/_doc/a%2Fb",
		Query:  url.Values{"refresh": {"wait_for"}},
		Body:   map[string]any{"msg": "hi"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"ok":true}` {
		t.Errorf("out = %s", out)
	}
	if got.method != "POST" || got.rawPath != "/logs/_doc/a%2Fb" || got.query != "refresh=wait_for" {
		t.Errorf("request = %s %s?%s", got.method, got.rawPath, got.query)
	}
	if !strings.HasPrefix(got.auth, "Basic ") || got.contentType != "application/json" || got.body["msg"] != "hi" {
		t.Errorf("auth=%q content-type=%q body=%v", got.auth, got.contentType, got.body)
	}
}

func TestDoCompactsJSON(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "{\n  \"name\" : \"node-1\"\n}\n")
	}, 1024)

	out, err := c.Do(t.Context(), Request{Method: http.MethodGet, Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"name":"node-1"}` {
		t.Errorf("out = %s", out)
	}
}

func TestDoStatusError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"type":"index_not_found_exception"},"status":404}`)
	}, 1024)

	_, err := c.Do(t.Context(), Request{Method: http.MethodGet, Path: "/missing"})
	var se *StatusError
	if !errors.As(err, &se) || se.StatusCode != 404 || !strings.Contains(se.Body, "index_not_found_exception") {
		t.Errorf("err = %v", err)
	}
}

func TestDoResponseLimit(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":"`+strings.Repeat("x", 100)+`"}`)
	}, 50)

	if _, err := c.Do(t.Context(), Request{Method: http.MethodGet, Path: "/"}); !errors.Is(err, ErrResponseTooLarge) {
		t.Errorf("err = %v, want ErrResponseTooLarge", err)
	}
}
