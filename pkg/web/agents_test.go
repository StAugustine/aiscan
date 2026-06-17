package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func dialAgent(t *testing.T, srv *httptest.Server, name string, commands []string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/agent/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	reg, _ := json.Marshal(map[string]any{"name": name, "commands": commands})
	conn.WriteJSON(WSMessage{Type: "register", Payload: reg})
	var ack WSMessage
	conn.ReadJSON(&ack)
	if ack.Type != "connected" {
		t.Fatalf("expected connected, got %s", ack.Type)
	}
	return conn
}

func setupTestServer(t *testing.T) (*httptest.Server, *AgentPool) {
	t.Helper()
	hub := NewHub()
	pool := NewAgentPool(hub)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/agent/ws", pool.HandleWS)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, pool
}

func TestWSRegisterAndList(t *testing.T) {
	srv, pool := setupTestServer(t)
	conn := dialAgent(t, srv, "test-agent", []string{"scan", "gogo"})
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)
	agents := pool.List()
	if len(agents) != 1 || agents[0].Name != "test-agent" {
		t.Fatalf("expected 1 agent named test-agent, got %+v", agents)
	}
}

func TestWSDispatchAndComplete(t *testing.T) {
	srv, pool := setupTestServer(t)
	conn := dialAgent(t, srv, "worker", []string{"scan"})
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)
	agentID := pool.List()[0].ID

	progressCh, unsub := pool.hub.Subscribe("task-1")
	defer unsub()

	resultCh, err := pool.DispatchCommand(agentID, "task-1", "scan -i 1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}

	var cmd WSMessage
	conn.ReadJSON(&cmd)
	if cmd.Type != "exec" || cmd.Data != "scan -i 1.2.3.4" {
		t.Fatalf("unexpected: %+v", cmd)
	}

	conn.WriteJSON(WSMessage{Type: "output", TaskID: "task-1", Data: "port 80 open"})
	select {
	case evt := <-progressCh:
		if !strings.Contains(string(evt.Data), "port 80 open") {
			t.Fatalf("unexpected progress: %s", evt.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}

	result, _ := json.Marshal(map[string]int{"ports": 3})
	conn.WriteJSON(WSMessage{Type: "complete", TaskID: "task-1", Data: "done", Payload: result})
	select {
	case res := <-resultCh:
		if res.Err != "" || res.Output != "done" {
			t.Fatalf("unexpected result: %+v", res)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestWSPick(t *testing.T) {
	_, pool := setupTestServer(t)
	if pool.Pick() != nil {
		t.Fatal("expected nil when no agents")
	}
}

func TestWSTelemetryForwarding(t *testing.T) {
	srv, pool := setupTestServer(t)
	conn := dialAgent(t, srv, "tele-agent", []string{"scan"})
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	progressCh, unsub := pool.hub.Subscribe("task-2")
	defer unsub()

	conn.WriteJSON(WSMessage{Type: "agent.turn_start", TaskID: "task-2", Data: "turn 1"})

	select {
	case evt := <-progressCh:
		if !strings.Contains(string(evt.Data), "turn 1") {
			t.Fatalf("unexpected: %s", evt.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}
