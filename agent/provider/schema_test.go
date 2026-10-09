package provider

import (
	"bytes"
	"encoding/json"
	"testing"

	aop "github.com/chainreactors/cyber/aop"
)

func TestVendorToolSchemasPreserveExactJSONAndFallbacks(t *testing.T) {
	for _, raw := range []string{
		`{"type":"object","properties":{"id":{"type":"integer","const":9007199254740993}},"required":["id"]}`,
		` {"type":"object","properties":{"value":{"oneOf":[{"type":"string"},{"type":"null"}]}}} `,
		``, `null`, `[]`, `true`, `{"type":`,
	} {
		t.Run(raw, func(t *testing.T) {
			def := &aop.ToolDefinition{Name: "fixture"}
			if raw != "" {
				def.InputSchema = &aop.EncodedValue{Data: []byte(raw)}
			}
			req := &ChatCompletionRequest{Model: "fixture", Tools: []*aop.ToolDefinition{def}}
			anthropic := &AnthropicProvider{config: &ProviderConfig{BaseURL: "https://api.anthropic.com"}}
			for _, vendor := range []struct {
				name   string
				encode func(*ChatCompletionRequest) ([]byte, error)
			}{{"openai", marshalOpenAIRequest}, {"anthropic", anthropic.marshalRequest}} {
				t.Run(vendor.name, func(t *testing.T) {
					body, err := vendor.encode(req)
					if err != nil {
						t.Fatal(err)
					}
					var result struct {
						Tools []struct {
							Input    json.RawMessage `json:"input_schema"`
							Function struct {
								Parameters json.RawMessage `json:"parameters"`
							} `json:"function"`
						} `json:"tools"`
					}
					if err := json.Unmarshal(body, &result); err != nil || len(result.Tools) != 1 {
						t.Fatalf("request: %s, %v", body, err)
					}
					got := result.Tools[0].Input
					if vendor.name == "openai" {
						got = result.Tools[0].Function.Parameters
					}
					want := raw
					if !json.Valid([]byte(raw)) || len(bytes.TrimSpace([]byte(raw))) == 0 || bytes.TrimSpace([]byte(raw))[0] != '{' {
						want = `{"type":"object","properties":{}}`
					}
					var compact bytes.Buffer
					if err := json.Compact(&compact, []byte(want)); err != nil {
						t.Fatal(err)
					}
					if string(got) != compact.String() {
						t.Fatalf("schema changed: %s, want %s", got, compact.String())
					}
					if def.InputSchema != nil && string(def.InputSchema.Data) != raw {
						t.Fatal("input mutated")
					}
				})
			}
		})
	}
}

func TestAnthropicToolArgumentsPreserveNumbersAndValidation(t *testing.T) {
	p := &AnthropicProvider{config: &ProviderConfig{BaseURL: "https://api.anthropic.com"}}
	for _, raw := range []string{`{"id":9007199254740993}`, ``, ` {"value":null} `, `{invalid`} {
		t.Run(raw, func(t *testing.T) {
			call := &aop.ToolCall{Id: "call", Name: "fixture", Arguments: &aop.EncodedValue{Data: []byte(raw)}}
			req := &ChatCompletionRequest{Messages: []*aop.Message{{Role: "assistant", Content: []*aop.Content{{Value: &aop.Content_ToolCall{ToolCall: call}}}}}}
			body, err := p.marshalRequest(req)
			if raw == `{invalid` {
				if err == nil {
					t.Fatal("invalid arguments reached the vendor request")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				Messages []struct {
					Content []struct {
						Input json.RawMessage `json:"input"`
					} `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(body, &result); err != nil || len(result.Messages) != 1 || len(result.Messages[0].Content) != 1 {
				t.Fatalf("request: %s, %v", body, err)
			}
			want := raw
			if want == "" {
				want = `{}`
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, []byte(want)); err != nil {
				t.Fatal(err)
			}
			if got := result.Messages[0].Content[0].Input; string(got) != compact.String() {
				t.Fatalf("arguments changed: %s, want %s", got, compact.String())
			}
			if string(call.Arguments.Data) != raw {
				t.Fatal("source arguments mutated")
			}
		})
	}
}
