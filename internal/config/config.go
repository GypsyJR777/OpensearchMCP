// Package config loads server configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Transport selects how the MCP server talks to its client.
type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportHTTP  Transport = "http"
)

// Config is the full runtime configuration.
type Config struct {
	// OpenSearch connection.
	Addresses          []string
	Username           string
	Password           string
	CACertFile         string
	ClientCertFile     string
	ClientKeyFile      string
	InsecureSkipVerify bool
	RequestTimeout     time.Duration
	MaxRetries         int

	// Safety limits.
	ReadOnly         bool
	AllowedIndices   []string
	MaxResponseBytes int64
	MaxSearchSize    int

	// MCP transport.
	Transport     Transport
	HTTPAddr      string
	HTTPAuthToken string

	LogLevel slog.Level
}

// Load reads the configuration from the process environment.
func Load() (*Config, error) {
	return load(os.Getenv, os.ReadFile)
}

func load(getenv func(string) string, readFile func(string) ([]byte, error)) (*Config, error) {
	e := envReader{getenv: getenv, readFile: readFile}

	cfg := &Config{
		Addresses:          e.list("OPENSEARCH_URL", []string{"http://localhost:9200"}),
		Username:           e.str("OPENSEARCH_USERNAME", ""),
		Password:           e.secret("OPENSEARCH_PASSWORD"),
		CACertFile:         e.str("OPENSEARCH_CA_CERT", ""),
		ClientCertFile:     e.str("OPENSEARCH_CLIENT_CERT", ""),
		ClientKeyFile:      e.str("OPENSEARCH_CLIENT_KEY", ""),
		InsecureSkipVerify: e.bool("OPENSEARCH_INSECURE_SKIP_VERIFY", false),
		RequestTimeout:     e.duration("OPENSEARCH_TIMEOUT", 30*time.Second),
		MaxRetries:         e.int("OPENSEARCH_MAX_RETRIES", 3),

		ReadOnly:         e.bool("OPENSEARCH_READ_ONLY", true),
		AllowedIndices:   e.list("OPENSEARCH_ALLOWED_INDICES", nil),
		MaxResponseBytes: int64(e.int("OPENSEARCH_MAX_RESPONSE_BYTES", 512*1024)),
		MaxSearchSize:    e.int("OPENSEARCH_MAX_SEARCH_SIZE", 100),

		Transport:     Transport(strings.ToLower(e.str("MCP_TRANSPORT", string(TransportStdio)))),
		HTTPAddr:      e.str("MCP_HTTP_ADDR", ":8080"),
		HTTPAuthToken: e.secret("MCP_HTTP_AUTH_TOKEN"),

		LogLevel: e.level("LOG_LEVEL", slog.LevelInfo),
	}

	if err := errors.Join(append(e.errs, cfg.validate())...); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	var errs []error
	if len(c.Addresses) == 0 {
		errs = append(errs, errors.New("OPENSEARCH_URL must not be empty"))
	}
	if c.Transport != TransportStdio && c.Transport != TransportHTTP {
		errs = append(errs, fmt.Errorf("MCP_TRANSPORT must be %q or %q, got %q", TransportStdio, TransportHTTP, c.Transport))
	}
	if (c.ClientCertFile == "") != (c.ClientKeyFile == "") {
		errs = append(errs, errors.New("OPENSEARCH_CLIENT_CERT and OPENSEARCH_CLIENT_KEY must be set together"))
	}
	if c.MaxResponseBytes <= 0 {
		errs = append(errs, errors.New("OPENSEARCH_MAX_RESPONSE_BYTES must be positive"))
	}
	if c.MaxSearchSize <= 0 {
		errs = append(errs, errors.New("OPENSEARCH_MAX_SEARCH_SIZE must be positive"))
	}
	if c.RequestTimeout <= 0 {
		errs = append(errs, errors.New("OPENSEARCH_TIMEOUT must be positive"))
	}
	return errors.Join(errs...)
}

// envReader parses typed values and accumulates errors so that all
// misconfigurations are reported at once.
type envReader struct {
	getenv   func(string) string
	readFile func(string) ([]byte, error)
	errs     []error
}

// secret reads KEY, falling back to the contents of the file named by
// KEY_FILE (the Docker/Kubernetes secrets convention).
func (e *envReader) secret(key string) string {
	if v := e.str(key, ""); v != "" {
		return v
	}
	path := e.str(key+"_FILE", "")
	if path == "" {
		return ""
	}
	b, err := e.readFile(path)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s_FILE: %w", key, err))
		return ""
	}
	return strings.TrimSpace(string(b))
}

func (e *envReader) str(key, def string) string {
	if v := strings.TrimSpace(e.getenv(key)); v != "" {
		return v
	}
	return def
}

func (e *envReader) list(key string, def []string) []string {
	v := e.str(key, "")
	if v == "" {
		return def
	}
	var out []string
	for _, item := range strings.Split(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func (e *envReader) bool(key string, def bool) bool {
	v := e.str(key, "")
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s: invalid boolean %q", key, v))
		return def
	}
	return b
}

func (e *envReader) int(key string, def int) int {
	v := e.str(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s: invalid integer %q", key, v))
		return def
	}
	return n
}

func (e *envReader) duration(key string, def time.Duration) time.Duration {
	v := e.str(key, "")
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s: invalid duration %q (use e.g. 30s, 1m)", key, v))
		return def
	}
	return d
}

func (e *envReader) level(key string, def slog.Level) slog.Level {
	v := e.str(key, "")
	if v == "" {
		return def
	}
	var l slog.Level
	if err := l.UnmarshalText([]byte(v)); err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s: invalid log level %q (use debug, info, warn, error)", key, v))
		return def
	}
	return l
}
