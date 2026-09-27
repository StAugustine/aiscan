package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/hooks"
	"github.com/chainreactors/cyber/agent/inbox"
	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	aop "github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
	"google.golang.org/protobuf/proto"
)

func TestContextRewriterLeavesDecisionWithModel(t *testing.T) {
	for _, useHook := range []bool{false, true} {
		t.Run(fmt.Sprint(useHook), func(t *testing.T) {
			client := fakeJEV(t, func(jevapi.Request) map[string]jevapi.Answer {
				t.Error("controller must not act on a different request projection")
				return nil
			})
			e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
			if useHook {
				hooks.Context.On(cfg.Hooks, "rewrite", func(_ context.Context, ev hooks.ContextEvent) (hooks.ContextResult, error) {
					return hooks.ContextResult{Messages: []*aop.Message{provider.TextMessage("user", "Updated task")}}, nil
				})
			} else {
				cfg.TransformContext = func([]*aop.Message) []*aop.Message {
					return []*aop.Message{provider.TextMessage("user", "Updated task")}
				}
			}
			cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
				if provider.MessageText(req.Messages[len(req.Messages)-1]) != "Updated task" {
					t.Error("ordinary context projection was bypassed")
				}
				return reply(provider.TextMessage("assistant", "Done")), nil
			})
			seedActive(e, cfg, "context", map[string]string{"one": "judgment one", "two": "judgment two"})
			if _, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Old task")); err != nil {
				t.Fatal(err)
			}
			awaitLearning(t, e)
			if len(e.training) != 0 || client.Usage().Detail["requests"] != 0 {
				t.Fatal("rewritten context produced training or takeover")
			}
		})
	}
}

func TestCompilationSharesMessagesWithoutLosingDecisionEvidence(t *testing.T) {
	e := New(Config{Directory: t.TempDir()})
	projection, ok := contextState([]*aop.Message{provider.TextMessage("system", strings.Repeat("authorized constraints; ", 200)), provider.TextMessage("user", "Inspect A, never B")})
	if !ok {
		t.Fatal("fixture projection rejected")
	}
	var samples []sample
	for i := 0; i < 8; i++ {
		state, _ := json.Marshal(map[string]any{"context": projection, "observation": map[string]int{"step": i}})
		samples = append(samples, sample{Source: "step", State: state})
	}
	llm := testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		data := provider.MessageText(req.Messages[1])
		if len(data) >= len(samples[0].State)*len(samples)/2 {
			t.Fatal("repeated context was not removed from compilation wire")
		}
		var input struct {
			Messages map[string]json.RawMessage `json:"messages"`
			Examples []struct {
				State map[string]json.RawMessage `json:"state"`
			} `json:"examples"`
		}
		if err := json.Unmarshal([]byte(data), &input); err != nil {
			t.Fatal(err)
		}
		for i, example := range input.Examples {
			var context map[string]json.RawMessage
			_ = json.Unmarshal(example.State["context"], &context)
			var refs []string
			_ = json.Unmarshal(context["messages"], &refs)
			var prefix []json.RawMessage
			for _, ref := range refs {
				if input.Messages[ref] == nil {
					t.Fatal("unresolved message reference")
				}
				prefix = append(prefix, input.Messages[ref])
			}
			context["messages"], _ = json.Marshal(prefix)
			example.State["context"], _ = json.Marshal(context)
			var original map[string]json.RawMessage
			_ = json.Unmarshal(samples[i].State, &original)
			if digest(example.State) != digest(original) {
				t.Fatal("shared compilation changed decision evidence")
			}
		}
		return reply(provider.TextMessage("assistant", "[]")), nil
	})
	for i := range samples {
		samples[i].cfg.Provider = llm
	}
	if _, err := e.compile(t.Context(), samples); err != nil {
		t.Fatal(err)
	}
}

func TestLearningRejectsChangedDecisionEvidence(t *testing.T) {
	for _, variant := range []string{"unchanged", "assigned receipt ID", "rewritten prefix", "new input"} {
		t.Run(variant, func(t *testing.T) {
			e := New(Config{})
			prefix := []*aop.Message{provider.TextMessage("user", "Inspect A"), receipt([]string{"observed A"}, "evidence.jsonl", "review required")[0]}
			e.pending["task"] = []sample{{Source: "playwright", Messages: cloneMessages(prefix), boundary: len(prefix)}}
			messages := cloneMessages(prefix)
			switch variant {
			case "assigned receipt ID":
				messages[1].Id = "committed-receipt"
			case "rewritten prefix":
				messages[0] = provider.TextMessage("user", "Inspect B")
			case "new input":
				messages = append(messages, provider.TextMessage("user", "Stop; do B instead"))
			}
			messages = append(messages, provider.TextMessage("assistant", "Finished"))
			before := cloneMessages(messages)
			e.collect("task", messages)
			want := 0
			if variant == "unchanged" || variant == "assigned receipt ID" {
				want = 1
			}
			if len(e.tasks["task"]) != want {
				t.Fatalf("collected %d samples, want %d", len(e.tasks["task"]), want)
			}
			for i := range messages {
				if !proto.Equal(messages[i], before[i]) {
					t.Fatal("collection changed model history")
				}
			}
		})
	}
}

func TestMediaConstraintsCannotSilentlyBecomeTextOnly(t *testing.T) {
	m := provider.TextMessage("user", "Use the target shown in this image")
	m.Content = append(m.Content, &aop.Content{Value: &aop.Content_Media{Media: &aop.MediaContent{Kind: "image"}}})
	if _, ok := contextState([]*aop.Message{m}); ok {
		t.Fatal("controller accepted task without its media constraints")
	}
}

func TestInputDuringDecisionStopsDispatch(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var executions atomic.Int64
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		if strings.Contains(string(req.State), "Stop executing") {
			return map[string]jevapi.Answer{"entry": answer(Defer)}
		}
		close(entered)
		<-release
		out := map[string]jevapi.Answer{}
		for id := range req.Questions {
			if id != "entry" {
				out["entry"], out[id] = answer(id), answer("go")
			}
		}
		return out
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{Name: "step", Contract: "step-v1", Run: func(context.Context, *coretool.Execution) (any, error) { executions.Add(1); return nil, nil }, Choices: func(context.Context, []*aop.Message) (json.RawMessage, map[string]*aop.Content, error) {
		return json.RawMessage(`{}`), map[string]*aop.Content{"go": action("step")}, nil
	}})
	ib := inbox.NewBuffered(8)
	cfg.Inbox = ib
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		return reply(provider.TextMessage("assistant", "Stopped as requested.")), nil
	})
	seedActive(e, cfg, "step", nil)
	done := make(chan error, 1)
	go func() {
		_, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Perform the finite step."))
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("controller did not enter")
	}
	msg := inbox.NewUserMessage("Stop executing steps")
	msg.Interrupt = true
	if err := ib.Push(msg); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("input failed to interrupt controller")
	}
	if executions.Load() != 0 {
		t.Fatal("dispatched after new input")
	}
}

func TestFiniteJudgmentAppendsOnceAndRequiresModelReview(t *testing.T) {
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		out := map[string]jevapi.Answer{}
		for id := range req.Questions {
			if id != "entry" {
				out["entry"], out[id] = answer(id), answer("insufficient")
			}
		}
		return out
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		last := req.Messages[len(req.Messages)-1]
		if last.Name != "jev" || !strings.Contains(provider.MessageText(last), "requires review; not proof of success") {
			t.Error("judgment presented as proof")
		}
		return reply(provider.TextMessage("assistant", "Independent evidence is still missing.")), nil
	})
	seedActive(e, cfg, "context", map[string]string{"insufficient": "The supplied observations do not independently confirm a vulnerability.", "sufficient": "Evidence is available for further verification."})
	r, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Assess the evidence."))
	if err != nil {
		t.Fatal(err)
	}
	if client.Usage().Detail["requests"] != 1 || r.Turns != 1 {
		t.Fatal("judgment looped or bypassed model review")
	}
}

func TestEnvironmentVariantsDoNotOverwriteActivation(t *testing.T) {
	e := New(Config{Directory: t.TempDir()})
	r := &Reflex{Source: "step", Contract: "step-v1", Enter: "entry", Decide: "decision", Environment: "model-a", Phase: "active", TrainingTasks: []string{"training"}, Checks: passingChecks()}
	r.ID = ruleID(r)
	e.rules[r.ID] = r
	// A training-task sample cannot be validation evidence for either variant.
	e.learn(t.Context(), []sample{{Source: r.Source, Contract: r.Contract, Environment: "model-b", Task: "training"}})
	if e.rules[r.ID].Environment != "model-a" || e.rules[r.ID].Phase != "active" || len(e.rules[r.ID].Checks) != 32 {
		t.Fatal("old deployment evidence mutated")
	}
	if len(e.Rules()) != 2 {
		t.Fatal("new environment not independently bound")
	}
	for _, local := range e.Rules() {
		if local.Environment == "model-b" && (local.Phase != "validating" || len(local.Checks) != 0) {
			t.Fatal("old evidence trusted by new environment")
		}
	}
}

func TestNoProgressYieldsWithinBound(t *testing.T) {
	var executions atomic.Int64
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		out := map[string]jevapi.Answer{}
		for id := range req.Questions {
			if id != "entry" {
				out["entry"], out[id] = answer(id), answer("go")
			}
		}
		return out
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{Name: "stuck", Contract: "stuck-v1", Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
		executions.Add(1)
		_, err := fmt.Fprint(ex.Stdout, "unchanged")
		return nil, err
	}, Choices: func(context.Context, []*aop.Message) (json.RawMessage, map[string]*aop.Content, error) {
		return json.RawMessage(`{"status":"unchanged"}`), map[string]*aop.Content{"go": action("stuck")}, nil
	}})
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		return reply(provider.TextMessage("assistant", "No progress; another strategy is required.")), nil
	})
	seedActive(e, cfg, "stuck", nil)
	if _, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Try the known step.")); err != nil {
		t.Fatal(err)
	}
	if executions.Load() != 2 {
		t.Fatalf("no-progress count=%d", executions.Load())
	}
}
