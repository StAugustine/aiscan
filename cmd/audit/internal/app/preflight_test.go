package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/cmd/audit/internal/toolchain"
)

func TestInvalidInvocationDoesNotPrepareTools(t *testing.T) {
	for _, args := range [][]string{
		{"--server-url", "http://127.0.0.1:1", "-p", "local task"},
		{"--timeout", "-1", "-p", "fixture"},
		{"--bash-timeout", "0", "-p", "fixture"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			calls := 0
			args = append(args, "--workdir", t.TempDir(), "--data-dir", t.TempDir(), "--provider", "openai", "--base-url", "http://127.0.0.1:1", "--api-key", "fixture", "--model", "fixture")
			err := run(t.Context(), args, io.Discard, io.Discard, func(*toolchain.Manager, context.Context, io.Writer) ([]toolchain.Status, error) {
				calls++
				return nil, io.ErrUnexpectedEOF
			})
			if err == nil || calls != 0 {
				t.Fatalf("err=%v preparation calls=%d", err, calls)
			}
		})
	}
}

func TestDoctorHasSameMeaningBeforeAndAfterGlobalFlags(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("CYBER_PROVIDER", "unsupported-fixture-provider")
	data := t.TempDir()
	for _, args := range [][]string{{"doctor", "--data-dir", data}, {"--data-dir", data, "doctor"}} {
		var out strings.Builder
		err := Run(t.Context(), args, &out, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "required tools unavailable") || !strings.Contains(out.String(), "osv-scanner") {
			t.Fatalf("%v: %v output=%s", args, err, out.String())
		}
	}
}

func TestDoctorJSONReportsToolFailure(t *testing.T) {
	t.Setenv("PATH", "")
	var output strings.Builder
	err := Run(t.Context(), []string{"--data-dir", t.TempDir(), "doctor", "--json"}, &output, io.Discard)
	var result struct {
		Tools   []toolchain.Status `json:"tools"`
		IsError bool               `json:"is_error"`
	}
	if decodeErr := json.Unmarshal([]byte(output.String()), &result); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if err == nil || !result.IsError || len(result.Tools) == 0 {
		t.Fatalf("err=%v result=%+v", err, result)
	}
}

func TestToolDispatchDoesNotInterpretOptionValuesAsCommands(t *testing.T) {
	for _, args := range [][]string{{"-p", "doctor"}, {"--task-file", "tools"}, {"--data-dir", "doctor", "-p", "fixture"}} {
		if got := toolCommandFirst(args); strings.Join(got, "\x00") != strings.Join(args, "\x00") {
			t.Fatalf("rewrote %v to %v", args, got)
		}
	}
}

func TestToolInstallHelpAcceptsLeadingGlobalOptions(t *testing.T) {
	var output strings.Builder
	err := Run(t.Context(), []string{"--data-dir", t.TempDir(), "tools", "install", "--help"}, &output, io.Discard)
	if err == nil || !strings.Contains(output.String(), "Prepare required tools") {
		t.Fatalf("err=%v output=%s", err, output.String())
	}
}

func TestDoctorReturnsOutputFailure(t *testing.T) {
	t.Setenv("PATH", "")
	for _, format := range []string{"text", "json"} {
		writeErr := errors.New("doctor output failed")
		err := Run(t.Context(), []string{"doctor", "--data-dir", t.TempDir(), "--output-format", format}, failedOutput{writeErr}, io.Discard)
		if !errors.Is(err, writeErr) {
			t.Fatalf("format=%s error=%v", format, err)
		}
	}
}
