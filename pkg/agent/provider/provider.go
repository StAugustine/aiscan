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
	Images   *bool  `yaml:"images,omitempty" config:"images"`
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

	if resolved.Images == nil {
		v := inferImageSupport(resolved.BaseURL, resolved.Model)
		resolved.Images = &v
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

// inferImageSupport guesses whether a provider+model combination accepts
// image content parts.  Known vision-capable providers return true; unknown
// providers default to false so the agent gracefully degrades instead of
// crashing with a 400.
func inferImageSupport(baseURL, model string) bool {
	u := strings.ToLower(strings.TrimSpace(baseURL))
	m := strings.ToLower(strings.TrimSpace(model))

	// Anthropic Claude models support images.
	if strings.Contains(u, "anthropic.com") {
		return true
	}
	// OpenAI vision models.
	if strings.Contains(u, "openai.com") {
		return true
	}
	// Google Gemini models support images.
	if strings.Contains(u, "googleapis.com") || strings.Contains(u, "generativelanguage") {
		return true
	}

	// Model-name heuristics for providers that mix vision/text-only models.
	for _, kw := range []string{"vision", "vl", "multimodal", "4o", "gpt-4-turbo"} {
		if strings.Contains(m, kw) {
			return true
		}
	}

	return false
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
