package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/pkg/agent/tmux"
	"github.com/chainreactors/aiscan/pkg/commands"
	"github.com/chainreactors/aiscan/pkg/remotepty"
	"github.com/chainreactors/aiscan/pkg/telemetry"
)

func TestRemoteREPLOpenerUsesRuntimeManagerWithoutProvider(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	option := &cfg.Option{}
	rt, err := NewAgentRuntime(ctx, option, telemetry.NopLogger(), &RuntimeConfig{
		ProviderOptional: true,
		NoOutput:         true,
	})
	if err != nil {
		t.Fatalf("runtime without provider: %v", err)
	}
	defer rt.Close()

	mgr := testRegistryPTYManager(rt.App.Commands)
	if mgr == nil {
		t.Fatal("pty manager unavailable")
	}

	messages := make(chan remotepty.Frame, 64)
	router := remotepty.NewRouter(mgr, remotepty.WithOpener("repl", NewRemoteREPLOpener(rt, mgr)))
	defer router.Close()

	router.Handle(ctx, remotepty.Frame{
		Type:     remotepty.FrameOpen,
		StreamID: "term-repl",
		Kind:     "repl",
		Name:     "remote-repl-test",
	}, func(frame remotepty.Frame) { messages <- frame })
	waitForFrame(t, messages, time.Second, func(frame remotepty.Frame) bool {
		if frame.Type == remotepty.FrameError {
			t.Fatalf("unexpected pty error: %s", frame.Error)
		}
		return frame.Type == remotepty.FrameOpened
	})

	router.Handle(ctx, remotepty.Frame{Type: remotepty.FrameInput, StreamID: "term-repl", Data: []byte("/status\r")}, func(frame remotepty.Frame) {
		messages <- frame
	})
	waitForFrame(t, messages, 3*time.Second, func(frame remotepty.Frame) bool {
		if frame.Type == remotepty.FrameError {
			t.Fatalf("unexpected pty error: %s", frame.Error)
		}
		return frame.Type == remotepty.FrameOutput && strings.Contains(string(frame.Data), "not configured")
	})

	router.Handle(ctx, remotepty.Frame{Type: remotepty.FrameInput, StreamID: "term-repl", Data: []byte("!tmux new-session -d -s webtask echo tmux_remote_ok\r")}, func(frame remotepty.Frame) {
		messages <- frame
	})
	waitForCondition(t, 3*time.Second, func() bool {
		for _, info := range mgr.List() {
			if info.Name == "webtask" {
				return true
			}
		}
		return false
	})
}

func testRegistryPTYManager(reg *commands.CommandRegistry) *tmux.Manager {
	if reg == nil {
		return nil
	}
	tool, ok := reg.GetTool("bash")
	if !ok {
		return nil
	}
	manager, ok := tool.(interface {
		Manager() *tmux.Manager
	})
	if !ok {
		return nil
	}
	return manager.Manager()
}

func waitForCondition(t *testing.T, timeout time.Duration, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !predicate() {
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %s", timeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitForFrame(t *testing.T, ch <-chan remotepty.Frame, timeout time.Duration, match func(remotepty.Frame) bool) remotepty.Frame {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case frame := <-ch:
			if match(frame) {
				return frame
			}
		case <-deadline:
			t.Fatalf("timeout waiting for matching frame")
			return remotepty.Frame{}
		}
	}
}
