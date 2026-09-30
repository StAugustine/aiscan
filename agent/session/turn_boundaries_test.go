package session

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/provider"
	aop "github.com/chainreactors/cyber/aop"
)

func TestCancelQueuedRunsAndWrongSessionDoNotInterruptRunningTurn(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) > 1 {
			writeRecoveryStream(w)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	p, err := provider.NewProvider(&provider.ProviderConfig{Provider: "openai", BaseURL: server.URL + "/v1", APIKey: "fixture", Model: "fixture", Timeout: 30})
	if err != nil {
		t.Fatal(err)
	}
	defer p.(interface{ CloseIdleConnections() }).CloseIdleConnections()
	rt := newBareRuntime(t, nil, p)
	conversation, err := rt.EnsureSession(SessionOptions{ID: "boundaries"})
	if err != nil {
		t.Fatal(err)
	}
	terminals := make(chan *aop.Event, 32)
	subscription := rt.Observe(recoveryObserver(func(event *aop.Event) {
		if event.GetTurnEnded() != nil {
			terminals <- event
		}
	}))
	defer subscription.Cancel()
	active, err := conversation.Run(t.Context(), RunInput{TurnID: "active", Message: agent.TextInput("hold")})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("stream did not start")
	}
	defer active.cancel()
	if err := rt.CancelSessionRun("unrelated", "active"); err == nil {
		t.Fatal("cross-session cancellation succeeded")
	}
	if _, err := conversation.Run(t.Context(), RunInput{TurnID: "active", Message: agent.TextInput("duplicate")}); err == nil {
		t.Fatal("duplicate active turn ID was admitted")
	}
	const count = 24
	queued := make([]*Run, count)
	for i := range queued {
		queued[i], err = conversation.Run(t.Context(), RunInput{TurnID: fmt.Sprintf("queued-%d", i), Message: agent.TextInput("must not reach model")})
		if err != nil {
			t.Fatal(err)
		}
	}
	var canceled sync.WaitGroup
	for _, run := range queued {
		canceled.Go(func() {
			if err := rt.CancelSessionRun("boundaries", run.TurnID()); err != nil {
				t.Error(err)
			}
		})
	}
	canceled.Wait()
	select {
	case <-active.done:
		t.Fatal("canceling queued work interrupted active stream")
	default:
	}
	if calls.Load() != 1 {
		t.Fatalf("queued turns reached provider before admission: %d", calls.Load())
	}
	if err := rt.CancelSessionRun("boundaries", "active"); err != nil {
		t.Fatal(err)
	}
	for _, run := range append([]*Run{active}, queued...) {
		select {
		case <-run.done:
		case <-time.After(time.Second):
			t.Fatalf("turn %s did not drain", run.TurnID())
		}
		if result, err := run.Wait(); !errors.Is(err, context.Canceled) || result.Stop != agent.StopReasonCanceled {
			t.Fatalf("turn %s = %v, %v", run.TurnID(), result, err)
		}
	}
	next, err := conversation.Run(t.Context(), RunInput{TurnID: "recovery", Message: agent.TextInput("recover")})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-next.done:
	case <-time.After(time.Second):
		t.Fatal("recovery remained blocked behind canceled queue")
	}
	if result, err := next.Wait(); err != nil || !strings.Contains(result.Output, "recovered") {
		t.Fatalf("recovery = %v, %v", result, err)
	}
	seen := make(map[string]int)
	for range count + 2 {
		select {
		case event := <-terminals:
			seen[event.TurnId]++
		case <-time.After(time.Second):
			t.Fatal("missing terminal")
		}
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("turn %s emitted %d terminals", id, count)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("canceled queued inputs leaked to provider: requests=%d", calls.Load())
	}
}
