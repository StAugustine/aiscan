package tool

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	aop "github.com/chainreactors/cyber/aop"
)

func TestObserveSharesRegistrationLifetime(t *testing.T) {
	r, _ := loadTestRegistry(t)
	entered := make(chan struct{})
	h, err := r.Add(Command{Name: "observe", Run: func(context.Context, *Execution) (any, error) { t.Error("observation ran action"); return nil, nil }, Observe: func(ctx context.Context, _ []*aop.Message) (json.RawMessage, map[string]*aop.Content, error) {
		close(entered)
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.ObserveCommands()) != 1 || r.ObserveCommands()[0] != "observe" {
		t.Fatal("missing contract")
	}
	done := make(chan error, 1)
	go func() { _, _, err := r.Observe(t.Context(), "observe", nil); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("not observed")
	}
	if err = h.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("observation was not canceled on unregister", err)
	}
	if len(r.ObserveCommands()) != 0 {
		t.Fatal("removed callback still discoverable")
	}
	if _, _, err = r.Observe(t.Context(), "observe", nil); err == nil {
		t.Fatal("removed callback callable")
	}
}

func TestObserveIsOptionalAndDoesNotInvokeRun(t *testing.T) {
	r, _ := loadTestRegistry(t)
	called := false
	_, err := r.Add(Command{Name: "ordinary", Run: func(context.Context, *Execution) (any, error) {
		called = true
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	state, candidates, err := r.Observe(t.Context(), "ordinary", nil)
	if err != nil || len(state) != 0 || len(candidates) != 0 || called || len(r.ObserveCommands()) != 0 {
		t.Fatalf("optional observation changed behavior: %s %v %v", state, candidates, err)
	}
	if _, err := r.Execute(t.Context(), "ordinary", &Execution{Stdout: io.Discard, Stderr: io.Discard}); err != nil || !called {
		t.Fatalf("ordinary command unavailable: %v", err)
	}
}
