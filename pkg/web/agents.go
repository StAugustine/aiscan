package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// AgentCommand is a command sent from web to agent via SSE.
type AgentCommand struct {
	Type    string `json:"type"`
	TaskID  string `json:"task_id,omitempty"`
	Command string `json:"command,omitempty"`
}

// AgentRegisterRequest is sent by the agent to register itself.
type AgentRegisterRequest struct {
	Name     string   `json:"name"`
	Commands []string `json:"commands,omitempty"`
}

// AgentRegisterResponse is returned after registration.
type AgentRegisterResponse struct {
	AgentID string `json:"agent_id"`
}

// AgentOutputMsg is sent by the agent to report streaming output.
type AgentOutputMsg struct {
	AgentID string `json:"agent_id"`
	TaskID  string `json:"task_id"`
	Data    string `json:"data"`
}

// AgentCompleteMsg is sent by the agent when a task finishes.
type AgentCompleteMsg struct {
	AgentID    string          `json:"agent_id"`
	TaskID     string          `json:"task_id"`
	Output     string          `json:"output,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
}

// AgentInfo is the public view of a connected agent.
type AgentInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Commands  []string  `json:"commands,omitempty"`
	Busy      bool      `json:"busy"`
	ConnectAt time.Time `json:"connected_at"`
}

type taskResult struct {
	Output string
	Result json.RawMessage
	Err    string
}

type remoteAgent struct {
	id        string
	name      string
	commands  []string
	connectAt time.Time

	mu    sync.Mutex
	busy  bool
	cmdCh chan AgentCommand
	tasks map[string]chan taskResult
}

func (a *remoteAgent) info() AgentInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	return AgentInfo{
		ID:        a.id,
		Name:      a.name,
		Commands:  a.commands,
		Busy:      a.busy,
		ConnectAt: a.connectAt,
	}
}

// AgentPool manages connected remote aiscan agents via SSE+POST.
type AgentPool struct {
	mu     sync.RWMutex
	agents map[string]*remoteAgent
	hub    *Hub
}

func NewAgentPool(hub *Hub) *AgentPool {
	return &AgentPool{
		agents: make(map[string]*remoteAgent),
		hub:    hub,
	}
}

func (p *AgentPool) Register(name string, commands []string) string {
	agent := &remoteAgent{
		id:        generateID(),
		name:      name,
		commands:  commands,
		connectAt: time.Now(),
		cmdCh:     make(chan AgentCommand, 16),
		tasks:     make(map[string]chan taskResult),
	}
	p.mu.Lock()
	p.agents[agent.id] = agent
	p.mu.Unlock()
	return agent.id
}

func (p *AgentPool) Unregister(id string) {
	p.mu.Lock()
	agent, ok := p.agents[id]
	delete(p.agents, id)
	p.mu.Unlock()
	if ok {
		agent.mu.Lock()
		for _, ch := range agent.tasks {
			close(ch)
		}
		agent.tasks = nil
		agent.mu.Unlock()
	}
}

func (p *AgentPool) get(id string) *remoteAgent {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.agents[id]
}

func (p *AgentPool) List() []AgentInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	infos := make([]AgentInfo, 0, len(p.agents))
	for _, a := range p.agents {
		infos = append(infos, a.info())
	}
	return infos
}

func (p *AgentPool) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.agents)
}

// Pick selects an idle agent, or any agent if none idle.
func (p *AgentPool) Pick() *remoteAgent {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var fallback *remoteAgent
	for _, a := range p.agents {
		a.mu.Lock()
		busy := a.busy
		a.mu.Unlock()
		if !busy {
			return a
		}
		if fallback == nil {
			fallback = a
		}
	}
	return fallback
}

// DispatchCommand sends a command to an agent and returns a channel that
// receives the result when the task completes.
func (p *AgentPool) DispatchCommand(agentID, taskID, command string) (<-chan taskResult, error) {
	agent := p.get(agentID)
	if agent == nil {
		return nil, fmt.Errorf("agent %s not connected", agentID)
	}

	ch := make(chan taskResult, 1)
	agent.mu.Lock()
	agent.tasks[taskID] = ch
	agent.busy = true
	agent.mu.Unlock()

	select {
	case agent.cmdCh <- AgentCommand{Type: "exec", TaskID: taskID, Command: command}:
	default:
		agent.mu.Lock()
		delete(agent.tasks, taskID)
		agent.busy = len(agent.tasks) > 0
		agent.mu.Unlock()
		close(ch)
		return nil, fmt.Errorf("agent %s command channel full", agentID)
	}
	return ch, nil
}

func (p *AgentPool) CancelTask(agentID, taskID string) {
	agent := p.get(agentID)
	if agent == nil {
		return
	}
	select {
	case agent.cmdCh <- AgentCommand{Type: "cancel", TaskID: taskID}:
	default:
	}
}

// HandleOutput processes streaming output from an agent and forwards it to
// the SSE hub for the frontend.
func (p *AgentPool) HandleOutput(agentID, taskID, data string) {
	if p.hub != nil {
		p.hub.Broadcast(taskID, ScanEvent{
			Type:   "progress",
			ScanID: taskID,
			Data:   data,
		})
	}
}

// HandleComplete processes a task completion from an agent.
func (p *AgentPool) HandleComplete(agentID, taskID, output string, result json.RawMessage, errMsg string) {
	agent := p.get(agentID)
	if agent == nil {
		return
	}
	agent.mu.Lock()
	ch, ok := agent.tasks[taskID]
	if ok {
		delete(agent.tasks, taskID)
	}
	agent.busy = len(agent.tasks) > 0
	agent.mu.Unlock()

	if ok && ch != nil {
		ch <- taskResult{Output: output, Result: result, Err: errMsg}
		close(ch)
	}
}

// ServeAgentSSE handles the SSE stream from web to agent. The agent opens
// GET /api/agent/stream?agent_id=xxx and receives commands as SSE events.
func (p *AgentPool) ServeAgentSSE(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent_id")
	agent := p.get(agentID)
	if agent == nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			p.Unregister(agentID)
			return
		case <-ticker.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case cmd := <-agent.cmdCh:
			data, err := json.Marshal(cmd)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", cmd.Type, data)
			flusher.Flush()
		}
	}
}
