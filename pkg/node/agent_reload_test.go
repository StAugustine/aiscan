package node

import (
	"testing"

	cfg "github.com/chainreactors/cyber/pkg/config"
)

func TestOnlyLLMChanged(t *testing.T) {
	base := &cfg.Option{
		NodeOptions: cfg.NodeOptions{NodeID: "node-1", NodeName: "worker"},
		LLMOptions:  cfg.LLMOptions{Provider: "openai", BaseURL: "https://llm.invalid/v1", APIKey: "key", Model: "model-a"},
	}
	next := *base
	next.LLMOptions.Model = "model-b"
	if !onlyLLMChanged(base, &next) {
		t.Fatal("model-only changes should be eligible for in-place reload")
	}
	next.Tools = []string{"new-tool"}
	if onlyLLMChanged(base, &next) {
		t.Fatal("tool changes must still rebuild the profile")
	}
}
