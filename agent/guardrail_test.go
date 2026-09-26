package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/cyber/core/events"
	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/guardrail"
	"github.com/chainreactors/cyber/core/hooks"
	coretool "github.com/chainreactors/cyber/core/tool"
	toolhooks "github.com/chainreactors/cyber/core/tool/hooks"
)

func TestAutomaticGuardrailKeepsAgentLoopRunning(t *testing.T) {
	for _, action := range []guardrail.Action{guardrail.Action_ACTION_REVIEW, guardrail.Action_ACTION_BLOCK} {
		t.Run(action.String(), func(t *testing.T) {
			registry := hooks.New()
			runtime := guardrail.New(events.New(), time.Second, guardrail.ModeAuto)
			defer runtime.Close(t.Context())
			_, _ = runtime.Register("test", func(_ context.Context, ev toolhooks.CallEvent) (*guardrail.Decision, error) {
				if strings.Contains(string(ev.Call.Arguments.Data), "blocked") {
					return &guardrail.Decision{Action: action, Reason: "possible business impact"}, nil
				}
				return &guardrail.Decision{Action: guardrail.Action_ACTION_RECORD}, nil
			})
			toolhooks.Before.On(registry, "guardrail", runtime.Admit)
			echo := &recordingTool{name: "echo", output: "safe alternative executed"}
			tools := coretool.NewToolRegistry()
			if _, err := tools.Add(echo); err != nil {
				t.Fatal(err)
			}
			set, err := extension.New(extension.Provided[*hooks.Registry](registry), tools)
			if err != nil {
				t.Fatal(err)
			}
			if err := set.Load(t.Context()); err != nil {
				t.Fatal(err)
			}
			defer set.Close(t.Context())
			call := func(id, arguments string) *ChatCompletionResponse {
				return chatResponse(ChatMessage{Role: "assistant", ToolCalls: []ToolCall{{ID: id, Type: "function", Function: FunctionCall{Name: "echo", Arguments: arguments}}}})
			}
			llm := &scriptedProvider{responses: []*ChatCompletionResponse{call("first", `{"command":"blocked"}`), call("second", `{"command":"safe"}`), chatResponse(NewTextMessage("assistant", "finished safely"))}}
			result, err := NewAgent(Config{Loop: StandardLoop{}, Provider: llm, Tools: tools, Model: "test", SessionID: "guardrail-loop"}).Run(t.Context(), TextInput("complete the task"))
			if err != nil || result.Err != nil || result.Output != "finished safely" {
				t.Fatalf("loop stopped at interception: %v, %v", result, err)
			}
			requests := llm.requestsSnapshot()
			if len(requests) != 3 || !hasToolMessage(requests[1].Messages, "first", "possible business impact") || !hasToolMessage(requests[1].Messages, "first", "was not executed") || !hasToolMessage(requests[2].Messages, "second", "safe alternative executed") {
				t.Fatal("LLM did not receive interception followed by the next tool result")
			}
			if calls := echo.callsSnapshot(); len(calls) != 1 || calls[0] != `{"command":"safe"}` {
				t.Fatalf("unexpected executions: %v", calls)
			}
			if len(runtime.Pending("guardrail-loop")) != 0 {
				t.Fatal("automatic mode waited for human approval")
			}
		})
	}
}
