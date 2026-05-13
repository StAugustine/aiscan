package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/chainreactors/aiscan/pkg/provider"
	"github.com/chainreactors/aiscan/pkg/scanner/cyberhub"
	"github.com/chainreactors/aiscan/pkg/scanner/resources"
)

const defaultCyberhubLimit = 20

// CyberhubSearchTool searches loaded fingerprints and POC templates.
// It delegates to the existing cyberhub.Command for filtering and formatting.
type CyberhubSearchTool struct {
	resources *resources.Set
}

// NewCyberhubSearchTool creates a cyberhub search tool using the given resource set.
func NewCyberhubSearchTool(res *resources.Set) *CyberhubSearchTool {
	return &CyberhubSearchTool{resources: res}
}

func (t *CyberhubSearchTool) Name() string { return "cyberhub_search" }

func (t *CyberhubSearchTool) Description() string {
	return "Search loaded fingerprints and POC vulnerability templates in the local database. " +
		"Use for offline CVE matching, finding available exploits for detected services, and checking template coverage. " +
		"Supports filtering by type (finger/poc), severity, and tags."
}

func (t *CyberhubSearchTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionDefinition{
			Name:        "cyberhub_search",
			Description: t.Description(),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search term: product name, CVE ID, technology name, or keyword",
					},
					"resource_type": map[string]any{
						"type":        "string",
						"enum":        []string{"finger", "poc", "all"},
						"description": "Resource type to search. finger = service fingerprints, poc = vulnerability templates, all = both (default: all)",
					},
					"severity": map[string]any{
						"type":        "string",
						"description": "Filter POCs by severity: critical, high, medium, low, info. Comma-separated for multiple.",
					},
					"tags": map[string]any{
						"type":        "string",
						"description": "Filter by tags. Comma-separated for multiple (e.g. 'rce,cve').",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results (default: 20, 0 for all)",
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

func (t *CyberhubSearchTool) Execute(ctx context.Context, arguments string) (string, error) {
	var args struct {
		Query        string `json:"query"`
		ResourceType string `json:"resource_type"`
		Severity     string `json:"severity"`
		Tags         string `json:"tags"`
		Limit        int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	cmd := cyberhub.New(t.resources)
	cmdArgs := buildCyberhubArgs(query, args.ResourceType, args.Severity, args.Tags, args.Limit)
	return cmd.Execute(ctx, cmdArgs)
}

func buildCyberhubArgs(query, resourceType, severity, tags string, limit int) []string {
	var args []string
	args = append(args, "search")

	resourceType = strings.TrimSpace(strings.ToLower(resourceType))
	if resourceType != "" {
		args = append(args, resourceType)
	}

	args = append(args, query)

	if severity = strings.TrimSpace(severity); severity != "" {
		args = append(args, "--severity", severity)
	}
	if tags = strings.TrimSpace(tags); tags != "" {
		args = append(args, "--tag", tags)
	}

	if limit <= 0 {
		limit = defaultCyberhubLimit
	}
	args = append(args, "--limit", strconv.Itoa(limit))
	args = append(args, "--json")

	return args
}
