package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/events"
	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/guardrail"
	"github.com/chainreactors/cyber/core/hooks"
	"github.com/chainreactors/cyber/core/resource"
	toolhooks "github.com/chainreactors/cyber/core/tool/hooks"
	"github.com/chainreactors/cyber/core/types"
	cfg "github.com/chainreactors/cyber/pkg/config"
)

func call() toolhooks.CallEvent {
	return toolhooks.CallEvent{Call: &aop.ToolCall{Name: "shell", WorkingDirectory: "/workspace", Arguments: &aop.EncodedValue{Data: []byte(`{"command":"curl -H 'Authorization: Bearer dummysecret' https://target/","api_key":"dummykey"}`)}}}
}

func TestEnvironmentKeyViewIsMetadataOnly(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "fixture-env-secret")
	view := &types.ConfigView{}
	ProjectView(view)
	ProjectView(view)
	entry := view.Extensions[ConfigKey]
	if len(entry.ConfiguredSecrets) != 1 || entry.ConfiguredSecrets[0] != "api_key" {
		t.Fatal("environment activation missing or duplicated")
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), "fixture-env-secret") || len(entry.Values.Fields) != 0 {
		t.Fatal("environment key entered editable configuration")
	}
}

func TestConfiguredKeyAlwaysInstallsTwoStageChecks(t *testing.T) {
	for _, consequence := range []string{"record", "review", "block", "failure"} {
		t.Run(consequence, func(t *testing.T) {
			var requests, executions atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body request
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				q := body.Questions["action"]
				choice := "review"
				if requests.Add(1) == 1 {
					if !strings.Contains(q.Instructions, "Stage 1:") || q.Criteria["review"] != "operator screening marker" {
						t.Error("missing screening policy")
					}
				} else {
					if !strings.Contains(q.Instructions, "Stage 2:") || q.Criteria["review"] == "operator screening marker" {
						t.Error("screening criteria reused as consequence verdict")
					}
					if consequence == "failure" {
						w.WriteHeader(500)
						return
					}
					choice = consequence
				}
				fmt.Fprintf(w, `{"answers":{"action":{"type":"choice","choice":%q}}}`, choice)
			}))
			defer server.Close()
			runtime := guardrail.New(events.New(), time.Second, "")
			defer runtime.Close(t.Context())
			// Even legacy enabled:false cannot disable a configured provider.
			e := New(Config{Enabled: false, APIKey: "fixture-key", OnError: "record", Criteria: map[string]string{"review": "operator screening marker"}})
			e.endpoint = server.URL
			set, _ := extension.New(extension.Provided[*guardrail.Runtime](runtime), e)
			if err := set.Load(t.Context()); err != nil {
				t.Fatal(err)
			}
			defer set.Close(t.Context())
			registry := hooks.New()
			toolhooks.Before.On(registry, "guardrail", runtime.Admit)
			result, err := toolhooks.Execute(t.Context(), registry, "echo", `{"command":"echo safe"}`, func(context.Context, string) (*aop.ToolResult, error) {
				executions.Add(1)
				return &aop.ToolResult{}, nil
			})
			if requests.Load() != 2 {
				t.Fatalf("wanted both stages, requests=%d", requests.Load())
			}
			if consequence == "record" {
				if err != nil || executions.Load() != 1 {
					t.Fatalf("harmless consequence not released: %v", err)
				}
			} else if err == nil || executions.Load() != 0 || !result.IsError || result.Terminate {
				t.Fatal("harmful/unknown/failed consequence executed or stopped the agent")
			}
		})
	}
}

func TestChoiceWireAndPerInvocationJudgment(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("bad authentication")
		}
		var body request
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Model != defaultModel || body.Questions["action"].Type != "choice" || len(body.Questions["action"].Criteria) != 3 {
			t.Errorf("bad judgment request: %+v", body.Questions)
		}
		state := string(body.State)
		if !strings.Contains(state, "curl") || strings.Contains(state, "dummysecret") || strings.Contains(state, "dummykey") {
			t.Errorf("state was opaque or unsanitized: %s", state)
		}
		fmt.Fprint(w, `{"model":"jev-1.13.0","answers":{"action":{"type":"choice","choice":"review","confidence":0.9}}}`)
	}))
	defer server.Close()
	e := New(Config{APIKey: "fixture-key"})
	e.endpoint = server.URL
	for range 2 {
		d, err := e.check(t.Context(), call())
		if err != nil || d.Action != guardrail.Action_ACTION_REVIEW {
			t.Fatalf("decision=%v err=%v", d, err)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("judgment was cached")
	}
}

func TestProviderFailureFallbackAndCancellation(t *testing.T) {
	for _, failure := range []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", 401, "private upstream body"},
		{"unprocessable", 422, "private upstream body"},
		{"server", 500, "private upstream body"},
		{"malformed", 200, "not JSON"},
		{"missing", 200, `{"answers":{}}`},
		{"invalid", 200, `{"answers":{"action":{"type":"choice","choice":"allow"}}}`},
		{"wrong-type", 200, `{"answers":{"action":{"type":"noul","choice":"record"}}}`},
	} {
		t.Run(failure.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(failure.status)
				fmt.Fprint(w, failure.body)
			}))
			defer server.Close()
			for _, fallback := range []string{"record", "review", "block"} {
				e := New(Config{OnError: fallback})
				e.endpoint = server.URL
				d, err := e.check(t.Context(), call())
				want := action(fallback)
				if fallback == "record" {
					want = guardrail.Action_ACTION_REVIEW
				}
				if err != nil || d.Action != want || strings.Contains(d.Reason, "private") {
					t.Fatalf("decision=%v err=%v", d, err)
				}
			}
			if calls.Load() != 3 {
				t.Fatalf("non-transient failures retried: %d", calls.Load())
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer server.Close()
	e := New(Config{Timeout: "30ms", OnError: "record"})
	e.endpoint = server.URL
	d, err := e.check(t.Context(), call())
	if err != nil || d.Action != guardrail.Action_ACTION_REVIEW {
		t.Fatalf("timeout fallback %v %v", d, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = e.check(ctx, call()); err == nil {
		t.Fatal("cancellation fell back to record")
	}
}

func TestRetriesAreBounded(t *testing.T) {
	for _, status := range []int{429, 529} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(status) }))
			defer server.Close()
			e := New(Config{})
			e.endpoint = server.URL
			d, err := e.check(t.Context(), call())
			if err != nil || d.Action != guardrail.Action_ACTION_BLOCK || calls.Load() != 3 {
				t.Fatalf("decision=%v err=%v attempts=%d", d, err, calls.Load())
			}
		})
	}
}

func TestHTTPRedirectDoesNotForwardCredentials(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer server.Close()
	e := New(Config{APIKey: "fixture-key"})
	e.endpoint = server.URL
	d, err := e.check(t.Context(), call())
	if err != nil || d.Action != guardrail.Action_ACTION_BLOCK || targetCalls.Load() != 0 {
		t.Fatal("redirect allowed or followed")
	}
}

func TestEnabledMissingKeyFailsLoad(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		runtime := guardrail.New(events.New(), time.Second, guardrail.ModeSafe)
		e := New(Config{Enabled: enabled})
		set, err := extension.New(extension.Provided[*guardrail.Runtime](runtime), e)
		if err != nil {
			t.Fatal(err)
		}
		err = set.Load(t.Context())
		if (err != nil) != enabled {
			t.Fatalf("enabled=%v err=%v", enabled, err)
		}
		if closeErr := set.Close(t.Context()); closeErr != nil {
			t.Fatal(closeErr)
		}
		_ = runtime.Close(t.Context())
	}
}

func TestConfigurationUsesSecretAndEnvironment(t *testing.T) {
	resources := resource.New()
	sections := cfg.NewSections()
	if _, err := resource.Define[cfg.Section](resources, sections); err != nil {
		t.Fatal(err)
	}
	if _, err := resource.Define[cfg.Connection](resources, sections.ConnectionPoint()); err != nil {
		t.Fatal(err)
	}
	if err := Declare(resources); err != nil {
		t.Fatal(err)
	}
	resolved, err := sections.ResolveValues(cfg.Values{ConfigKey: {"enabled": true}}, nil, func(key string) (string, bool) { return "fixture-env-key", key == "TYPESAFE_API_KEY" })
	if err != nil {
		t.Fatal(err)
	}
	config, err := cfg.Get[*Config](resolved, ConfigKey)
	if err != nil || config.APIKey != "fixture-env-key" || config.Model != defaultModel || config.OnError != "block" {
		t.Fatalf("config failed: %v", err)
	}
	view, secrets := sections.View(resolved.Values())
	if _, present := view[ConfigKey]["api_key"]; present || len(secrets[ConfigKey]) != 1 {
		t.Fatal("configuration view exposed or lost secret metadata")
	}
	preserved := sections.Preserve(view, resolved.Values())
	if preserved[ConfigKey]["api_key"] != "fixture-env-key" {
		t.Fatal("configuration roundtrip lost secret")
	}
	for _, bad := range []Config{{Level: "unknown"}, {Timeout: "0s"}, {OnError: "allow"}, {Criteria: map[string]string{"allow": "bad"}}} {
		if bad.validate() == nil {
			t.Error("invalid configuration accepted")
		}
	}
}
