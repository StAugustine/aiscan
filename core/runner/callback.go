package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chainreactors/aiscan/core/eventbus"
	"github.com/chainreactors/aiscan/core/output"
	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/commands"
	"github.com/gorilla/websocket"
)

// wsMsg mirrors pkg/wsMsg to avoid an import cycle.
type wsMsg struct {
	Type    string          `json:"type"`
	TaskID  string          `json:"task_id,omitempty"`
	Data    string          `json:"data,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// RunWebCallback connects to the web server via WebSocket and enters a
// loop receiving commands and forwarding agent events. It reconnects on
// disconnect until ctx is cancelled.
func RunWebCallback(ctx context.Context, serverURL, name string, reg *commands.CommandRegistry, bus *eventbus.Bus[agent.Event]) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		err := runCallbackOnce(ctx, serverURL, name, reg, bus)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(3 * time.Second):
			}
		}
	}
}

func runCallbackOnce(ctx context.Context, serverURL, name string, reg *commands.CommandRegistry, bus *eventbus.Bus[agent.Event]) error {
	wsURL := httpToWS(serverURL) + "/api/agent/ws"
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	defer conn.Close()

	sendCh := make(chan wsMsg, 64)
	done := make(chan struct{})
	defer close(done)

	send := func(m wsMsg) {
		select {
		case sendCh <- m:
		case <-done:
		}
	}

	// Register.
	regPayload, _ := json.Marshal(map[string]any{
		"name":     name,
		"commands": reg.Names(),
	})
	if err := conn.WriteJSON(wsMsg{Type: "register", Payload: regPayload}); err != nil {
		return fmt.Errorf("register: %w", err)
	}

	// Read connected ack.
	var ack wsMsg
	if err := conn.ReadJSON(&ack); err != nil || ack.Type != "connected" {
		return fmt.Errorf("expected connected ack")
	}

	// Write goroutine.
	go func() {
		for {
			select {
			case msg, ok := <-sendCh:
				if !ok {
					return
				}
				_ = conn.WriteJSON(msg)
			case <-ctx.Done():
				conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			case <-done:
				return
			}
		}
	}()

	// Context cancellation: unblock ReadJSON.
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()

	// Forward agent events to WebSocket.
	var activeMu sync.Mutex
	activeTasks := make(map[string]struct{})
	if bus != nil {
		unsub := bus.Subscribe(func(e agent.Event) {
			payload, _ := json.Marshal(e)
			data := agentEventSummary(e)
			if data == "" {
				data = string(payload)
			}
			activeMu.Lock()
			taskIDs := make([]string, 0, len(activeTasks))
			for taskID := range activeTasks {
				taskIDs = append(taskIDs, taskID)
			}
			activeMu.Unlock()
			for _, taskID := range taskIDs {
				send(wsMsg{
					Type:    "agent." + string(e.Type),
					TaskID:  taskID,
					Data:    data,
					Payload: payload,
				})
			}
		})
		defer unsub()
	}

	// Task management.
	var taskMu sync.Mutex
	taskCancels := make(map[string]context.CancelFunc)

	for {
		var msg wsMsg
		if err := conn.ReadJSON(&msg); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}

		switch msg.Type {
		case "exec":
			taskCtx, cancel := context.WithCancel(ctx)
			taskMu.Lock()
			taskCancels[msg.TaskID] = cancel
			taskMu.Unlock()
			go func(m wsMsg, tCtx context.Context, tCancel context.CancelFunc) {
				defer tCancel()
				activeMu.Lock()
				activeTasks[m.TaskID] = struct{}{}
				activeMu.Unlock()
				defer func() {
					activeMu.Lock()
					delete(activeTasks, m.TaskID)
					activeMu.Unlock()
					taskMu.Lock()
					delete(taskCancels, m.TaskID)
					taskMu.Unlock()
				}()
				execCommand(tCtx, m.TaskID, m.Data, reg, send)
			}(msg, taskCtx, cancel)

		case "cancel":
			taskMu.Lock()
			if cancel, ok := taskCancels[msg.TaskID]; ok {
				cancel()
			}
			taskMu.Unlock()
		}
	}
}

func execCommand(ctx context.Context, taskID, cmdLine string, reg *commands.CommandRegistry, send func(wsMsg)) {
	tokens, err := commands.SplitCommandLine(cmdLine)
	if err != nil {
		send(wsMsg{Type: "error", TaskID: taskID, Data: err.Error()})
		return
	}
	if len(tokens) == 0 {
		send(wsMsg{Type: "error", TaskID: taskID, Data: "empty command"})
		return
	}

	writer := &wsStreamWriter{taskID: taskID, sendFn: send}

	// Try structured execution (scan commands return *output.Result).
	if cmd, ok := reg.Get(tokens[0]); ok {
		if sc, ok := cmd.(interface {
			ExecuteStructured(ctx context.Context, args []string, stream io.Writer) (string, *output.Result, error)
		}); ok {
			out, result, err := sc.ExecuteStructured(ctx, tokens[1:], writer)
			if err != nil {
				send(wsMsg{Type: "error", TaskID: taskID, Data: err.Error()})
				return
			}
			var payload json.RawMessage
			if result != nil {
				payload, _ = json.Marshal(result)
			}
			send(wsMsg{Type: "complete", TaskID: taskID, Data: out, Payload: payload})
			return
		}
	}

	out, err := reg.ExecuteArgsStreaming(ctx, tokens, writer)
	if err != nil {
		send(wsMsg{Type: "error", TaskID: taskID, Data: err.Error()})
		return
	}
	send(wsMsg{Type: "complete", TaskID: taskID, Data: out})
}

func agentEventSummary(e agent.Event) string {
	switch e.Type {
	case agent.EventToolExecutionStart:
		return e.ToolName
	case agent.EventToolExecutionEnd:
		if e.IsError {
			return e.ToolName + " error"
		}
		return e.ToolName + " done"
	case agent.EventTurnStart:
		return fmt.Sprintf("turn %d", e.Turn)
	case agent.EventTurnEnd:
		if e.Usage != nil {
			return fmt.Sprintf("turn %d tokens=%d", e.Turn, e.Usage.TotalTokens)
		}
		return fmt.Sprintf("turn %d", e.Turn)
	default:
		return ""
	}
}

type wsStreamWriter struct {
	taskID string
	sendFn func(wsMsg)
	buf    []byte
}

func (w *wsStreamWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		line := string(w.buf[:idx])
		w.buf = w.buf[idx+1:]
		if strings.TrimSpace(line) == "" {
			continue
		}
		w.sendFn(wsMsg{Type: "output", TaskID: w.taskID, Data: line})
	}
	return len(p), nil
}

func httpToWS(rawURL string) string {
	u, err := url.Parse(strings.TrimRight(rawURL, "/"))
	if err != nil {
		return rawURL
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	return u.String()
}

// DeriveWebURL extracts the web server base URL from an IOA URL.
func DeriveWebURL(ioaURL string) string {
	u, err := url.Parse(ioaURL)
	if err != nil {
		return ""
	}
	u.User = nil
	u.Path = strings.TrimRight(u.Path, "/")
	u.Path = strings.TrimSuffix(u.Path, "/ioa")
	u.Path = strings.TrimSuffix(u.Path, "/")
	if u.Host == "" {
		return ""
	}
	return u.String()
}
