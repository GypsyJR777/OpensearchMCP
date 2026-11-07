// Command opensearch-mcp is a Model Context Protocol server for OpenSearch.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/GypsyJR777/OpensearchMCP/internal/config"
	"github.com/GypsyJR777/OpensearchMCP/internal/opensearch"
	"github.com/GypsyJR777/OpensearchMCP/internal/policy"
	"github.com/GypsyJR777/OpensearchMCP/internal/server"
	"github.com/GypsyJR777/OpensearchMCP/internal/tools"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println(version)
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "opensearch-mcp:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("invalid configuration:\n%w", err)
	}

	// Logs always go to stderr: in stdio mode stdout carries the protocol.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))

	client, err := opensearch.New(cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	s := server.New(version, &tools.Deps{
		Backend:       client,
		Policy:        policy.New(cfg.AllowedIndices),
		ReadOnly:      cfg.ReadOnly,
		MaxSearchSize: cfg.MaxSearchSize,
		Logger:        logger,
	})

	logger.Info("starting opensearch-mcp",
		"version", version,
		"opensearch", cfg.Addresses,
		"transport", cfg.Transport,
		"read_only", cfg.ReadOnly,
		"allowed_indices", cfg.AllowedIndices,
	)
	return server.Run(ctx, cfg, s, client, logger)
}
