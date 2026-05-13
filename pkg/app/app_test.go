package app

import (
	"testing"

	"github.com/chainreactors/aiscan/pkg/provider"
)

func TestInitToolRegistryRegistersVisionOnlyWhenEnabled(t *testing.T) {
	cfg := &provider.ProviderConfig{
		BaseURL: "http://example.test/v1",
		APIKey:  "test-key",
		Model:   "gpt-4o",
	}

	withoutVision := initToolRegistry(ToolConfig{}, nil, nil, nil, cfg)
	if _, ok := withoutVision.Get("vision"); ok {
		t.Fatal("vision tool should not be registered by default")
	}

	withVision := initToolRegistry(ToolConfig{VisionEnabled: true}, nil, nil, nil, cfg)
	if _, ok := withVision.Get("vision"); !ok {
		t.Fatal("vision tool should be registered when explicitly enabled")
	}
}

func TestInitToolRegistrySkipsVisionWithoutProviderConfig(t *testing.T) {
	reg := initToolRegistry(ToolConfig{VisionEnabled: true}, nil, nil, nil, nil)
	if _, ok := reg.Get("vision"); ok {
		t.Fatal("vision tool should not be registered without provider config")
	}
}
