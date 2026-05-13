package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBashToolDefinition(t *testing.T) {
	bt := NewBashTool("", 10, nil)
	if bt.Name() != "bash" {
		t.Fatalf("expected name bash, got %s", bt.Name())
	}
	def := bt.Definition()
	if def.Function.Name != "bash" {
		t.Fatalf("expected function name bash, got %s", def.Function.Name)
	}
	params, ok := def.Function.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in parameters")
	}
	for _, field := range []string{"command", "background"} {
		if _, ok := params[field]; !ok {
			t.Errorf("expected %s in properties", field)
		}
	}
}

func TestBashToolInvalidJSON(t *testing.T) {
	bt := NewBashTool("", 10, nil)
	_, err := bt.Execute(context.Background(), `not json`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestBashToolEmptyCommand(t *testing.T) {
	bt := NewBashTool("", 10, nil)
	_, err := bt.Execute(context.Background(), `{"command":""}`)
	if err == nil {
		t.Fatal("expected error for empty command")
	}
	if !strings.Contains(err.Error(), "empty command") {
		t.Errorf("expected 'empty command' error, got: %s", err.Error())
	}
}

func TestBashToolEcho(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	bt := NewBashTool("", 10, nil)
	result, err := bt.Execute(context.Background(), `{"command":"echo hello"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(result) != "hello" {
		t.Fatalf("expected 'hello', got %q", result)
	}
}

func TestBashToolBackground(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	bt := NewBashTool("", 10, nil)

	// Start a sleep in background — should return immediately
	start := time.Now()
	result, err := bt.Execute(context.Background(), `{"command":"sleep 30","background":true}`)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Background process started with PID") {
		t.Fatalf("expected background PID message, got %q", result)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("background should return immediately, took %v", elapsed)
	}

	// Extract PID from result
	var pid int
	if _, scanErr := fmt.Sscanf(result, "Background process started with PID %d", &pid); scanErr != nil {
		t.Fatalf("could not parse PID from result: %q", result)
	}
	if pid <= 0 {
		t.Fatalf("expected positive PID, got %d", pid)
	}

	// Verify the process group is running.
	if err := syscall.Kill(-pid, syscall.Signal(0)); err != nil {
		t.Fatalf("process group %d should be running: %v", pid, err)
	}

	// Kill the whole process group.
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		t.Fatalf("could not kill process group %d: %v", pid, err)
	}

	if !waitForCondition(2*time.Second, func() bool {
		return !isBackgroundTracked(bt, pid)
	}) {
		t.Fatalf("process %d still tracked after process group kill", pid)
	}
}

func TestBashToolBackgroundProcessCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	bt := NewBashTool("", 10, nil)

	// Start a process that survives the startup window, then exits naturally.
	result, err := bt.Execute(context.Background(), `{"command":"sleep 1","background":true}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var pid int
	if _, scanErr := fmt.Sscanf(result, "Background process started with PID %d", &pid); scanErr != nil {
		t.Fatalf("could not parse PID from result: %q", result)
	}

	if !isBackgroundTracked(bt, pid) {
		t.Fatalf("process %d should be tracked after startup", pid)
	}

	if !waitForCondition(2*time.Second, func() bool {
		return !isBackgroundTracked(bt, pid)
	}) {
		t.Errorf("process %d should have been cleaned up from map after exit", pid)
	}
}

func TestBashToolBackgroundReportsStartupFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	bt := NewBashTool("", 10, nil)

	_, err := bt.Execute(context.Background(), `{"command":"printf failed >&2; exit 42","background":true}`)
	if err == nil {
		t.Fatal("expected startup failure")
	}
	if !strings.Contains(err.Error(), "background process exited during startup") {
		t.Fatalf("expected startup failure message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "[stderr] failed") {
		t.Fatalf("expected captured stderr, got: %v", err)
	}
}

func TestBashToolBackgroundHonorsCanceledContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	bt := NewBashTool("", 10, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := bt.Execute(ctx, `{"command":"sleep 30","background":true}`)
	if err == nil {
		t.Fatal("expected canceled context error")
	}
	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("expected context canceled error, got: %v", err)
	}
}

func TestBashToolCloseStopsBackgroundProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	bt := NewBashTool("", 10, nil)

	result, err := bt.Execute(context.Background(), `{"command":"sleep 30","background":true}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pid := mustParseBackgroundPID(t, result)

	bt.Close()

	if isBackgroundTracked(bt, pid) {
		t.Fatalf("process %d should not be tracked after Close", pid)
	}
	if err := syscall.Kill(-pid, syscall.Signal(0)); err == nil {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		t.Fatalf("process group %d should not be running after Close", pid)
	}
}

func TestBashToolBackgroundProcessGroupKillStopsChildren(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	dir := t.TempDir()
	childPIDFile := filepath.Join(dir, "child.pid")
	bt := NewBashTool("", 10, nil)

	command := fmt.Sprintf("sh -c 'sleep 30 & echo $! > %s; wait'", childPIDFile)
	result, err := bt.Execute(context.Background(), fmt.Sprintf(`{"command":%q,"background":true}`, command))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pid := mustParseBackgroundPID(t, result)

	var childPID int
	if !waitForCondition(time.Second, func() bool {
		data, readErr := os.ReadFile(childPIDFile)
		if readErr != nil {
			return false
		}
		parsed, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if parseErr != nil {
			return false
		}
		childPID = parsed
		return childPID > 0
	}) {
		t.Fatalf("child pid was not written")
	}

	if err := syscall.Kill(childPID, syscall.Signal(0)); err != nil {
		t.Fatalf("child process %d should be running before group kill: %v", childPID, err)
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		t.Fatalf("could not kill process group %d: %v", pid, err)
	}
	if !waitForCondition(2*time.Second, func() bool {
		return syscall.Kill(childPID, syscall.Signal(0)) != nil
	}) {
		_ = syscall.Kill(childPID, syscall.SIGKILL)
		t.Fatalf("child process %d still running after process group kill", childPID)
	}
}

func TestFirstCommandToken(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"echo hello", "echo"},
		{"  gogo -i 10.0.0.1", "gogo"},
		{"'quoted cmd' args", "quoted cmd"},
		{`"double quoted" args`, "double quoted"},
		{"scan -i 192.168.1.0/24", "scan"},
		{"", ""},
	}
	for _, tt := range tests {
		got := firstCommandToken(tt.input)
		if got != tt.want {
			t.Errorf("firstCommandToken(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func mustParseBackgroundPID(t *testing.T, result string) int {
	t.Helper()
	var pid int
	if _, scanErr := fmt.Sscanf(result, "Background process started with PID %d", &pid); scanErr != nil {
		t.Fatalf("could not parse PID from result: %q", result)
	}
	if pid <= 0 {
		t.Fatalf("expected positive PID, got %d", pid)
	}
	return pid
}

func isBackgroundTracked(bt *BashTool, pid int) bool {
	bt.bgMu.Lock()
	defer bt.bgMu.Unlock()
	_, tracked := bt.bgProcesses[pid]
	return tracked
}

func waitForCondition(timeout time.Duration, condition func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return condition()
}
