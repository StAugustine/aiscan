package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockRuntime implements CallbackRuntime for testing.
type mockRuntime struct {
	commands []string
}

func (m *mockRuntime) CommandNames() []string { return m.commands }

func (m *mockRuntime) ExecuteCommand(_ context.Context, cmdLine string, stream io.Writer) (string, json.RawMessage, error) {
	_, _ = stream.Write([]byte("progress: executing " + cmdLine + "\n"))
	result, _ := json.Marshal(map[string]string{"status": "done"})
	return "completed " + cmdLine, result, nil
}

func TestAgentPoolRegisterAndList(t *testing.T) {
	pool := NewAgentPool(NewHub())
	id := pool.Register("test-agent", []string{"scan", "gogo"})
	if id == "" {
		t.Fatal("expected non-empty agent id")
	}

	agents := pool.List()
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].Name != "test-agent" {
		t.Fatalf("expected name test-agent, got %s", agents[0].Name)
	}
	if agents[0].ID != id {
		t.Fatalf("expected id %s, got %s", id, agents[0].ID)
	}

	pool.Unregister(id)
	if pool.Count() != 0 {
		t.Fatal("expected 0 agents after unregister")
	}
}

func TestAgentPoolDispatchAndComplete(t *testing.T) {
	hub := NewHub()
	pool := NewAgentPool(hub)
	id := pool.Register("worker", []string{"scan"})

	// Subscribe to hub to verify progress forwarding
	progressCh, unsub := hub.Subscribe("task-1")
	defer unsub()

	resultCh, err := pool.DispatchCommand(id, "task-1", "scan -i 1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}

	// Drain the command from agent's channel
	agent := pool.get(id)
	cmd := <-agent.cmdCh
	if cmd.Type != "exec" || cmd.TaskID != "task-1" || cmd.Command != "scan -i 1.2.3.4" {
		t.Fatalf("unexpected command: %+v", cmd)
	}

	// Simulate agent output
	pool.HandleOutput(id, "task-1", "scanning port 80")

	// Check progress was forwarded to hub
	select {
	case evt := <-progressCh:
		if evt.Type != "progress" || evt.Data != "scanning port 80" {
			t.Fatalf("unexpected progress event: %+v", evt)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for progress event")
	}

	// Simulate completion
	result, _ := json.Marshal(map[string]int{"ports": 3})
	pool.HandleComplete(id, "task-1", "done", result, "")

	select {
	case res := <-resultCh:
		if res.Err != "" {
			t.Fatalf("unexpected error: %s", res.Err)
		}
		if res.Output != "done" {
			t.Fatalf("expected output 'done', got %q", res.Output)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestAgentSSEAndHTTPRoundTrip(t *testing.T) {
	hub := NewHub()
	pool := NewAgentPool(hub)

	// Create a test server with agent routes
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		segments := pathSegments(r.URL.Path)
		if len(segments) < 3 || segments[0] != "api" || segments[1] != "agent" {
			http.Error(w, "not found", 404)
			return
		}
		switch segments[2] {
		case "register":
			var req AgentRegisterRequest
			json.NewDecoder(r.Body).Decode(&req)
			id := pool.Register(req.Name, req.Commands)
			writeJSON(w, http.StatusOK, AgentRegisterResponse{AgentID: id})
		case "stream":
			pool.ServeAgentSSE(w, r)
		case "output":
			var msg AgentOutputMsg
			json.NewDecoder(r.Body).Decode(&msg)
			pool.HandleOutput(msg.AgentID, msg.TaskID, msg.Data)
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		case "complete":
			var msg AgentCompleteMsg
			json.NewDecoder(r.Body).Decode(&msg)
			pool.HandleComplete(msg.AgentID, msg.TaskID, msg.Output, msg.Result, msg.Error)
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		}
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rt := &mockRuntime{commands: []string{"scan", "echo"}}

	// Start callback client in background
	done := make(chan error, 1)
	go func() {
		done <- RunCallback(ctx, CallbackConfig{
			ServerURL: srv.URL,
			Name:      "e2e-agent",
			Runtime:   rt,
		})
	}()

	// Wait for agent to register
	var agentID string
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		agents := pool.List()
		if len(agents) > 0 {
			agentID = agents[0].ID
			break
		}
	}
	if agentID == "" {
		t.Fatal("agent did not register within 2s")
	}

	// Subscribe to hub to capture progress
	progressCh, unsub := hub.Subscribe("test-task-1")
	defer unsub()

	// Dispatch a command
	resultCh, err := pool.DispatchCommand(agentID, "test-task-1", "scan -i 10.0.0.1")
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}

	// Wait for agent to complete and post results
	select {
	case res := <-resultCh:
		if res.Err != "" {
			t.Fatalf("task error: %s", res.Err)
		}
		if !strings.Contains(res.Output, "scan -i 10.0.0.1") {
			t.Fatalf("expected output to contain command, got %q", res.Output)
		}
		if len(res.Result) == 0 {
			t.Fatal("expected structured result")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for task result")
	}

	// Check that progress was forwarded to hub
	select {
	case evt := <-progressCh:
		if evt.Type != "progress" || !strings.Contains(evt.Data, "scan -i 10.0.0.1") {
			t.Fatalf("unexpected progress event: %+v", evt)
		}
	default:
		// progress may have been consumed already, that's ok
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("callback client did not shut down")
	}
}

func TestAgentPick(t *testing.T) {
	pool := NewAgentPool(NewHub())
	if pool.Pick() != nil {
		t.Fatal("expected nil when no agents")
	}

	id1 := pool.Register("a1", nil)
	id2 := pool.Register("a2", nil)

	// Both idle, should pick one
	picked := pool.Pick()
	if picked == nil {
		t.Fatal("expected a pick")
	}

	// Make one busy
	a1 := pool.get(id1)
	a1.mu.Lock()
	a1.busy = true
	a1.mu.Unlock()

	// Should pick the idle one
	picked = pool.Pick()
	if picked == nil || picked.id != id2 {
		t.Fatalf("expected to pick idle agent %s", id2)
	}

	pool.Unregister(id1)
	pool.Unregister(id2)
}
