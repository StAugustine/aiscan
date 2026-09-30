package session

import (
	"testing"

	"github.com/chainreactors/cyber/agent/provider"
	aop "github.com/chainreactors/cyber/aop"
)

func TestRuntimeProviderReloadPreservesSessionHistory(t *testing.T) {
	rt := newBareRuntime(t, nil, &runtimeSemanticProvider{})
	session, err := rt.OpenSession(t.Context(), SessionOptions{
		ID:       "provider-reload",
		Messages: []*aop.Message{{Role: "user", Content: []*aop.Content{aop.Text("keep this context")}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	rt.ApplyProvider(&runtimeSemanticProvider{}, provider.ProviderConfig{Provider: "test", Model: "next-model"})
	if session.Model() != "next-model" {
		t.Fatalf("session model = %q, want next-model", session.Model())
	}
	history := session.MessagesSnapshot()
	if len(history) != 1 || history[0].Content[0].GetText().GetText() != "keep this context" {
		t.Fatalf("provider reload changed session history: %v", history)
	}
}
