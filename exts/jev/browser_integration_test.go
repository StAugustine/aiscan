//go:build full

package jev

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

func browserExecutedOperations(t *testing.T, receipts []string, wanted string) (entry, operation, resultEvidence bool) {
	t.Helper()
	paths := map[string]bool{}
	for _, receipt := range receipts {
		_, path, ok := strings.Cut(receipt, "\nEvidence: ")
		if !ok || paths[path] {
			continue
		}
		paths[path] = true
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		decoder := json.NewDecoder(file)
		for {
			var row struct {
				Call   *aop.ToolCall
				Result *struct {
					IsError bool `json:"is_error"`
					Output  []struct {
						Value struct{ Text *struct{ Text string } }
					}
				}
			}
			if err := decoder.Decode(&row); err != nil {
				if err != io.EOF {
					t.Error(err)
				}
				break
			}
			if row.Result != nil && !row.Result.IsError {
				for _, part := range row.Result.Output {
					if part.Value.Text != nil {
						text := part.Value.Text.Text
						if data := resultJSON(text); data != nil {
							actual, err := json.Marshal(data)
							if err != nil {
								t.Fatal(err)
							}
							text = string(actual)
						}
						resultEvidence = resultEvidence || strings.Contains(text, wanted)
					}
				}
			}
			if row.Call == nil {
				continue
			}
			var args struct{ Command string }
			if json.Unmarshal(row.Call.GetArguments().GetData(), &args) != nil {
				continue
			}
			argv, err := coretool.SplitCommandLine(args.Command)
			if err == nil && len(argv) > 1 && argv[0] == "playwright" {
				entry = entry || argv[1] == "open"
				operation = operation || argv[1] == "click"
			}
		}
		_ = file.Close()
	}
	return
}

func TestBrowserExecutionOracleIgnoresGeneratedOperationSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "execution.jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	for _, command := range []string{"playwright open http://example.test --session current", `playwright evaluate current '({source:"playwright click current button; receipt-current"})'`} {
		if err := encoder.Encode(map[string]any{"call": action(command).GetToolCall()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := encoder.Encode(map[string]any{"result": coretool.TextResult("Script: receipt-current\n---\n{\"state\":{\"text\":\"pending\"},\"candidates\":[]}")}); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	entry, operation, resultEvidence := browserExecutedOperations(t, []string{"Execution observations\nEvidence: " + path}, "receipt-current")
	if !entry || operation || resultEvidence {
		t.Fatalf("source text counted as execution/evidence: entry=%t operation=%t evidence=%t", entry, operation, resultEvidence)
	}
	file, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	encoder = json.NewEncoder(file)
	if err := encoder.Encode(map[string]any{"result": coretool.TextResult("actual receipt-current")}); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	_, _, resultEvidence = browserExecutedOperations(t, []string{"Execution observations\nEvidence: " + path}, "receipt-current")
	if !resultEvidence {
		t.Fatal("actual native result was omitted from acceptance evidence")
	}
}
