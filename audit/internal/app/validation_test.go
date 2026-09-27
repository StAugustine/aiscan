package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chainreactors/cyber/audit/internal/toolchain"
	coretool "github.com/chainreactors/cyber/core/tool"
	flags "github.com/jessevdk/go-flags"
)

func TestHelpIsVisibleWithoutModelOrToolStartup(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"doctor", "--help"}, {"tools", "install", "--help"}, {"validate", "--help"}} {
		var output strings.Builder
		err := run(t.Context(), args, &output, io.Discard, func(*toolchain.Manager, context.Context, io.Writer) ([]toolchain.Status, error) {
			t.Error("help provisioned tools")
			return nil, nil
		})
		var flagErr *flags.Error
		if err != nil && !(errors.As(err, &flagErr) && flagErr.Type == flags.ErrHelp) {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), "cyber-audit") || !strings.Contains(output.String(), "validate") {
			t.Fatalf("missing help for %v", args)
		}
		if args[0] == "--help" && !strings.Contains(output.String(), "CYBER_API_KEY") {
			t.Fatal("help omits shared credentials")
		}
	}
}

func TestValidationEntrypointsUseFinalReportContract(t *testing.T) {
	r, err := newReport(t.Context(), t.TempDir(), "", "fixture", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	completeReport(t, r)
	metadata, err := os.ReadFile(filepath.Join(r.Directory, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []bool{false, true} {
		if invalid {
			body, _ := os.ReadFile(filepath.Join(r.Directory, "coverage.json"))
			body = []byte(strings.ReplaceAll(string(body), `"completed"`, `"reused"`))
			if err := os.WriteFile(filepath.Join(r.Directory, "coverage.json"), body, 0600); err != nil {
				t.Fatal(err)
			}
		}
		var output strings.Builder
		want := r.validate(t.Context())
		cliErr := run(t.Context(), []string{"validate", r.Directory}, &output, io.Discard, func(*toolchain.Manager, context.Context, io.Writer) ([]toolchain.Status, error) {
			t.Error("validation provisioned tools")
			return nil, nil
		})
		_, commandErr := reportCommand(r.Directory).Run(t.Context(), &coretool.Execution{Args: []string{"validate"}, Stdout: &output})
		for _, got := range []error{cliErr, commandErr} {
			if (got != nil) != invalid || got != nil && got.Error() != want.Error() {
				t.Fatalf("validation=%v want=%v", got, want)
			}
		}
	}
	stored, _ := os.ReadFile(filepath.Join(r.Directory, "run.json"))
	if string(stored) != string(metadata) {
		t.Fatal("read-only validation changed run metadata")
	}
}

func TestOneShotReportRepairUsesSameContextAndHasLimit(t *testing.T) {
	for _, problem := range []string{"status", "evidence", "persistent"} {
		t.Run(problem, func(t *testing.T) {
			workspace := t.TempDir()
			reportDir := filepath.Join(workspace, "report")
			valid := `{"reviewed":true,"scope":"functional fixture","examined":["fixture.txt"],"excluded":[],"unsupported":[],"unresolved":[],"checks":[{"tool":"osv-scanner","status":"not_applicable","evidence":"No manifests"},{"tool":"proton","status":"completed","evidence":"raw/proton.jsonl"}]}`
			invalid, wantError := strings.Replace(valid, `"completed"`, `"reused"`, 1), "invalid check status"
			if problem == "evidence" {
				invalid = strings.Replace(valid, "raw/proton.jsonl", "raw/proton.jsonl (reused output)", 1)
				wantError = "missing evidence"
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				body, _ := io.ReadAll(req.Body)
				if !strings.Contains(string(body), "cyber-audit") {
					textReply(w, "pong")
					return
				}
				n := requests.Add(1)
				if n == 1 {
					for name, content := range map[string]string{"coverage.json": invalid, "raw/proton.jsonl": "", "index.md": "---\nokf_version: '0.2'\n---\n# Functional test\n"} {
						if err := os.WriteFile(filepath.Join(reportDir, name), []byte(content), 0600); err != nil {
							t.Error(err)
							return
						}
					}
					streamReply(w, "REPORT_CREATED_MARKER")
					return
				}
				if !strings.Contains(string(body), wantError) || !strings.Contains(string(body), "REPORT_CREATED_MARKER") {
					t.Error("repair did not receive error and prior context")
				}
				if problem != "persistent" && n == 2 {
					toolReply(w, "write", map[string]any{"path": "report/coverage.json", "content": valid})
					return
				}
				streamReply(w, "Finished repair.")
			}))
			defer server.Close()
			output, err := captureStdout(t, func() error {
				return run(t.Context(), []string{"--workdir", workspace, "--data-dir", t.TempDir(), "--report-dir", reportDir, "--provider", "openai", "--base-url", server.URL, "--api-key", "fixture", "--model", "fixture", "-p", "FUNCTIONAL_ONLY", "--output-format", "json", "--quiet", "--no-color"}, io.Discard, io.Discard, fakeTools)
			})
			if (err != nil) != (problem == "persistent") {
				t.Fatalf("run=%v", err)
			}
			if requests.Load() != 3 {
				t.Fatalf("repair requests=%d; want 3", requests.Load())
			}
			var result struct {
				IsError bool `json:"is_error"`
			}
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatal(err)
			}
			if result.IsError != (problem == "persistent") {
				t.Fatalf("result=%s", output)
			}
			body, _ := os.ReadFile(filepath.Join(reportDir, "run.json"))
			var report runReport
			if err := json.Unmarshal(body, &report); err != nil {
				t.Fatal(err)
			}
			want := "completed"
			if problem == "persistent" {
				want = "incomplete"
			}
			if report.Status != want {
				t.Fatalf("status=%s", report.Status)
			}
		})
	}
}
