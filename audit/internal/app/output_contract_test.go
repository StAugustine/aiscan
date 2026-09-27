package app

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

type failedOutput struct{ err error }

func (w failedOutput) Write([]byte) (int, error) { return 0, w.err }

func TestOneShotHonorsProvidedOutput(t *testing.T) {
	for _, format := range []string{"text", "json", "stream-json", "failed-writer"} {
		t.Run(format, func(t *testing.T) {
			workspace := t.TempDir()
			reportDir := filepath.Join(workspace, "report")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				body, _ := io.ReadAll(req.Body)
				if !strings.Contains(string(body), "cyber-audit") {
					textReply(w, "pong")
					return
				}
				completeReport(t, &runReport{Directory: reportDir})
				streamReply(w, "FUNCTIONAL_WRITER_MARKER")
			}))
			defer server.Close()
			var output strings.Builder
			var destination io.Writer = &output
			writeErr := errors.New("output sink failed")
			if format == "failed-writer" {
				destination = failedOutput{writeErr}
			}
			actualFormat := format
			if format == "failed-writer" {
				actualFormat = "json"
			}
			leaked, runErr := captureStdout(t, func() error {
				return run(t.Context(), []string{"--workdir", workspace, "--report-dir", reportDir, "--data-dir", t.TempDir(), "--provider", "openai", "--base-url", server.URL, "--api-key", "fixture", "--model", "fixture", "-p", "Only test output delivery", "--output-format", actualFormat, "--quiet", "--no-color"}, destination, io.Discard, fakeTools)
			})
			if leaked != "" {
				t.Error("output leaked to process stdout")
			}
			if format == "failed-writer" {
				if !errors.Is(runErr, writeErr) {
					t.Fatalf("writer error not returned: %v", runErr)
				}
			} else if runErr != nil || !strings.Contains(output.String(), "FUNCTIONAL_WRITER_MARKER") {
				t.Fatalf("run=%v output=%q", runErr, output.String())
			}
		})
	}
}
