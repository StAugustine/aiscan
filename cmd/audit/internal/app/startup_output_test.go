package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/cmd/audit/internal/toolchain"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestStartupFailureHasMachineOutput(t *testing.T) {
	for _, format := range []string{"json", "stream-json"} {
		for _, failure := range []string{"config", "workdir", "task-file", "tools", "report", "parse", "positional"} {
			t.Run(format+"/"+failure, func(t *testing.T) {
				workspace := t.TempDir()
				args := []string{"--output-format", format, "--workdir", workspace, "--data-dir", t.TempDir(), "--provider", "openai", "--api-key", "fixture", "--model", "fixture", "-p", "functional test"}
				ensure := fakeTools
				switch failure {
				case "config":
					args = append(args, "--config", filepath.Join(workspace, "missing.yaml"))
				case "workdir":
					args = append(args, "--workdir", filepath.Join(workspace, "missing"))
				case "task-file":
					args = append(args, "--task-file", "missing-task")
				case "tools":
					ensure = func(*toolchain.Manager, context.Context, io.Writer) ([]toolchain.Status, error) {
						return nil, io.ErrUnexpectedEOF
					}
				case "report":
					args = append(args, "--report-dir", workspace)
				case "positional":
					args = append(args, "unexpected-positional")
				case "parse":
					args = append(args, "--unknown-fixture-option")
				}
				var output strings.Builder
				err := run(t.Context(), args, &output, io.Discard, ensure)
				if err == nil {
					t.Fatal("startup failure accepted")
				}
				if failure == "tools" && !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Fatalf("original error lost: %v", err)
				}
				if format == "json" {
					var result struct {
						Type    string `json:"type"`
						IsError bool   `json:"is_error"`
						Error   struct {
							Message string `json:"message"`
						} `json:"error"`
					}
					if decodeErr := json.Unmarshal([]byte(output.String()), &result); decodeErr != nil {
						t.Fatalf("missing or invalid startup result: %v", decodeErr)
					}
					if result.Type != "result" || !result.IsError || result.Error.Message != err.Error() {
						t.Fatalf("unexpected result: %+v", result)
					}
				} else {
					event := new(aop.Event)
					if decodeErr := protojson.Unmarshal([]byte(output.String()), event); decodeErr != nil {
						t.Fatal(decodeErr)
					}
					if event.GetError().GetMessage() != err.Error() || event.Id == "" || event.Seq != 1 || event.EmittedAt == nil || event.SessionId != "" {
						t.Fatalf("unexpected startup event: %s", output.String())
					}
				}
			})
		}
	}
}

func TestStartupOutputFailurePreservesBothErrors(t *testing.T) {
	writeErr := errors.New("startup output failed")
	err := run(t.Context(), []string{"--json", "--unknown-fixture-option"}, failedOutput{writeErr}, io.Discard, fakeTools)
	if !errors.Is(err, writeErr) || !strings.Contains(err.Error(), "unknown-fixture-option") {
		t.Fatalf("errors lost: %v", err)
	}
}
