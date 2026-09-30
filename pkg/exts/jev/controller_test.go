package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	aop "github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

func TestPendingEffectStaysWithReflexUntilReport(t *testing.T) {
	var executed, observations, decisions, declarations atomic.Int64
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		if !runtimeRequest(req) {
			declarations.Add(1)
			return runtimeAnswers(req, Defer)
		}
		n := decisions.Add(1)
		var state struct {
			Candidates map[string]string `json:"candidates"`
		}
		if err := json.Unmarshal(req.State, &state); err != nil {
			t.Error(err)
		}
		if n == 1 && state.Candidates["async/go"] == "" {
			t.Error("generation judgment cannot see current argument bindings")
		}
		if n > 1 && state.Candidates["async/go"] != "" {
			t.Error("already dispatched binding remained in shared state")
		}
		if _, entry := req.Questions["entry"]; entry != (n == 1) {
			t.Errorf("scene entry re-evaluated during an active Reflex: decision=%d entry=%t", n, entry)
		}
		switch n {
		case 1:
			return runtimeAnswers(req, "async/go")
		case 2, 3, 4, 5:
			for id, q := range req.Questions {
				if id != "entry" && q.Criteria.(map[string]any)["async/go"] != nil {
					t.Error("already dispatched action remained selectable")
				}
			}
			return runtimeAnswers(req, reobserve)
		default:
			return runtimeAnswers(req, report)
		}
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{
		Name: "async", Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
			executed.Add(1)
			_, err := fmt.Fprint(ex.Stdout, "effect dispatched")
			return nil, err
		}, Observe: func(context.Context, []*aop.Message) (json.RawMessage, map[string]*aop.Content, error) {
			if observations.Add(1) >= 6 {
				return json.RawMessage(`{"receipt":"completed"}`), nil, nil
			}
			return json.RawMessage(`{"state":"unchanged"}`), map[string]*aop.Content{"go": action("async")}, nil
		},
	})
	installReflex(e, "async")
	var modelCalls atomic.Int64
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		modelCalls.Add(1)
		last := provider.MessageText(req.Messages[len(req.Messages)-1])
		if !strings.Contains(last, "REPORT:") || !strings.Contains(last, `"receipt":"completed"`) {
			t.Errorf("premature model handoff: %s", last)
		}
		return reply(provider.TextMessage("assistant", "completed")), nil
	})
	result, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Complete the asynchronous operation"))
	if err != nil || result.Output != "completed" || executed.Load() != 1 || modelCalls.Load() != 1 {
		t.Fatalf("result=%v err=%v executions=%d model=%d", result, err, executed.Load(), modelCalls.Load())
	}
	settle(t, e)
	if declarations.Load() != 1 {
		t.Fatalf("expected only the final model output to need discovery, got %d", declarations.Load())
	}
}

func TestPendingObservationStopsAtDecisionBudget(t *testing.T) {
	var decisions, modelCalls atomic.Int64
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		if runtimeRequest(req) {
			decisions.Add(1)
			return runtimeAnswers(req, reobserve)
		}
		return runtimeAnswers(req, Defer)
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{
		Name: "pending", Run: func(context.Context, *coretool.Execution) (any, error) {
			t.Error("observation dispatched a tool")
			return nil, nil
		},
		Observe: func(context.Context, []*aop.Message) (json.RawMessage, map[string]*aop.Content, error) {
			return json.RawMessage(`{"status":"pending"}`), nil, nil
		},
	})
	installReflex(e, "pending")
	cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		modelCalls.Add(1)
		return reply(provider.TextMessage("assistant", "The effect is still pending.")), nil
	})
	if _, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Report when the pending operation completes")); err != nil {
		t.Fatal(err)
	}
	settle(t, e)
	if decisions.Load() != maxDecisions || modelCalls.Load() != 1 {
		t.Fatalf("decisions=%d model=%d", decisions.Load(), modelCalls.Load())
	}
}

func TestStaleCommandAcrossShellReobservesWithoutModel(t *testing.T) {
	var attempts atomic.Int64
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		if attempts.Load() >= 2 {
			return runtimeAnswers(req, report)
		}
		return runtimeAnswers(req, "fresh/go")
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{
		Name: "fresh", Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
			if attempts.Add(1) == 1 {
				return nil, coretool.ErrStaleChoice
			}
			_, err := fmt.Fprint(ex.Stdout, "fresh-result")
			return nil, err
		}, Observe: func(context.Context, []*aop.Message) (json.RawMessage, map[string]*aop.Content, error) {
			return json.RawMessage(`{}`), map[string]*aop.Content{"go": action("fresh")}, nil
		},
	})
	installReflex(e, "fresh")
	var modelCalls atomic.Int64
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		modelCalls.Add(1)
		if !strings.Contains(provider.MessageText(req.Messages[len(req.Messages)-1]), "fresh-result") {
			t.Error("stale dispatch incorrectly yielded before re-observation")
		}
		if text := provider.MessageText(req.Messages[len(req.Messages)-1]); strings.Contains(text, "stale") || strings.Contains(text, "outcome requires review") {
			t.Errorf("recovered no-effect dispatch leaked into report: %s", text)
		}
		return reply(provider.TextMessage("assistant", "done")), nil
	})
	if _, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Perform the operation")); err != nil {
		t.Fatal(err)
	}
	settle(t, e)
	if attempts.Load() != 2 || modelCalls.Load() != 1 || len(e.executions) != 0 {
		t.Fatalf("attempts=%d model=%d pending=%d", attempts.Load(), modelCalls.Load(), len(e.executions))
	}
}

func TestCompoundCommandWithEffectsCannotRecoverAsStale(t *testing.T) {
	var effects atomic.Int64
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		return runtimeAnswers(req, "mixed/go")
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{
		Name: "mixed", Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
			if len(ex.Args) != 0 {
				return nil, coretool.ErrStaleChoice
			}
			effects.Add(1)
			return nil, nil
		}, Observe: func(context.Context, []*aop.Message) (json.RawMessage, map[string]*aop.Content, error) {
			return json.RawMessage(`{}`), map[string]*aop.Content{"go": action("mixed; mixed stale")}, nil
		},
	})
	installReflex(e, "mixed")
	cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		return reply(provider.TextMessage("assistant", "Partial effects need review")), nil
	})
	if _, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Perform the known operation")); err != nil {
		t.Fatal(err)
	}
	if effects.Load() != 1 {
		t.Fatalf("partial effects replayed %d times", effects.Load())
	}
}

func TestModelSuppliesMissingInputThenReflexResumes(t *testing.T) {
	for _, gap := range []string{"parameter", "strategy", Defer} {
		t.Run(gap, func(t *testing.T) {
			var ready atomic.Bool
			var position, modelCalls atomic.Int64
			client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
				if !ready.Load() {
					answers := runtimeAnswers(req, "workflow/go")
					if _, ok := req.Questions["generation"]; ok {
						answers["generation"] = answer(gap)
					}
					return answers // The closest action cannot override a generation gap.
				}
				if position.Load() == 3 {
					return runtimeAnswers(req, report)
				}
				return runtimeAnswers(req, "workflow/go")
			})
			e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client,
				coretool.Command{Name: "provide", Run: func(context.Context, *coretool.Execution) (any, error) {
					ready.Store(true)
					return nil, nil
				}},
				coretool.Command{Name: "workflow", Run: func(context.Context, *coretool.Execution) (any, error) {
					if !ready.Load() {
						t.Error("acted before missing input was supplied")
					}
					position.Add(1)
					return nil, nil
				}, Observe: func(context.Context, []*aop.Message) (json.RawMessage, map[string]*aop.Content, error) {
					return json.RawMessage(fmt.Sprintf(`{"ready":%t,"step":%d}`, ready.Load(), position.Load())), map[string]*aop.Content{"go": action("workflow")}, nil
				}})
			installReflex(e, "workflow")
			cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
				if modelCalls.Add(1) == 1 {
					return reply(&aop.Message{Role: "assistant", Content: []*aop.Content{action("provide")}}), nil
				}
				if position.Load() != 3 || !strings.Contains(provider.MessageText(req.Messages[len(req.Messages)-1]), "REPORT:") {
					t.Error("model retained control of the remaining workflow")
				}
				return reply(provider.TextMessage("assistant", "done")), nil
			})
			result, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Complete the workflow"))
			if err != nil || result.Output != "done" || modelCalls.Load() != 2 {
				t.Fatalf("result=%v err=%v model=%d", result, err, modelCalls.Load())
			}
			settle(t, e)
		})
	}
}
