package config

import (
	"errors"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func noFiles(string) ([]byte, error) { return nil, os.ErrNotExist }

func TestDefaults(t *testing.T) {
	cfg, err := load(env(nil), noFiles)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Addresses, []string{"http://localhost:9200"}) {
		t.Errorf("Addresses = %v", cfg.Addresses)
	}
	if !cfg.ReadOnly {
		t.Error("ReadOnly should default to true")
	}
	if cfg.Transport != TransportStdio {
		t.Errorf("Transport = %q", cfg.Transport)
	}
	if cfg.RequestTimeout != 30*time.Second || cfg.MaxSearchSize != 100 || cfg.LogLevel != slog.LevelInfo {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

func TestParsing(t *testing.T) {
	cfg, err := load(env(map[string]string{
		"OPENSEARCH_URL":             "https://a:9200, https://b:9200",
		"OPENSEARCH_READ_ONLY":       "false",
		"OPENSEARCH_ALLOWED_INDICES": "logs-*,metrics",
		"OPENSEARCH_TIMEOUT":         "5s",
		"MCP_TRANSPORT":              "HTTP",
		"LOG_LEVEL":                  "debug",
	}), noFiles)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Addresses, []string{"https://a:9200", "https://b:9200"}) {
		t.Errorf("Addresses = %v", cfg.Addresses)
	}
	if cfg.ReadOnly || cfg.Transport != TransportHTTP || cfg.RequestTimeout != 5*time.Second || cfg.LogLevel != slog.LevelDebug {
		t.Errorf("unexpected config: %+v", cfg)
	}
	if !slices.Equal(cfg.AllowedIndices, []string{"logs-*", "metrics"}) {
		t.Errorf("AllowedIndices = %v", cfg.AllowedIndices)
	}
}

func TestSecretFromFile(t *testing.T) {
	files := map[string]string{"/run/secrets/pw": "s3cret\n"}
	readFile := func(p string) ([]byte, error) {
		if v, ok := files[p]; ok {
			return []byte(v), nil
		}
		return nil, os.ErrNotExist
	}

	cfg, err := load(env(map[string]string{"OPENSEARCH_PASSWORD_FILE": "/run/secrets/pw"}), readFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Password != "s3cret" {
		t.Errorf("Password = %q", cfg.Password)
	}

	cfg, err = load(env(map[string]string{
		"OPENSEARCH_PASSWORD":      "direct",
		"OPENSEARCH_PASSWORD_FILE": "/run/secrets/pw",
	}), readFile)
	if err != nil || cfg.Password != "direct" {
		t.Errorf("direct value should win: %q, %v", cfg.Password, err)
	}

	_, err = load(env(map[string]string{"MCP_HTTP_AUTH_TOKEN_FILE": "/missing"}), readFile)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing secret file error = %v", err)
	}
}

func TestInvalidValuesReportedTogether(t *testing.T) {
	_, err := load(env(map[string]string{
		"OPENSEARCH_READ_ONLY":   "maybe",
		"OPENSEARCH_TIMEOUT":     "soon",
		"MCP_TRANSPORT":          "grpc",
		"OPENSEARCH_CLIENT_CERT": "/cert.pem",
	}), noFiles)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"OPENSEARCH_READ_ONLY", "OPENSEARCH_TIMEOUT", "MCP_TRANSPORT", "OPENSEARCH_CLIENT_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}
