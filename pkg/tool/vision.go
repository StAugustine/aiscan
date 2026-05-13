package tool

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chainreactors/aiscan/pkg/provider"
)

const (
	visionTimeout   = 120 * time.Second
	maxImageSize    = 20 * 1024 * 1024 // 20 MB
	visionMaxTokens = 2048
)

// VisionTool sends a local image to a vision-capable LLM and returns the
// model's textual analysis.  Primary use cases: captcha solving, screenshot
// OCR, diagram interpretation, credential extraction from images.
type VisionTool struct {
	config *provider.ProviderConfig
	client *http.Client
}

// NewVisionTool creates a vision analysis tool that reuses the configured LLM
// provider endpoint.  Passing a nil config is safe — Execute will return an
// error explaining that no provider is configured.
func NewVisionTool(cfg *provider.ProviderConfig) *VisionTool {
	transport := &http.Transport{}
	if cfg != nil && cfg.Proxy != "" {
		if proxyURL, err := url.Parse(cfg.Proxy); err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}
	return &VisionTool{
		config: cfg,
		client: &http.Client{
			Transport: transport,
			Timeout:   visionTimeout,
		},
	}
}

func (t *VisionTool) Name() string { return "vision" }

func (t *VisionTool) Description() string {
	return "Analyze an image file using a vision-capable LLM. " +
		"Use for OCR on screenshots, interpreting network topology diagrams, " +
		"extracting visible text from authorized assessment artifacts, and analyzing web page screenshots. " +
		"Provide a local file path and a prompt describing what to extract or analyze."
}

func (t *VisionTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionDefinition{
			Name:        "vision",
			Description: t.Description(),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"image_path": map[string]any{
						"type":        "string",
						"description": "Absolute path to the image file (PNG, JPG, JPEG, GIF, WEBP, BMP).",
					},
					"prompt": map[string]any{
						"type":        "string",
						"description": "What to analyze or extract from the image, e.g. 'Read the visible text', 'Summarize the screenshot', 'Describe the network topology'.",
					},
				},
				"required": []string{"image_path", "prompt"},
			},
		},
	}
}

func (t *VisionTool) Execute(ctx context.Context, arguments string) (string, error) {
	if t.config == nil || t.config.BaseURL == "" {
		return "", fmt.Errorf("vision tool: no LLM provider configured")
	}

	var args struct {
		ImagePath string `json:"image_path"`
		Prompt    string `json:"prompt"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	imagePath := strings.TrimSpace(args.ImagePath)
	prompt := strings.TrimSpace(args.Prompt)
	if imagePath == "" {
		return "", fmt.Errorf("image_path is required")
	}
	if prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}

	// Validate extension and resolve MIME type.
	ext := filepath.Ext(imagePath)
	mime, ok := mimeFromExt(ext)
	if !ok {
		return "", fmt.Errorf("unsupported image format %q (supported: png, jpg, jpeg, gif, webp, bmp)", ext)
	}

	// Read and size-check the file from one open handle so a file cannot grow
	// past the limit between a preflight stat and the actual read.
	data, err := readImageFile(imagePath)
	if err != nil {
		return "", err
	}

	// Build base64 data URI.
	b64 := base64.StdEncoding.EncodeToString(data)
	dataURI := fmt.Sprintf("data:%s;base64,%s", mime, b64)

	// Assemble the multimodal chat completion request.
	reqBody := visionRequest{
		Model: t.config.Model,
		Messages: []visionMessage{
			{
				Role: "user",
				Content: []contentPart{
					{Type: "text", Text: prompt},
					{Type: "image_url", ImageURL: &imageURL{URL: dataURI, Detail: "high"}},
				},
			},
		},
		MaxTokens: visionMaxTokens,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	endpoint := strings.TrimSuffix(t.config.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if t.config.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+t.config.APIKey)
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("vision request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("vision API error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result provider.ChatCompletionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("vision API error: [%s] %s", result.Error.Type, result.Error.Message)
	}
	if len(result.Choices) == 0 || result.Choices[0].Message.Content == nil {
		return "", fmt.Errorf("vision API returned empty response")
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Image: %s (%s, %d bytes)\n", filepath.Base(imagePath), mime, len(data)))
	sb.WriteString(fmt.Sprintf("Prompt: %s\n", prompt))
	sb.WriteString("---\n")
	sb.WriteString(*result.Choices[0].Message.Content)
	return sb.String(), nil
}

// ---------------------------------------------------------------------------
// Internal types — multimodal message format (OpenAI-compatible vision API)
// ---------------------------------------------------------------------------

// visionMessage carries an array of content parts instead of a plain string,
// which is the format vision-capable endpoints expect.
type visionMessage struct {
	Role    string        `json:"role"`
	Content []contentPart `json:"content"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type visionRequest struct {
	Model     string          `json:"model"`
	Messages  []visionMessage `json:"messages"`
	MaxTokens int             `json:"max_tokens"`
}

// mimeFromExt maps common image file extensions to MIME types.
func mimeFromExt(ext string) (string, bool) {
	switch strings.ToLower(ext) {
	case ".png":
		return "image/png", true
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".gif":
		return "image/gif", true
	case ".webp":
		return "image/webp", true
	case ".bmp":
		return "image/bmp", true
	default:
		return "", false
	}
}

func readImageFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot access image: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat image: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("image must be a regular file")
	}
	if info.Size() > maxImageSize {
		return nil, fmt.Errorf("image too large: %d bytes (max %d)", info.Size(), maxImageSize)
	}

	data, err := io.ReadAll(io.LimitReader(f, int64(maxImageSize)+1))
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	if len(data) > maxImageSize {
		return nil, fmt.Errorf("image too large: %d bytes (max %d)", len(data), maxImageSize)
	}
	return data, nil
}
