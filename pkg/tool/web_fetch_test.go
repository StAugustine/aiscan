package tool

import (
	"context"
	"strings"
	"testing"
)

func TestWebFetchToolDefinition(t *testing.T) {
	wf := NewWebFetchTool()
	if wf.Name() != "web_fetch" {
		t.Fatalf("expected name web_fetch, got %s", wf.Name())
	}
	def := wf.Definition()
	if def.Function.Name != "web_fetch" {
		t.Fatalf("expected function name web_fetch, got %s", def.Function.Name)
	}
	params, ok := def.Function.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in parameters")
	}
	if _, ok := params["url"]; !ok {
		t.Fatal("expected url in properties")
	}
	if _, ok := params["extract"]; !ok {
		t.Fatal("expected extract in properties")
	}
}

func TestWebFetchToolEmptyURL(t *testing.T) {
	wf := NewWebFetchTool()
	_, err := wf.Execute(context.Background(), `{"url":""}`)
	if err == nil {
		t.Fatal("expected error for empty url")
	}
	if !strings.Contains(err.Error(), "url is required") {
		t.Errorf("expected 'url is required' error, got: %s", err.Error())
	}
}

func TestWebFetchToolInvalidJSON(t *testing.T) {
	wf := NewWebFetchTool()
	_, err := wf.Execute(context.Background(), `not json`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestWebFetchToolNoHost(t *testing.T) {
	wf := NewWebFetchTool()
	_, err := wf.Execute(context.Background(), `{"url":"https://"}`)
	if err == nil {
		t.Fatal("expected error for URL without host")
	}
}

// ---------------------------------------------------------------------------
// HTML → Markdown conversion tests
// ---------------------------------------------------------------------------

func TestHTMLToMarkdownHeadings(t *testing.T) {
	html := `<h1>Title</h1><h2>Subtitle</h2><h3>Section</h3>`
	md := htmlToMarkdown(html)
	if !strings.Contains(md, "# Title") {
		t.Errorf("expected '# Title', got: %s", md)
	}
	if !strings.Contains(md, "## Subtitle") {
		t.Errorf("expected '## Subtitle', got: %s", md)
	}
	if !strings.Contains(md, "### Section") {
		t.Errorf("expected '### Section', got: %s", md)
	}
}

func TestHTMLToMarkdownLinks(t *testing.T) {
	html := `<a href="https://example.com">click here</a>`
	md := htmlToMarkdown(html)
	if !strings.Contains(md, "[click here](https://example.com)") {
		t.Errorf("expected markdown link, got: %s", md)
	}
}

func TestHTMLToMarkdownBoldItalic(t *testing.T) {
	html := `<b>bold</b> and <em>italic</em> and <strong>also bold</strong>`
	md := htmlToMarkdown(html)
	if !strings.Contains(md, "**bold**") {
		t.Errorf("expected **bold**, got: %s", md)
	}
	if !strings.Contains(md, "*italic*") {
		t.Errorf("expected *italic*, got: %s", md)
	}
	if !strings.Contains(md, "**also bold**") {
		t.Errorf("expected **also bold**, got: %s", md)
	}
}

func TestHTMLToMarkdownCodeBlock(t *testing.T) {
	html := `<pre><code>func main() {
	fmt.Println("hello")
}</code></pre>`
	md := htmlToMarkdown(html)
	if !strings.Contains(md, "```") {
		t.Errorf("expected code block markers, got: %s", md)
	}
	if !strings.Contains(md, "func main()") {
		t.Errorf("expected code content, got: %s", md)
	}
}

func TestHTMLToMarkdownStripScript(t *testing.T) {
	html := `<p>Hello</p><script>alert('xss')</script><p>World</p>`
	md := htmlToMarkdown(html)
	if strings.Contains(md, "alert") {
		t.Errorf("script content should be stripped, got: %s", md)
	}
	if !strings.Contains(md, "Hello") || !strings.Contains(md, "World") {
		t.Errorf("expected paragraph content preserved, got: %s", md)
	}
}

func TestHTMLToMarkdownStripStyle(t *testing.T) {
	html := `<style>body { color: red; }</style><p>Content</p>`
	md := htmlToMarkdown(html)
	if strings.Contains(md, "color") {
		t.Errorf("style content should be stripped, got: %s", md)
	}
	if !strings.Contains(md, "Content") {
		t.Errorf("expected content preserved, got: %s", md)
	}
}

func TestHTMLToMarkdownList(t *testing.T) {
	html := `<ul><li>First</li><li>Second</li><li>Third</li></ul>`
	md := htmlToMarkdown(html)
	if !strings.Contains(md, "- First") {
		t.Errorf("expected list items, got: %s", md)
	}
	if !strings.Contains(md, "- Second") {
		t.Errorf("expected list items, got: %s", md)
	}
}

func TestHTMLToMarkdownEntities(t *testing.T) {
	html := `<p>A &amp; B &lt; C &gt; D</p>`
	md := htmlToMarkdown(html)
	if !strings.Contains(md, "A & B < C > D") {
		t.Errorf("expected decoded entities, got: %s", md)
	}
}

func TestHTMLToMarkdownEmpty(t *testing.T) {
	md := htmlToMarkdown("")
	if md != "" {
		t.Errorf("expected empty output for empty input, got: %q", md)
	}
}

func TestHTMLToMarkdownTable(t *testing.T) {
	html := `<table><tr><th>Name</th><th>Value</th></tr><tr><td>CVE</td><td>2024-3400</td></tr></table>`
	md := htmlToMarkdown(html)
	if !strings.Contains(md, "| Name | Value |") {
		t.Errorf("expected table header, got: %s", md)
	}
	if !strings.Contains(md, "| CVE | 2024-3400 |") {
		t.Errorf("expected table row, got: %s", md)
	}
}
