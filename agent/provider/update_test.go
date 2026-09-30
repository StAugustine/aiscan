package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
			if p != old || pending != config || state.Health() != health {
				t.Error("unverified configuration became visible to sessions")
			}
			unblock.Do(func() { close(release) })
			err := <-done
			p, current := state.Current()
			if status == http.StatusOK {
				if err != nil || p == old || current.Model != "next" || state.Health().State != HealthReady {
					t.Fatalf("successful update: config=%v health=%v err=%v", current, state.Health(), err)
				}
			} else if err == nil || p != old || current != config || state.Health() != health || len(state.owned) != 0 {
				t.Fatalf("failed update changed prior state: config=%v health=%v owned=%d err=%v", current, state.Health(), len(state.owned), err)
			}
		})
	}
}

func TestProviderUpdateCanceledDuringProbeKeepsUsableState(t *testing.T) {
	started, disconnected := make(chan struct{}), make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		select {
		case <-r.Context().Done():
			close(disconnected)
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	old := &borrowedStateProvider{}
	var state State
	config := ProviderConfig{Model: "usable"}
	state.Set(old, config)
	defer state.reset()
	health := state.Health()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- state.Update(ctx, ProviderConfig{Provider: "openai", BaseURL: server.URL + "/v1", APIKey: "fixture", Model: "pending"}, nil)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled update = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not interrupt provider probe")
	}
	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("canceled probe left its HTTP request running")
	}
	if current, got := state.Current(); current != old || got != config || state.Health() != health {
		t.Fatalf("canceled probe changed usable provider: config=%v health=%v", got, state.Health())
	}
}

func TestProviderUpdateCommitsOnceAfterProbeAndKeepsOldClientOnFailure(t *testing.T) {
	for _, failure := range []string{"none", "probe", "commit"} {
		t.Run(failure, func(t *testing.T) {
			var probes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				probes.Add(1)
				if failure == "probe" {
					http.Error(w, `{"error":{"message":"quota"}}`, http.StatusPaymentRequired)
					return
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
			}))
			defer server.Close()
			old := &borrowedStateProvider{}
			var state State
			config := ProviderConfig{Model: "old"}
			state.Set(old, config)
			defer state.reset()
			health := state.Health()
			commits := 0
			diskFull := errors.New("disk full")
			err := state.Update(t.Context(), ProviderConfig{Provider: "openai", BaseURL: server.URL + "/v1", APIKey: "fixture", Model: "new"}, nil, func() error {
				commits++
				if failure == "commit" {
					return diskFull
				}
				return nil
			})
			if failure == "probe" && (err == nil || commits != 0) {
				t.Fatalf("unverified provider committed: commits=%d err=%v", commits, err)
			}
			if failure == "commit" && (!errors.Is(err, diskFull) || commits != 1) {
				t.Fatalf("commit failure = %v, commits=%d", err, commits)
			}
			current, next := state.Current()
			if failure != "none" {
				if current != old || next != config || state.Health() != health {
					t.Fatalf("failed transaction damaged old provider: config=%v", next)
				}
			} else if err != nil || commits != 1 || current == old || next.Model != "new" {
				t.Fatalf("successful update = %v, commits=%d config=%v", err, commits, next)
			}
			if probes.Load() != 1 {
				t.Fatalf("update probed %d times; expected one validation with no rollback reload", probes.Load())
			}
		})
	}
}

func TestConcurrentProviderProbesCannotPublishStaleOrResurrectResetOwner(t *testing.T) {
	for _, reset := range []bool{false, true} {
		t.Run(fmt.Sprint("reset=", reset), func(t *testing.T) {
			started := make(chan string, 2)
			gates := map[string]chan struct{}{"slow": make(chan struct{}), "fast": make(chan struct{})}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, _ := io.ReadAll(r.Body)
				var input struct {
					Model string `json:"model"`
				}
				_ = json.Unmarshal(data, &input)
				started <- input.Model
				<-gates[input.Model]
				_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
			}))
			defer server.Close()
			var releases sync.Once
			defer releases.Do(func() { close(gates["slow"]); close(gates["fast"]) })
			var state State
			state.Set(&borrowedStateProvider{}, ProviderConfig{Model: "old"})
			defer state.reset()
			done := map[string]chan error{"slow": make(chan error, 1), "fast": make(chan error, 1)}
			for _, model := range []string{"slow", "fast"} {
				go func() {
					done[model] <- state.Update(t.Context(), ProviderConfig{Provider: "openai", BaseURL: server.URL + "/v1", APIKey: "fixture", Model: model}, nil)
				}()
			}
			for range 2 {
				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("concurrent probes did not start")
				}
			}
			if reset {
				state.reset()
			}
			close(gates["fast"])
			fastErr := <-done["fast"]
			close(gates["slow"])
			releases.Do(func() {})
			slowErr := <-done["slow"]
			current, config := state.Current()
			if reset {
				if fastErr == nil || slowErr == nil || current != nil || config != (ProviderConfig{}) {
					t.Fatalf("late probes resurrected reset owner: config=%v errors=%v/%v", config, fastErr, slowErr)
				}
			} else if fastErr != nil || slowErr == nil || current == nil || config.Model != "fast" {
				t.Fatalf("stale probe won publication: config=%v errors=%v/%v", config, fastErr, slowErr)
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
