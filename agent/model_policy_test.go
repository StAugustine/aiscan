package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/agent/hooks"
	"github.com/chainreactors/cyber/agent/provider"
	aop "github.com/chainreactors/cyber/aop"
	corehooks "github.com/chainreactors/cyber/core/hooks"
)

func TestModelRequestPolicyCannotDispatchCompositionTools(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "response", true: "stream"}[stream], func(t *testing.T) {
			registry := corehooks.New()
			calls := 0
			hooks.ModelRequestPolicy.On(registry, "composition", func(_ context.Context, ev hooks.ModelRequestEvent) (hooks.ModelPolicy, error) {
				calls++
				ev.Messages[0] = provider.TextMessage("user", "modified")
				return hooks.ModelPolicy{Purpose: "composition", DisableTools: true}, nil
			})
			hooks.ModelRequestPolicy.On(registry, "later", func(context.Context, hooks.ModelRequestEvent) (hooks.ModelPolicy, error) {
				return hooks.ModelPolicy{DisableTools: false}, nil
			})
			echo := &recordingTool{name: "echo", output: "must not execute"}
			toolMessage := &aop.Message{Role: "assistant", Content: []*aop.Content{{Value: &aop.Content_ToolCall{ToolCall: &aop.ToolCall{Id: "one", Name: "echo", Arguments: &aop.EncodedValue{Data: []byte(`{"value":"x"}`)}}}}}}
			llm := &scriptedProvider{responses: []*ChatCompletionResponse{{Choices: []provider.Choice{{Message: toolMessage}}}}, streamEventBatches: [][]ChatCompletionStreamEvent{{roleDelta("assistant"), toolCallDelta(0, "one", "echo", `{"value":"x"}`), {Done: true}}}}
			result, err := NewAgent(Config{Loop: StandardLoop{}, Provider: llm, Tools: newTestTools(t, echo), Hooks: registry, Stream: stream, MaxRetries: -1}).Run(t.Context(), TextInput("Compose only"))
			if err == nil || !strings.Contains(err.Error(), "forbids tool calls") || calls != 1 || len(echo.callsSnapshot()) != 0 {
				t.Fatalf("result=%v err=%v policy=%d effects=%d", result, err, calls, len(echo.callsSnapshot()))
			}
		})
	}
}
func TestModelRequestPolicyDenialAndRetries(t *testing.T) {
	registry := corehooks.New()
	policies, requests := 0, 0
	hooks.ModelRequestPolicy.On(registry, "policy", func(context.Context, hooks.ModelRequestEvent) (hooks.ModelPolicy, error) {
		policies++
		return hooks.ModelPolicy{Purpose: "composition", DisableTools: true}, nil
	})
	llm := &callbackProvider{fn: func(_ context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
		requests++
		if req.Purpose != "composition" || len(req.Tools) != 0 {
			t.Fatal("restriction was lost")
		}
		if requests == 1 {
			return nil, errors.New("connection reset")
		}
		return &ChatCompletionResponse{Choices: []provider.Choice{{Message: provider.TextMessage("assistant", "answer")}}, Usage: &aop.TokenUsage{}}, nil
	}}
	result, err := NewAgent(Config{Loop: StandardLoop{}, Provider: llm, Hooks: registry, MaxRetries: 1}).Run(t.Context(), TextInput("Compose"))
	if err != nil || result.Output != "answer" || policies != 2 || requests != 2 {
		t.Fatalf("result=%v err=%v requests=%d policies=%d", result, err, requests, policies)
	}
	hooks.ModelRequestPolicy.On(registry, "deny", func(context.Context, hooks.ModelRequestEvent) (hooks.ModelPolicy, error) {
		return hooks.ModelPolicy{Deny: errors.New("execution not permitted")}, nil
	})
	_, err = NewAgent(Config{Provider: llm, Hooks: registry}).Run(t.Context(), TextInput("Denied"))
	if err == nil || requests != 2 {
		t.Fatal("denied policy still contacted model")
	}
}
