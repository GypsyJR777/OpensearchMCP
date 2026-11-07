// Package server assembles the MCP server and runs it over the configured
// transport.
package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GypsyJR777/OpensearchMCP/internal/config"
	"github.com/GypsyJR777/OpensearchMCP/internal/opensearch"
	"github.com/GypsyJR777/OpensearchMCP/internal/tools"
)

const instructions = `Tools for exploring and querying an OpenSearch cluster.
Typical workflow: list_indices to discover data, get_mapping to learn field names and types,
then search or count. Prefer aggregations over fetching many documents, and use _source
filtering to keep responses small.`

const shutdownTimeout = 10 * time.Second

// New builds an MCP server with all tools registered.
func New(version string, deps *tools.Deps) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "opensearch-mcp",
		Title:   "OpenSearch MCP",
		Version: version,
	}, &mcp.ServerOptions{
		Instructions: instructions,
		Logger:       deps.Logger,
	})
	tools.Register(s, deps)
	return s
}

// Run serves s over the transport selected in cfg until ctx is cancelled.
func Run(ctx context.Context, cfg *config.Config, s *mcp.Server, backend tools.Backend, logger *slog.Logger) error {
	switch cfg.Transport {
	case config.TransportStdio:
		logger.Info("serving MCP over stdio")
		err := s.Run(ctx, &mcp.StdioTransport{})
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	case config.TransportHTTP:
		return runHTTP(ctx, cfg, s, backend, logger)
	default:
		return fmt.Errorf("unsupported transport %q", cfg.Transport)
	}
}

func runHTTP(ctx context.Context, cfg *config.Config, s *mcp.Server, backend tools.Backend, logger *slog.Logger) error {
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           Handler(cfg, s, backend, logger),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("serving MCP over streamable HTTP", "addr", cfg.HTTPAddr, "endpoint", "/mcp", "auth", cfg.HTTPAuthToken != "")
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down HTTP server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down: %w", err)
	}
	return nil
}

// Handler returns the HTTP handler: /mcp for the protocol, /healthz for
// liveness and /readyz for readiness (OpenSearch reachable).
func Handler(cfg *config.Config, s *mcp.Server, backend tools.Backend, logger *slog.Logger) http.Handler {
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, &mcp.StreamableHTTPOptions{
		// Tools never call back into the client, so sessions are unnecessary
		// and any replica can serve any request.
		Stateless: true,
		Logger:    logger,
	})

	mux := http.NewServeMux()
	mux.Handle("/mcp", requireToken(cfg.HTTPAuthToken, mcpHandler))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if _, err := backend.Do(ctx, opensearch.Request{Method: http.MethodGet, Path: "/"}); err != nil {
			logger.Warn("readiness check failed", "error", err)
			http.Error(w, "opensearch unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}

// requireToken enforces "Authorization: Bearer <token>" when token is set.
func requireToken(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	want := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="opensearch-mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
