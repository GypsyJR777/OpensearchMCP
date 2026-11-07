package policy

import "testing"

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern, s string
		want       bool
	}{
		{"*", "", true},
		{"*", "anything", true},
		{"logs-*", "logs-2024", true},
		{"logs-*", "logs-", true},
		{"logs-*", "metrics", false},
		{"logs-*-prod", "logs-a-b-prod", true},
		{"logs-*-prod", "logs-a-b-dev", false},
		{"a*b*c", "axxbyyc", true},
		{"a*b*c", "axxbyy", false},
		{"exact", "exact", true},
		{"exact", "exactly", false},
	}
	for _, tt := range tests {
		if got := matchGlob(tt.pattern, tt.s); got != tt.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.s, got, tt.want)
		}
	}
}

func TestExpression(t *testing.T) {
	restricted := New([]string{"logs-*", "metrics"})
	open := New(nil)

	tests := []struct {
		name    string
		p       *Policy
		expr    string
		want    string
		wantErr bool
	}{
		{name: "open allows wildcard", p: open, expr: "*", want: "*"},
		{name: "open normalizes spaces", p: open, expr: " a , b ", want: "a,b"},
		{name: "allowed concrete", p: restricted, expr: "metrics", want: "metrics"},
		{name: "allowed by pattern", p: restricted, expr: "logs-2024", want: "logs-2024"},
		{name: "narrower wildcard covered", p: restricted, expr: "logs-2024*", want: "logs-2024*"},
		{name: "same wildcard covered", p: restricted, expr: "logs-*", want: "logs-*"},
		{name: "broader wildcard denied", p: restricted, expr: "*", wantErr: true},
		{name: "partially allowed list denied", p: restricted, expr: "logs-a,secret", wantErr: true},
		{name: "prefix wildcard denied", p: restricted, expr: "log*", wantErr: true},
		{name: "empty", p: open, expr: "  ", wantErr: true},
		{name: "empty part", p: open, expr: "a,,b", wantErr: true},
		{name: "_all reserved", p: open, expr: "_all", wantErr: true},
		{name: "exclusion", p: open, expr: "-secret", wantErr: true},
		{name: "path traversal", p: open, expr: "a/../_cluster", wantErr: true},
		{name: "query injection", p: open, expr: "a?pretty", wantErr: true},
		{name: "date math", p: open, expr: "<logs-{now/d}>", wantErr: true},
		{name: "cross cluster", p: open, expr: "remote:logs", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.p.Expression(tt.expr)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Expression(%q) error = %v, wantErr %v", tt.expr, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("Expression(%q) = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}
}

func TestConcrete(t *testing.T) {
	p := New(nil)
	for _, bad := range []string{"logs-*", "a,b"} {
		if _, err := p.Concrete(bad); err == nil {
			t.Errorf("Concrete(%q) succeeded, want error", bad)
		}
	}
	if got, err := p.Concrete("logs"); err != nil || got != "logs" {
		t.Errorf("Concrete(logs) = %q, %v", got, err)
	}
}

func TestSyntaxIgnoresAllowlist(t *testing.T) {
	if got, err := Syntax("*"); err != nil || got != "*" {
		t.Errorf("Syntax(*) = %q, %v", got, err)
	}
	if _, err := Syntax("a/b"); err == nil {
		t.Error("Syntax(a/b) succeeded, want error")
	}
}
