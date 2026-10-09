package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExplicitTaskRejectsAmbiguousOrEmptyInput(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "empty.txt")
	if err := os.WriteFile(empty, []byte(" \n\t"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, input := range []AgentOptions{
		{Prompt: "review", TaskFile: empty},
		{Prompt: " "},
		{TaskFile: empty},
		{Prompt: empty},
		{TaskFile: empty, Inputs: []string{"fixture"}},
	} {
		if task, err := ResolveTask(&Option{AgentOptions: input}); err == nil {
			t.Errorf("invalid explicit input accepted as %q", task)
		}
	}
}

func TestRuntimeRejectsInvalidExecutionOptions(t *testing.T) {
	for _, resolver := range []func(*Option) (string, error){ResolveRuntimeConfig, ResolveAgentRuntimeConfig, ResolveToolRuntimeConfig} {
		for _, mutate := range []func(*Option){
			func(o *Option) { o.Timeout = -1; o.MarkExplicit("timeout") },
			func(o *Option) { o.OutputFormat = "invalid"; o.MarkExplicit("output-format") },
		} {
			o := &Option{Context: isolatedContext(t)}
			mutate(o)
			if _, err := resolver(o); err == nil {
				t.Error("invalid execution options accepted")
			}
		}
	}
}

func TestNodeRejectsLocalTaskBeforeModelSelection(t *testing.T) {
	for _, input := range []AgentOptions{{Prompt: "fixture"}, {TaskFile: "fixture"}, {Inputs: []string{"fixture"}}, {Resume: "fixture"}} {
		input.ServerURL = "https://server.invalid"
		o := &Option{Context: isolatedContext(t), AgentOptions: input}
		if _, err := ResolveAgentRuntimeConfig(o); err == nil {
			t.Error("node silently ignored local input")
		}
	}
}

func TestExplicitEmptyTaskDoesNotFallBack(t *testing.T) {
	for _, flag := range []string{"prompt", "task-file"} {
		o := &Option{}
		o.MarkExplicit(flag)
		if !HasAgentTaskInput(o) {
			t.Fatalf("--%s lost its presence", flag)
		}
		if _, err := ResolveTask(o); err == nil {
			t.Fatalf("empty --%s silently fell back to stdin", flag)
		}
	}
}
