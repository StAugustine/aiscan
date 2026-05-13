package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/chainreactors/aiscan/pkg/provider"
)

const (
	ddgSearchURL    = "https://html.duckduckgo.com/html/"
	ddgUserAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	searchTimeout   = 30 * time.Second
	maxResponseBody = 1024 * 1024 // 1MB for HTML

	defaultNumResults = 5
	maxNumResults     = 10

	tavilySearchURL = "https://api.tavily.com/search"
)

// searchBackend identifies which search provider to use.
type searchBackend int

const (
	backendDDG    searchBackend = iota
	backendTavily               // requires TAVILY_API_KEY
)

// WebSearchTool performs web searches. When TAVILY_API_KEY is set it uses
// the Tavily API (structured JSON, designed for AI agents, free 1000/month).
// Otherwise it falls back to DuckDuckGo HTML scraping.
type WebSearchTool struct {
	client  *http.Client
	backend searchBackend
	apiKey  string
}

// NewWebSearchTool creates a web search tool. It auto-detects the backend:
// TAVILY_API_KEY set → Tavily, otherwise → DuckDuckGo.
func NewWebSearchTool() *WebSearchTool {
	t := &WebSearchTool{
		client: &http.Client{Timeout: searchTimeout},
	}
	if key := os.Getenv("TAVILY_API_KEY"); key != "" {
		t.backend = backendTavily
		t.apiKey = key
	}
	return t
}

func (t *WebSearchTool) Name() string { return "web_search" }

func (t *WebSearchTool) Description() string {
	return "Search the web for information. Use for CVE lookups, vulnerability research, exploit details, technology documentation, and any query requiring up-to-date internet data."
}

func (t *WebSearchTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionDefinition{
			Name:        "web_search",
			Description: t.Description(),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query string",
					},
					"num_results": map[string]any{
						"type":        "integer",
						"description": "Number of results to return (1-10, default 5)",
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, arguments string) (string, error) {
	var args struct {
		Query      string `json:"query"`
		NumResults int    `json:"num_results"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	num := args.NumResults
	if num <= 0 {
		num = defaultNumResults
	}
	if num > maxNumResults {
		num = maxNumResults
	}

	switch t.backend {
	case backendTavily:
		result, err := t.searchTavily(ctx, query, num)
		if err != nil {
			// Tavily failed, try DDG as fallback
			fallback, fbErr := t.searchDDG(ctx, query, num)
			if fbErr != nil {
				return "", fmt.Errorf("tavily: %w; ddg fallback: %w", err, fbErr)
			}
			return fallback, nil
		}
		return result, nil
	default:
		return t.searchDDG(ctx, query, num)
	}
}

// ---------------------------------------------------------------------------
// Tavily API backend (primary when API key is available)
// ---------------------------------------------------------------------------

// tavilyRequest is the request body for the Tavily Search API.
type tavilyRequest struct {
	Query                string `json:"query"`
	MaxResults           int    `json:"max_results"`
	SearchDepth          string `json:"search_depth"`
	IncludeAnswer        bool   `json:"include_answer"`
	IncludeRawContent    bool   `json:"include_raw_content"`
	IncludeImageDescriptions bool `json:"include_image_descriptions"`
}

// tavilyResponse is the response from the Tavily Search API.
type tavilyResponse struct {
	Answer  string         `json:"answer"`
	Results []tavilyResult `json:"results"`
	Query   string         `json:"query"`
}

type tavilyResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

func (t *WebSearchTool) searchTavily(ctx context.Context, query string, num int) (string, error) {
	reqBody := tavilyRequest{
		Query:             query,
		MaxResults:        num,
		SearchDepth:       "basic",
		IncludeAnswer:     true,
		IncludeRawContent: false,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tavilySearchURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.apiKey)

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("tavily request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return "", fmt.Errorf("read tavily response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tavily returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tavilyResp tavilyResponse
	if err := json.Unmarshal(body, &tavilyResp); err != nil {
		return "", fmt.Errorf("parse tavily response: %w", err)
	}

	return formatTavilyResults(tavilyResp, query), nil
}

func formatTavilyResults(resp tavilyResponse, query string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Web search results for: %s\n\n", query))

	if resp.Answer != "" {
		sb.WriteString(fmt.Sprintf("Summary: %s\n\n", resp.Answer))
	}

	if len(resp.Results) == 0 {
		sb.WriteString("No results found.\n")
		return sb.String()
	}

	for i, r := range resp.Results {
		sb.WriteString(fmt.Sprintf("[%d] %s\n", i+1, r.Title))
		sb.WriteString(fmt.Sprintf("    URL: %s\n", r.URL))
		if r.Content != "" {
			// Truncate long snippets
			snippet := r.Content
			if len(snippet) > 300 {
				snippet = snippet[:300] + "..."
			}
			sb.WriteString(fmt.Sprintf("    %s\n", snippet))
		}
		sb.WriteByte('\n')
	}

	return sb.String()
}

// ---------------------------------------------------------------------------
// DuckDuckGo HTML backend (fallback, no API key)
// ---------------------------------------------------------------------------

func (t *WebSearchTool) searchDDG(ctx context.Context, query string, num int) (string, error) {
	form := url.Values{"q": {query}, "b": {""}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ddgSearchURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", ddgUserAgent)

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("DuckDuckGo request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("DuckDuckGo returned HTTP %d", resp.StatusCode)
	}

	return parseDDGHTML(string(body), query, num)
}

// ddgResult holds a single DuckDuckGo search result.
type ddgResult struct {
	Title   string
	URL     string
	Snippet string
}

// Regex patterns for DuckDuckGo HTML parsing.
var (
	ddgResultBlockRe = regexp.MustCompile(`(?s)<div[^>]*class="[^"]*result[^"]*links_main[^"]*"[^>]*>.*?</div>\s*</div>`)
	ddgLinkRe        = regexp.MustCompile(`(?s)<a[^>]*class="result__a"[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
	ddgSnippetRe     = regexp.MustCompile(`(?s)<a[^>]*class="result__snippet"[^>]*>(.*?)</a>`)
	htmlTagRe        = regexp.MustCompile(`<[^>]*>`)
	htmlEntityRe     = regexp.MustCompile(`&[a-zA-Z]+;|&#\d+;`)
)

func parseDDGHTML(html, query string, num int) (string, error) {
	results := extractDDGResults(html, num)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Web search results for: %s\n\n", query))

	if len(results) == 0 {
		sb.WriteString("No results found.\n")
		return sb.String(), nil
	}

	for i, r := range results {
		sb.WriteString(fmt.Sprintf("[%d] %s\n", i+1, r.Title))
		sb.WriteString(fmt.Sprintf("    URL: %s\n", r.URL))
		if r.Snippet != "" {
			sb.WriteString(fmt.Sprintf("    %s\n", r.Snippet))
		}
		sb.WriteByte('\n')
	}

	return sb.String(), nil
}

func extractDDGResults(html string, max int) []ddgResult {
	linkMatches := ddgLinkRe.FindAllStringSubmatchIndex(html, -1)
	snippetMatches := ddgSnippetRe.FindAllStringSubmatch(html, -1)

	var results []ddgResult
	for i, loc := range linkMatches {
		if len(results) >= max {
			break
		}
		if len(loc) < 6 {
			continue
		}
		rawURL := html[loc[2]:loc[3]]
		rawTitle := html[loc[4]:loc[5]]

		resolvedURL := resolveDDGURL(rawURL)
		if resolvedURL == "" {
			continue
		}
		title := cleanHTML(rawTitle)
		if title == "" {
			continue
		}

		var snippet string
		if i < len(snippetMatches) && len(snippetMatches[i]) > 1 {
			snippet = cleanHTML(snippetMatches[i][1])
		}

		results = append(results, ddgResult{
			Title:   title,
			URL:     resolvedURL,
			Snippet: snippet,
		})
	}
	return results
}

// resolveDDGURL extracts the actual URL from DuckDuckGo's redirect wrapper.
func resolveDDGURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "duckduckgo.com/l/") {
		parsed, err := url.Parse(raw)
		if err == nil {
			if uddg := parsed.Query().Get("uddg"); uddg != "" {
				return uddg
			}
		}
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	return ""
}

func cleanHTML(s string) string {
	s = htmlTagRe.ReplaceAllString(s, "")
	s = decodeHTMLEntities(s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

func decodeHTMLEntities(s string) string {
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
		"&apos;", "'",
		"&nbsp;", " ",
		"&mdash;", "—",
		"&ndash;", "–",
		"&hellip;", "...",
	)
	return replacer.Replace(s)
}
