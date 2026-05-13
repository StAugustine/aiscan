package tool

import (
	"context"
	"strings"
	"testing"
)

func TestCyberhubSearchToolDefinition(t *testing.T) {
	tool := NewCyberhubSearchTool(nil)
	if tool.Name() != "cyberhub_search" {
		t.Fatalf("expected name cyberhub_search, got %s", tool.Name())
	}
	def := tool.Definition()
	if def.Function.Name != "cyberhub_search" {
		t.Fatalf("expected function name cyberhub_search, got %s", def.Function.Name)
	}
	params, ok := def.Function.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in parameters")
	}
	for _, field := range []string{"query", "resource_type", "severity", "tags", "limit"} {
		if _, ok := params[field]; !ok {
			t.Errorf("expected %s in properties", field)
		}
	}
}

func TestCyberhubSearchToolEmptyQuery(t *testing.T) {
	tool := NewCyberhubSearchTool(nil)
	_, err := tool.Execute(context.Background(), `{"query":""}`)
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	if !strings.Contains(err.Error(), "query is required") {
		t.Errorf("expected 'query is required' error, got: %s", err.Error())
	}
}

func TestCyberhubSearchToolInvalidJSON(t *testing.T) {
	tool := NewCyberhubSearchTool(nil)
	_, err := tool.Execute(context.Background(), `not json`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestBuildCyberhubArgs(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		resourceType string
		severity     string
		tags         string
		limit        int
		wantContains []string
	}{
		{
			name:         "basic query",
			query:        "spring",
			resourceType: "",
			severity:     "",
			tags:         "",
			limit:        0,
			wantContains: []string{"search", "spring", "--limit", "20", "--json"},
		},
		{
			name:         "poc type with severity",
			query:        "CVE-2020",
			resourceType: "poc",
			severity:     "critical,high",
			tags:         "",
			limit:        10,
			wantContains: []string{"search", "poc", "CVE-2020", "--severity", "critical,high", "--limit", "10", "--json"},
		},
		{
			name:         "finger type with tags",
			query:        "nginx",
			resourceType: "finger",
			severity:     "",
			tags:         "web,proxy",
			limit:        5,
			wantContains: []string{"search", "finger", "nginx", "--tag", "web,proxy", "--limit", "5", "--json"},
		},
		{
			name:         "all filters",
			query:        "weblogic",
			resourceType: "poc",
			severity:     "critical",
			tags:         "rce",
			limit:        50,
			wantContains: []string{"search", "poc", "weblogic", "--severity", "critical", "--tag", "rce", "--limit", "50", "--json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := buildCyberhubArgs(tt.query, tt.resourceType, tt.severity, tt.tags, tt.limit)
			argsStr := strings.Join(args, " ")
			for _, want := range tt.wantContains {
				if !strings.Contains(argsStr, want) {
					t.Errorf("expected args to contain %q, got: %v", want, args)
				}
			}
		})
	}
}

func TestCyberhubSearchToolNilResources(t *testing.T) {
	// With nil resources, cyberhub.Command should handle gracefully
	tool := NewCyberhubSearchTool(nil)
	result, err := tool.Execute(context.Background(), `{"query":"spring"}`)
	// Should not panic — may return empty results or error depending on cyberhub impl
	if err != nil {
		t.Logf("Expected: nil resources returns error or empty: %v", err)
	} else {
		t.Logf("Result with nil resources: %s", result)
	}
}
