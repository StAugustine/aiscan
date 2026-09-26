package jev

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"strings"
	"time"

	"github.com/chainreactors/cyber/aop"
	toolhooks "github.com/chainreactors/cyber/core/tool/hooks"
	"github.com/chainreactors/cyber/core/types"
	cfg "github.com/chainreactors/cyber/pkg/config"
)

// testConnection judges an inert local-read description. It never executes a
// tool, and uses judge directly so a fallback cannot masquerade as API success.
func testConnection(ctx context.Context, incoming, stored *types.DistributeConfig) []*types.ConnectionCheck {
	started := time.Now()
	check := &types.ConnectionCheck{Name: "jev"}
	defer func() { check.LatencyMs = time.Since(started).Milliseconds() }()
	values := maps.Clone(cfg.ValuesFromProto(stored.GetExtensions())[ConfigKey])
	if values == nil {
		values = map[string]any{}
	}
	for key, value := range cfg.ValuesFromProto(incoming.GetExtensions())[ConfigKey] {
		if key == "api_key" && (value == nil || value == "") {
			continue
		}
		values[key] = value
	}
	config := defaults(Config{})
	raw, err := json.Marshal(values)
	if err == nil {
		err = json.Unmarshal(raw, &config)
	}
	if err == nil {
		err = config.validate()
	}
	if err != nil {
		check.Error = "Invalid JEV configuration"
		return []*types.ConnectionCheck{check}
	}
	if strings.TrimSpace(config.APIKey) == "" {
		config.APIKey = os.Getenv("TYPESAFE_API_KEY")
	}
	if strings.TrimSpace(config.APIKey) == "" {
		check.Error = "JEV API key is not configured"
		return []*types.ConnectionCheck{check}
	}
	duration, _ := time.ParseDuration(config.Timeout)
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	client := New(config)
	defer client.client.CloseIdleConnections()
	decision, err := client.judge(ctx, toolhooks.CallEvent{Call: &aop.ToolCall{Name: "read_file", WorkingDirectory: "local-test", Arguments: &aop.EncodedValue{Data: []byte(`{"path":"README.md","purpose":"Read a local public project document; no network access or writes"}`), MediaType: aop.JSONMediaType}}})
	if err != nil {
		check.Error = err.Error()
		return []*types.ConnectionCheck{check}
	}
	check.Ok = true
	check.Detail = "JEV " + strings.TrimPrefix(decision.Action.String(), "ACTION_") + "; judgment only, no tool executed"
	return []*types.ConnectionCheck{check}
}
