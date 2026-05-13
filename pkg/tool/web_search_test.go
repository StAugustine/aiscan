package tool

import (
	"context"
	"strings"
	"testing"
)

func TestWebSearchToolDefinition(t *testing.T) {
	ws := NewWebSearchTool()
	if ws.Name() != "web_search" {
		t.Fatalf("expected name web_search, got %s", ws.Name())
	}
	def := ws.Definition()
	if def.Function.Name != "web_search" {
		t.Fatalf("expected function name web_search, got %s", def.Function.Name)
	}
	params, ok := def.Function.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in parameters")
	}
	if _, ok := params["query"]; !ok {
		t.Fatal("expected query in properties")
	}
	if _, ok := params["num_results"]; !ok {
		t.Fatal("expected num_results in properties")
	}
}

func TestWebSearchToolEmptyQuery(t *testing.T) {
	ws := NewWebSearchTool()
	_, err := ws.Execute(context.Background(), `{"query":""}`)
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	if !strings.Contains(err.Error(), "query is required") {
		t.Errorf("expected 'query is required' error, got: %s", err.Error())
	}
}

func TestWebSearchToolInvalidJSON(t *testing.T) {
	ws := NewWebSearchTool()
	_, err := ws.Execute(context.Background(), `not json`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// DuckDuckGo HTML parsing tests
// ---------------------------------------------------------------------------

const sampleDDGHTML = `
<html>
<body>
<div class="result results_links results_links_deep web-result">
  <div class="links_main links_deep result__body">
    <h2 class="result__title">
      <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fnvd.nist.gov%2Fvuln%2Fdetail%2FCVE-2025-58434&rut=abc">CVE-2025-58434 Detail - NVD</a>
    </h2>
    <a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fnvd.nist.gov%2Fvuln%2Fdetail%2FCVE-2025-58434">Critical RCE in Flowise v3.0.5 allows remote attackers to execute code</a>
  </div>
</div>
<div class="result results_links results_links_deep web-result">
  <div class="links_main links_deep result__body">
    <h2 class="result__title">
      <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgithub.com%2FFlowiseAI%2FFlowise%2Fsecurity&rut=def">Flowise Security Advisories &amp; GitHub</a>
    </h2>
    <a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgithub.com%2FFlowiseAI%2FFlowise%2Fsecurity">Security advisory for versions prior to 3.1.0 &mdash; upgrade recommended</a>
  </div>
</div>
</body>
</html>
`

func TestParseDDGHTML(t *testing.T) {
	result, err := parseDDGHTML(sampleDDGHTML, "Flowise CVE", 10)
	if err != nil {
		t.Fatalf("parseDDGHTML error: %v", err)
	}
	if !strings.Contains(result, "CVE-2025-58434") {
		t.Error("expected CVE-2025-58434 in results")
	}
	if !strings.Contains(result, "nvd.nist.gov") {
		t.Error("expected resolved nvd.nist.gov URL")
	}
	if !strings.Contains(result, "Flowise Security Advisories & GitHub") {
		t.Error("expected decoded &amp; entity in title")
	}
	if !strings.Contains(result, "upgrade recommended") {
		t.Error("expected snippet text in results")
	}
}

func TestParseDDGHTMLLimit(t *testing.T) {
	result, err := parseDDGHTML(sampleDDGHTML, "test", 1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result, "[2]") {
		t.Error("expected only 1 result when limit=1")
	}
	if !strings.Contains(result, "[1]") {
		t.Error("expected result [1]")
	}
}

func TestParseDDGHTMLNoResults(t *testing.T) {
	result, err := parseDDGHTML("<html><body>No results</body></html>", "query", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "No results found") {
		t.Error("expected 'No results found' message")
	}
}

func TestResolveDDGURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"uddg wrapped", "//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fpage&rut=abc", "https://example.com/page"},
		{"direct https", "https://example.com", "https://example.com"},
		{"direct http", "http://example.com", "http://example.com"},
		{"protocol-relative", "//example.com/path", "https://example.com/path"},
		{"empty", "", ""},
		{"relative path", "/some/path", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveDDGURL(tt.raw)
			if got != tt.want {
				t.Errorf("resolveDDGURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestCleanHTML(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"<b>bold</b> text", "bold text"},
		{"a &amp; b", "a & b"},
		{"foo  &nbsp;  bar", "foo bar"},
		{"<span class='x'>hello &mdash; world</span>", "hello — world"},
		{"", ""},
	}
	for _, tt := range tests {
		got := cleanHTML(tt.input)
		if got != tt.want {
			t.Errorf("cleanHTML(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDecodeHTMLEntities(t *testing.T) {
	input := "&lt;script&gt;alert(&#39;xss&#39;)&lt;/script&gt;"
	got := decodeHTMLEntities(input)
	if !strings.Contains(got, "<script>") {
		t.Errorf("expected decoded entities, got %q", got)
	}
}
