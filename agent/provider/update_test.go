package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderUpdatePublishesOnlyAfterSuccessfulProbe(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusPaymentRequired} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			var unblock sync.Once
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				close(started)
				<-release
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				if status == http.StatusOK {
					_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
				} else {
					_, _ = w.Write([]byte(`{"error":{"message":"insufficient quota"}}`))
				}
			}))
			defer server.Close()
			defer unblock.Do(func() { close(release) })
			old := &borrowedStateProvider{}
			config := ProviderConfig{Model: "old"}
			health := Health{State: HealthReady, LatencyMs: 7, CheckedAt: time.Now()}
			var state State
			state.install(old, config, health)
			defer state.reset()
			var notifications atomic.Int32
			unsubscribe := state.Subscribe(func(Provider, ProviderConfig) { notifications.Add(1) })
			defer unsubscribe()
			done := make(chan error, 1)
			go func() {
				done <- state.Update(t.Context(), ProviderConfig{Provider: "openai", BaseURL: server.URL + "/v1", APIKey: "fixture", Model: "next"}, nil)
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("probe did not start")
			}
			p, pending := state.Current()
			if p != old || pending != config || state.Health() != health || notifications.Load() != 0 {
				t.Error("unverified configuration became visible to sessions")
			}
			unblock.Do(func() { close(release) })
			err := <-done
			p, current := state.Current()
			if status == http.StatusOK {
				if err != nil || p == old || current.Model != "next" || state.Health().State != HealthReady || notifications.Load() != 1 {
					t.Fatalf("successful update: config=%v health=%v notifications=%d err=%v", current, state.Health(), notifications.Load(), err)
				}
			} else if err == nil || p != old || current != config || state.Health() != health || notifications.Load() != 0 || len(state.owned) != 0 {
				t.Fatalf("failed update changed prior state: config=%v health=%v notifications=%d owned=%d err=%v", current, state.Health(), notifications.Load(), len(state.owned), err)
			}
		})
	}
}

func TestProviderUpdateCannotReplaceNewerPublication(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()
	defer unblock.Do(func() { close(release) })
	var state State
	defer state.reset()
	done := make(chan error, 1)
	go func() {
		done <- state.Update(t.Context(), ProviderConfig{Provider: "openai", BaseURL: server.URL + "/v1", APIKey: "fixture", Model: "checking"}, nil)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	newer := &borrowedStateProvider{}
	state.Set(newer, ProviderConfig{Model: "newer"})
	unblock.Do(func() { close(release) })
	if err := <-done; err == nil {
		t.Error("stale update was accepted")
	}
	if p, config := state.Current(); p != newer || config.Model != "newer" {
		t.Fatalf("late probe replaced newer publication: %v", config)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := state.Update(canceled, ProviderConfig{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled update = %v", err)
	}
}
