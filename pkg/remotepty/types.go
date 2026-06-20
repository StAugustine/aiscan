package remotepty

import (
	"context"
	"time"

	"github.com/chainreactors/aiscan/pkg/agent/tmux"
)

type FrameType string

const (
	FrameOpen     FrameType = "open"
	FrameOpened   FrameType = "opened"
	FrameAttach   FrameType = "attach"
	FrameAttached FrameType = "attached"
	FrameInput    FrameType = "input"
	FrameOutput   FrameType = "output"
	FrameResize   FrameType = "resize"
	FrameDetach   FrameType = "detach"
	FrameDetached FrameType = "detached"
	FrameKill     FrameType = "kill"
	FrameList     FrameType = "list"
	FrameSessions FrameType = "sessions"
	FrameClosed   FrameType = "closed"
	FrameError    FrameType = "error"
)

type Frame struct {
	Type      FrameType
	StreamID  string
	SessionID string
	Kind      string
	Name      string
	Command   string
	Args      []string
	Data      []byte
	Cols      int
	Rows      int
	Bytes     int
	Offset    int64
	Singleton bool
	Error     string
	State     tmux.State
	ExitCode  int
	Session   *tmux.Info
	Sessions  []tmux.Info
}

type OpenSpec struct {
	Kind    string
	Name    string
	Command string
	Args    []string
	Cols    int
	Rows    int
}

type ResizeFunc func(cols, rows int)

type OpenResult struct {
	Info   tmux.Info
	Resize ResizeFunc
}

type OpenFunc func(ctx context.Context, spec OpenSpec) (OpenResult, error)

type SessionManager interface {
	List() []tmux.Info
	Get(id string) (tmux.Info, bool)
	Write(id string, data []byte) error
	Resize(id string, cols, rows int) error
	Kill(id string) error
	SnapshotBytes(id string, n int) ([]byte, int64, error)
	MonitorFrom(ctx context.Context, id string, offset int64, interval time.Duration, push func([]byte)) error
	Wait(ctx context.Context, id string, timeout time.Duration) (tmux.Info, error)
}
