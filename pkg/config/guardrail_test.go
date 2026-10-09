package config

import (
	"github.com/chainreactors/cyber/core/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"testing"
)

func TestGuardrailModeChangeIsNarrow(t *testing.T) {
	section, _ := structpb.NewStruct(map[string]any{"mode": "safe", "review_timeout": "5m"})
	policy, _ := structpb.NewStruct(map[string]any{"enabled": true, "model": "jev"})
	current := &types.DistributeConfig{Extensions: map[string]*structpb.Struct{"guardrail": section, "jev": policy}}
	for _, mode := range []string{"safe", "auto", "off"} {
		next := proto.CloneOf(current)
		next.Extensions["guardrail"].Fields["mode"] = structpb.NewStringValue(mode)
		if got, ok := GuardrailModeChange(current, next); !ok || got != mode {
			t.Fatalf("mode %s not recognized", mode)
		}
	}
	for _, edit := range []func(*types.DistributeConfig){
		func(c *types.DistributeConfig) { c.Extensions["jev"].Fields["enabled"] = structpb.NewBoolValue(false) },
		func(c *types.DistributeConfig) {
			c.Extensions["guardrail"].Fields["review_timeout"] = structpb.NewStringValue("1m")
		},
		func(c *types.DistributeConfig) {
			c.Extensions["guardrail"].Fields["mode"] = structpb.NewStringValue("unknown")
		},
	} {
		next := proto.CloneOf(current)
		edit(next)
		if _, ok := GuardrailModeChange(current, next); ok {
			t.Fatal("policy or invalid edit took mode-only shortcut")
		}
	}
	if current.Extensions["guardrail"].Fields["mode"].GetStringValue() != "safe" {
		t.Fatal("mutated source")
	}
}
