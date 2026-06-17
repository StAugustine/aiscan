package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// CallbackRuntime is the subset of runner that the callback client needs.
type CallbackRuntime interface {
	CommandNames() []string
	ExecuteCommand(ctx context.Context, cmdLine string, stream io.Writer) (string, json.RawMessage, error)
}

// CallbackConfig configures the agent callback client.
type CallbackConfig struct {
	ServerURL string
	Name      string
	Runtime   CallbackRuntime
}

// RunCallback connects to the web server and enters a loop receiving
// commands via SSE and posting results back via HTTP.
func RunCallback(ctx context.Context, cfg CallbackConfig) error {
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		err := runCallbackOnce(ctx, cfg)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			// Reconnect after delay
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(3 * time.Second):
			}
		}
	}
}

func runCallbackOnce(ctx context.Context, cfg CallbackConfig) error {
	serverURL := strings.TrimRight(cfg.ServerURL, "/")
	client := &http.Client{Timeout: 0}

	// 1. Register
	regBody, _ := json.Marshal(AgentRegisterRequest{
		Name:     cfg.Name,
		Commands: cfg.Runtime.CommandNames(),
	})
	regReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		serverURL+"/api/agent/register", bytes.NewReader(regBody))
	if err != nil {
		return err
	}
	regReq.Header.Set("Content-Type", "application/json")
	regResp, err := client.Do(regReq)
	if err != nil {
		return fmt.Errorf("register: %w", err)
	}
	defer regResp.Body.Close()
	if regResp.StatusCode != http.StatusOK && regResp.StatusCode != http.StatusCreated {
		return fmt.Errorf("register: status %d", regResp.StatusCode)
	}
	var regResult AgentRegisterResponse
	if err := json.NewDecoder(regResp.Body).Decode(&regResult); err != nil {
		return fmt.Errorf("register decode: %w", err)
	}
	agentID := regResult.AgentID

	// 2. Open SSE stream
	sseReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/api/agent/stream?agent_id=%s", serverURL, agentID), nil)
	if err != nil {
		return err
	}
	sseReq.Header.Set("Accept", "text/event-stream")
	sseResp, err := client.Do(sseReq)
	if err != nil {
		return fmt.Errorf("sse connect: %w", err)
	}
	defer sseResp.Body.Close()

	// 3. Read SSE events
	var taskMu sync.Mutex
	taskCancels := make(map[string]context.CancelFunc)

	scanner := bufio.NewScanner(sseResp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var eventType, eventData string

	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			eventData = strings.TrimPrefix(line, "data: ")
			continue
		}
		if line == "" && eventType != "" {
			switch eventType {
			case "exec":
				var cmd AgentCommand
				if json.Unmarshal([]byte(eventData), &cmd) == nil {
					taskCtx, cancel := context.WithCancel(ctx)
					taskMu.Lock()
					taskCancels[cmd.TaskID] = cancel
					taskMu.Unlock()
					go func(c AgentCommand, tCtx context.Context, tCancel context.CancelFunc) {
						defer tCancel()
						defer func() {
							taskMu.Lock()
							delete(taskCancels, c.TaskID)
							taskMu.Unlock()
						}()
						executeAndReport(tCtx, serverURL, agentID, c.TaskID, c.Command, cfg.Runtime)
					}(cmd, taskCtx, cancel)
				}
			case "cancel":
				var cmd AgentCommand
				if json.Unmarshal([]byte(eventData), &cmd) == nil {
					taskMu.Lock()
					if cancel, ok := taskCancels[cmd.TaskID]; ok {
						cancel()
					}
					taskMu.Unlock()
				}
			}
			eventType = ""
			eventData = ""
		}
	}
	return scanner.Err()
}

func executeAndReport(ctx context.Context, serverURL, agentID, taskID, command string, rt CallbackRuntime) {
	writer := &callbackStreamWriter{
		serverURL: serverURL,
		agentID:   agentID,
		taskID:    taskID,
	}

	output, result, err := rt.ExecuteCommand(ctx, command, writer)

	msg := AgentCompleteMsg{
		AgentID: agentID,
		TaskID:  taskID,
		Output:  output,
		Result:  result,
	}
	if err != nil {
		msg.Error = err.Error()
	}

	body, _ := json.Marshal(msg)
	postCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(postCtx, http.MethodPost,
		serverURL+"/api/agent/complete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, postErr := http.DefaultClient.Do(req)
	if postErr == nil {
		resp.Body.Close()
	}
}

// callbackStreamWriter posts each line of output back to the web server.
type callbackStreamWriter struct {
	serverURL string
	agentID   string
	taskID    string
	buf       []byte
}

func (w *callbackStreamWriter) Write(p []byte) (int, error) {
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
		w.postOutput(line)
	}
	return len(p), nil
}

func (w *callbackStreamWriter) postOutput(data string) {
	msg := AgentOutputMsg{AgentID: w.agentID, TaskID: w.taskID, Data: data}
	body, _ := json.Marshal(msg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		w.serverURL+"/api/agent/output", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}
