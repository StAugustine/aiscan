package tool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	aop "github.com/chainreactors/cyber/aop"
)

func TestChoiceObservationSharesRegistrationLifetime(t *testing.T) {
	r, _ := loadTestRegistry(t)
	entered := make(chan struct{})
	h, err := r.Add(Command{Name: "observe", Contract: "v1", Run: func(context.Context, *Execution) (any, error) { t.Error("observation ran action"); return nil, nil }, Choices: func(ctx context.Context, _ []*aop.Message) (json.RawMessage, map[string]*aop.Content, error) {
		close(entered)
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	if r.ChoiceCommands()["observe"] != "v1" {
		t.Fatal("missing contract")
	}
	done := make(chan error, 1)
	go func() { _, _, err := r.Choices(t.Context(), "observe", nil); done <- err }()
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
	if len(r.ChoiceCommands()) != 0 {
		t.Fatal("removed callback still discoverable")
	}
	if _, _, err = r.Choices(t.Context(), "observe", nil); err == nil {
		t.Fatal("removed callback callable")
	}
}
