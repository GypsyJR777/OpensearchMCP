// Package policy decides which index expressions a tool call may touch.
package policy

import (
	"fmt"
	"strings"
)

// Policy restricts access to indices. A zero Policy allows every index.
type Policy struct {
	allowed []string
}

// New builds a policy from allowlist patterns. Patterns support '*' only.
// An empty allowlist allows all indices.
func New(allowed []string) *Policy {
	return &Policy{allowed: allowed}
}

// Restricted reports whether an allowlist is configured.
func (p *Policy) Restricted() bool {
	return len(p.allowed) > 0
}

// Expression validates a comma-separated index expression supplied by the
// model (e.g. "logs-*, metrics"), verifies every part is permitted and returns
// it normalized for use in a URL path.
//
// A wildcard part is permitted only when the allowlist covers every index it
// could expand to: "logs-2024*" is covered by "logs-*", but "*" is not. This
// holds because the part's own '*' is matched as a literal character, and an
// allowlist '*' that absorbs it absorbs any expansion as well.
func (p *Policy) Expression(expr string) (string, error) {
	parts, err := splitExpression(expr)
	if err != nil {
		return "", err
	}
	for _, part := range parts {
		if !p.AllowsName(part) {
			return "", fmt.Errorf("access to index %q is not allowed by OPENSEARCH_ALLOWED_INDICES", part)
		}
	}
	return strings.Join(parts, ","), nil
}

// Syntax validates and normalizes an index expression without applying the
// allowlist. Listing tools use it and filter their results with AllowsName.
func Syntax(expr string) (string, error) {
	parts, err := splitExpression(expr)
	if err != nil {
		return "", err
	}
	return strings.Join(parts, ","), nil
}

// Concrete validates a single concrete index name, as required by write
// operations and document lookups.
func (p *Policy) Concrete(index string) (string, error) {
	if strings.ContainsAny(index, "*,") {
		return "", fmt.Errorf("index %q must be a single concrete index name without wildcards", index)
	}
	return p.Expression(index)
}

// AllowsName reports whether a concrete index or alias name returned by
// OpenSearch may be shown to the model.
func (p *Policy) AllowsName(name string) bool {
	if !p.Restricted() {
		return true
	}
	for _, pattern := range p.allowed {
		if matchGlob(pattern, name) {
			return true
		}
	}
	return false
}

// forbiddenChars are characters OpenSearch rejects in index names, plus
// characters that would alter the request path or enable date math.
const forbiddenChars = "\\/?\"<>| #{}:"

func splitExpression(expr string) ([]string, error) {
	if strings.TrimSpace(expr) == "" {
		return nil, fmt.Errorf("index must not be empty")
	}
	parts := strings.Split(expr, ",")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		switch {
		case part == "":
			return nil, fmt.Errorf("index expression %q contains an empty part", expr)
		case strings.HasPrefix(part, "-"):
			return nil, fmt.Errorf("index exclusions like %q are not supported", part)
		case strings.HasPrefix(part, "_"):
			return nil, fmt.Errorf("index %q is not allowed: names starting with '_' (such as _all) are reserved", part)
		case strings.ContainsAny(part, forbiddenChars):
			return nil, fmt.Errorf("index %q contains forbidden characters", part)
		}
		parts[i] = part
	}
	return parts, nil
}

// matchGlob matches s against pattern where '*' matches any (possibly empty)
// sequence of characters and every other character matches itself.
func matchGlob(pattern, s string) bool {
	p, i := 0, 0
	star, mark := -1, 0
	for i < len(s) {
		switch {
		case p < len(pattern) && pattern[p] == '*':
			star, mark = p, i
			p++
		case p < len(pattern) && pattern[p] == s[i]:
			p++
			i++
		case star >= 0:
			p = star + 1
			mark++
			i = mark
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}
