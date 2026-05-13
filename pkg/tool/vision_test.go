package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chainreactors/aiscan/pkg/provider"
)

func TestVisionTool_Name(t *testing.T) {
	vt := NewVisionTool(nil)
	if vt.Name() != "vision" {
		t.Fatalf("expected name 'vision', got %q", vt.Name())
	}
}

func TestVisionTool_Definition(t *testing.T) {
	vt := NewVisionTool(nil)
	def := vt.Definition()
	if def.Type != "function" {
		t.Fatalf("expected type 'function', got %q", def.Type)
	}
	if def.Function.Name != "vision" {
		t.Fatalf("expected function name 'vision', got %q", def.Function.Name)
	}
	params, ok := def.Function.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties map in parameters")
	}
	for _, required := range []string{"image_path", "prompt"} {
		if _, exists := params[required]; !exists {
			t.Errorf("missing required property %q", required)
		}
	}
}

func TestMimeFromExt(t *testing.T) {
	cases := []struct {
		ext  string
		mime string
		ok   bool
	}{
		{".png", "image/png", true},
		{".PNG", "image/png", true},
		{".jpg", "image/jpeg", true},
		{".jpeg", "image/jpeg", true},
		{".gif", "image/gif", true},
		{".webp", "image/webp", true},
		{".bmp", "image/bmp", true},
		{".svg", "", false},
		{".txt", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		mime, ok := mimeFromExt(tc.ext)
		if ok != tc.ok || mime != tc.mime {
			t.Errorf("mimeFromExt(%q) = (%q, %v), want (%q, %v)", tc.ext, mime, ok, tc.mime, tc.ok)
		}
	}
}

func TestVisionTool_Execute_NoProvider(t *testing.T) {
	vt := NewVisionTool(nil)
	_, err := vt.Execute(context.Background(), `{"image_path":"/tmp/x.png","prompt":"read"}`)
	if err == nil || !strings.Contains(err.Error(), "no LLM provider configured") {
		t.Fatalf("expected provider error, got: %v", err)
	}
}

func TestVisionTool_Execute_InvalidArgs(t *testing.T) {
	cfg := &provider.ProviderConfig{BaseURL: "http://localhost", APIKey: "test", Model: "test"}
	vt := NewVisionTool(cfg)
	_, err := vt.Execute(context.Background(), `not json`)
	if err == nil || !strings.Contains(err.Error(), "invalid arguments") {
		t.Fatalf("expected invalid arguments error, got: %v", err)
	}
}

func TestVisionTool_Execute_MissingFields(t *testing.T) {
	cfg := &provider.ProviderConfig{BaseURL: "http://localhost", APIKey: "test", Model: "test"}
	vt := NewVisionTool(cfg)

	// Missing prompt
	_, err := vt.Execute(context.Background(), `{"image_path":"/tmp/x.png"}`)
	if err == nil || !strings.Contains(err.Error(), "prompt is required") {
		t.Fatalf("expected prompt required error, got: %v", err)
	}

	// Missing image_path
	_, err = vt.Execute(context.Background(), `{"prompt":"read captcha"}`)
	if err == nil || !strings.Contains(err.Error(), "image_path is required") {
		t.Fatalf("expected image_path required error, got: %v", err)
	}
}

func TestVisionTool_Execute_UnsupportedFormat(t *testing.T) {
	cfg := &provider.ProviderConfig{BaseURL: "http://localhost", APIKey: "test", Model: "test"}
	vt := NewVisionTool(cfg)
	_, err := vt.Execute(context.Background(), `{"image_path":"/tmp/x.svg","prompt":"read"}`)
	if err == nil || !strings.Contains(err.Error(), "unsupported image format") {
		t.Fatalf("expected unsupported format error, got: %v", err)
	}
}

func TestVisionTool_Execute_FileNotFound(t *testing.T) {
	cfg := &provider.ProviderConfig{BaseURL: "http://localhost", APIKey: "test", Model: "test"}
	vt := NewVisionTool(cfg)
	_, err := vt.Execute(context.Background(), `{"image_path":"/nonexistent/cap.png","prompt":"read"}`)
	if err == nil || !strings.Contains(err.Error(), "cannot access image") {
		t.Fatalf("expected file access error, got: %v", err)
	}
}

func TestVisionTool_Execute_RejectsNonRegularFile(t *testing.T) {
	cfg := &provider.ProviderConfig{BaseURL: "http://localhost", APIKey: "test", Model: "test"}
	vt := NewVisionTool(cfg)
	imgPath := filepath.Join(t.TempDir(), "not-a-file.png")
	if err := os.Mkdir(imgPath, 0755); err != nil {
		t.Fatal(err)
	}

	args := fmt.Sprintf(`{"image_path":%q,"prompt":"read"}`, imgPath)
	_, err := vt.Execute(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("expected regular file error, got: %v", err)
	}
}

func TestVisionTool_Execute_RejectsOversizedFile(t *testing.T) {
	cfg := &provider.ProviderConfig{BaseURL: "http://localhost", APIKey: "test", Model: "test"}
	vt := NewVisionTool(cfg)
	imgPath := filepath.Join(t.TempDir(), "huge.png")
	f, err := os.Create(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(int64(maxImageSize) + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	args := fmt.Sprintf(`{"image_path":%q,"prompt":"read"}`, imgPath)
	_, err = vt.Execute(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "image too large") {
		t.Fatalf("expected image too large error, got: %v", err)
	}
}

func TestVisionTool_Execute_MockServer(t *testing.T) {
	// Create a tiny 1x1 PNG file for testing.
	tmpDir := t.TempDir()
	imgPath := filepath.Join(tmpDir, "captcha.png")
	// Minimal valid PNG: 1x1 pixel, RGBA
	pngData := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, // PNG signature
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41, // IDAT chunk
		0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x02, 0x00, 0x01, 0xe2, 0x21, 0xbc,
		0x33, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, // IEND chunk
		0x44, 0xae, 0x42, 0x60, 0x82,
	}
	if err := os.WriteFile(imgPath, pngData, 0644); err != nil {
		t.Fatal(err)
	}

	// Mock OpenAI-compatible vision endpoint.
	expectedText := "482956"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("unexpected auth header: %s", auth)
		}

		// Verify request body structure.
		body, _ := io.ReadAll(r.Body)
		var req visionRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("unmarshal request: %v", err)
		}
		if req.Model != "gpt-4o" {
			t.Errorf("expected model gpt-4o, got %s", req.Model)
		}
		if len(req.Messages) != 1 || len(req.Messages[0].Content) != 2 {
			t.Errorf("expected 1 message with 2 content parts, got %d messages", len(req.Messages))
		}
		if req.Messages[0].Content[0].Type != "text" {
			t.Errorf("first part should be text, got %s", req.Messages[0].Content[0].Type)
		}
		if req.Messages[0].Content[1].Type != "image_url" {
			t.Errorf("second part should be image_url, got %s", req.Messages[0].Content[1].Type)
		}
		if !strings.HasPrefix(req.Messages[0].Content[1].ImageURL.URL, "data:image/png;base64,") {
			t.Error("image_url should be a base64 data URI")
		}

		// Return mock response.
		resp := fmt.Sprintf(`{
			"id": "chatcmpl-test",
			"choices": [{
				"message": {"role": "assistant", "content": %q},
				"finish_reason": "stop"
			}]
		}`, expectedText)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	cfg := &provider.ProviderConfig{
		BaseURL: srv.URL,
		APIKey:  "test-key",
		Model:   "gpt-4o",
	}
	vt := NewVisionTool(cfg)

	args := fmt.Sprintf(`{"image_path":%q,"prompt":"Read the 6-digit captcha"}`, imgPath)
	result, err := vt.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !strings.Contains(result, expectedText) {
		t.Errorf("expected result to contain %q, got:\n%s", expectedText, result)
	}
	if !strings.Contains(result, "captcha.png") {
		t.Errorf("expected result to contain filename, got:\n%s", result)
	}
	if !strings.Contains(result, "image/png") {
		t.Errorf("expected result to contain MIME type, got:\n%s", result)
	}
}

func TestVisionTool_Execute_APIError(t *testing.T) {
	tmpDir := t.TempDir()
	imgPath := filepath.Join(tmpDir, "test.jpg")
	if err := os.WriteFile(imgPath, []byte("fake-jpeg"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"invalid api key","type":"auth_error"}}`))
	}))
	defer srv.Close()

	cfg := &provider.ProviderConfig{BaseURL: srv.URL, APIKey: "bad", Model: "test"}
	vt := NewVisionTool(cfg)
	args := fmt.Sprintf(`{"image_path":%q,"prompt":"read"}`, imgPath)
	_, err := vt.Execute(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected 401 error, got: %v", err)
	}
}
