package jev

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	aop "github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

func TestExistingClaimReconsidersCompileWithoutRegenerating(t *testing.T) {
	var id string
	groups, compiles := 0, 0
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		if _, ok := req.Questions["claim0"]; ok {
			var state map[string]json.RawMessage
			_ = json.Unmarshal(req.State, &state)
			if !strings.Contains(string(state["sources"]), "advance") {
				t.Error("discovery cannot see the registered capability")
			}
			return map[string]jevapi.Answer{"claim0": answer(id)}
		}
		groups++
		if !strings.Contains(string(req.State), "current-goal") {
			t.Error("compile judgment lost the current interaction")
		}
		return declarationAnswers(req, groups > 1)
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{
		Name: "advance", Run: func(context.Context, *coretool.Execution) (any, error) { return "ok", nil },
		Observe: func(context.Context, []*aop.Message) (json.RawMessage, map[string]*aop.Content, error) {
			return nil, nil, nil
		},
	})
	var claims []Claim
	_ = json.Unmarshal([]byte(fixtureClaim), &claims)
	id = "c" + digest(claims[0])[:16]
	e.library.Claims[id] = claimRecord{Claim: claims[0], Task: "original-task", Consumed: true}
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		if provider.MessageText(req.Messages[0]) != compilePrompt {
			t.Fatal("matching a Claim regenerated it")
		}
		compiles++
		return reply(provider.TextMessage("assistant", fixtureReflex)), nil
	})
	state, _ := json.Marshal(map[string]string{"goal": "current-goal"})
	job := declaration{cfg: cfg, task: "current-task", state: state, focus: []string{"Select the current operation"}}
	for i := 0; i < 3; i++ {
		if err := e.declare(t.Context(), job); err != nil {
			t.Fatal(err)
		}
		if i == 0 && len(e.snapshot().Reflexes) != 0 {
			t.Fatal("ignored JEV's compile defer")
		}
	}
	lib := e.snapshot()
	if groups != 2 || compiles != 1 || len(lib.Claims) != 1 || len(lib.Reflexes) != 1 || !lib.Claims[id].Consumed || lib.Claims[id].Task != "original-task" {
		t.Fatalf("groups=%d compiles=%d library=%+v", groups, compiles, lib)
	}
}

func TestCompileFailureDoesNotMarkGroupComplete(t *testing.T) {
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer { return declarationAnswers(req, true) })
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{
		Name: "advance", Run: func(context.Context, *coretool.Execution) (any, error) { return "ok", nil },
		Observe: func(context.Context, []*aop.Message) (json.RawMessage, map[string]*aop.Content, error) {
			return nil, nil, nil
		},
	})
	var claims []Claim
	_ = json.Unmarshal([]byte(fixtureClaim), &claims)
	id := "c" + digest(claims[0])[:16]
	e.library.Claims[id] = claimRecord{Claim: claims[0]}
	// A legacy attempt marker without a published scene must not survive load.
	e.library.Compiled[digest(map[string]Claim{id: claims[0]})] = true
	if err := e.saveLibrary(); err != nil {
		t.Fatal(err)
	}
	if err := e.loadLibrary(); err != nil || len(e.snapshot().Compiled) != 0 {
		t.Fatalf("legacy failed generation remained complete: %v", err)
	}
	calls := 0
	cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("generation unavailable")
		}
		if calls == 2 {
			return reply(provider.TextMessage("assistant", "null")), nil
		}
		return reply(provider.TextMessage("assistant", fixtureReflex)), nil
	})
	for i := 0; i < 4; i++ {
		err := e.compile(t.Context(), declaration{cfg: cfg}, id)
		if (err != nil) != (i == 0) {
			t.Fatalf("attempt=%d error=%v", i, err)
		}
		lib := e.snapshot()
		if i < 2 && (len(lib.Compiled) != 0 || len(lib.Reflexes) != 0) {
			t.Fatal("unsuccessful generation permanently marked the group complete")
		}
	}
	if calls != 3 || len(e.snapshot().Compiled) != 1 || len(e.snapshot().Reflexes) != 1 {
		t.Fatalf("generation calls=%d library=%+v", calls, e.snapshot())
	}
	restored := New(Config{Directory: e.config.Directory})
	if err := restored.loadLibrary(); err != nil || len(restored.snapshot().Compiled) != 1 || len(restored.snapshot().Reflexes) != 1 {
		t.Fatalf("durable completion failed: %v", err)
	}
}

func TestIdleAutoPreservesOrdinaryModelPrompt(t *testing.T) {
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		out := map[string]jevapi.Answer{}
		for id := range req.Questions {
			out[id] = answer(Defer)
		}
		return out
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
	calls := 0
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		calls++
		if len(req.Messages) != 2 || provider.MessageText(req.Messages[0]) != cfg.SystemPrompt || provider.MessageText(req.Messages[1]) != "Answer directly" {
			t.Error("idle acceleration expanded or rewrote the model context")
		}
		return reply(provider.TextMessage("assistant", "done")), nil
	})
	if _, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Answer directly")); err != nil {
		t.Fatal(err)
	}
	settle(t, e)
	if calls != 1 {
		t.Fatalf("model calls=%d", calls)
	}
	received := receipt([]string{"Executed a native operation"}, "evidence.jsonl", "REPORT")
	if len(received) != 1 || !strings.Contains(provider.MessageText(received[0]), Prompt) || len(receipt(nil, "", "")) != 0 {
		t.Fatal("controller guidance must accompany actual handoff evidence")
	}
}
