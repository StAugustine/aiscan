package provider

import (
	"context"
	"fmt"
	"strings"
)

type Provider interface {
	Name() string
	ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error)
}

type StreamingProvider interface {
	Provider
	ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan ChatCompletionStreamEvent, error)
}

type WebSearchProvider interface {
	WebSearch(ctx context.Context, query string, maxResults int) (*WebSearchResponse, error)
}

type WebSearchResult struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type WebSearchResponse struct {
	Results []WebSearchResult `json:"results,omitempty"`
	Summary string            `json:"summary,omitempty"`
}

type ProviderConfig struct {
	Provider string `yaml:"provider" config:"provider"`
	BaseURL  string `yaml:"base_url" config:"base_url"`
	APIKey   string `yaml:"api_key"  config:"api_key"`
	Model    string `yaml:"model"    config:"model"`
	Proxy    string `yaml:"proxy"    config:"proxy"`
	Timeout  int    `yaml:"timeout"  config:"timeout"`
}

func NormalizeProvider(name string) string {
	if strings.EqualFold(name, "anthropic") {
		return "anthropic"
	}
	return "openai"
}

func Resolve(cfg *ProviderConfig) (*ProviderConfig, error) {
	resolved := *cfg

	if resolved.Provider == "" {
		if resolved.BaseURL != "" {
			resolved.Provider = InferFromBaseURL(resolved.BaseURL)
		} else {
			resolved.Provider = "openai"
		}
	}
	resolved.Provider = NormalizeProvider(resolved.Provider)

	if resolved.BaseURL == "" {
		switch resolved.Provider {
		case "anthropic":
			resolved.BaseURL = "https://api.anthropic.com/v1"
		default:
			resolved.BaseURL = "https://api.openai.com/v1"
		}
	}

	if resolved.APIKey == "" {
		return nil, fmt.Errorf("no API key: set --api-key, llm.api_key, or AISCAN_API_KEY")
	}

	if resolved.Timeout <= 0 {
		resolved.Timeout = 120
	}

	return &resolved, nil
}

func NewProvider(cfg *ProviderConfig) (Provider, error) {
	resolved, err := Resolve(cfg)
	if err != nil {
		return nil, err
	}
	return NewProviderFromResolved(resolved)
}

func InferFromBaseURL(baseURL string) string {
	baseURL = strings.ToLower(strings.TrimSpace(baseURL))
	if strings.Contains(baseURL, "anthropic.com") {
		return "anthropic"
	}
	return "openai"
}

func NewProviderFromResolved(cfg *ProviderConfig) (Provider, error) {
	if strings.ToLower(cfg.Provider) == "anthropic" {
		return NewAnthropicProvider(cfg)
	}
	return NewOpenAIProvider(cfg)
}
