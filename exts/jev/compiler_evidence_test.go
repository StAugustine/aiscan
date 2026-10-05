package jev

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dop251/goja"
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// This opt-in forensic test replays provider artifacts through today's pure VM
// and independently parses serialized native readers. It never dispatches a
// candidate or calls a provider. Probe results are diagnostics, not a semantic
// publication verdict or a fresh live-generation success rate.
func TestRecordedCompilerEvidence(t *testing.T) {
	path := os.Getenv("JEV_COMPILER_CORPUS")
	if path == "" {
		t.Skip("set JEV_COMPILER_CORPUS to the corpus produced by cmd/harness/testdata/jev_evidence_audit.py")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			ID     int            `json:"request_id"`
			Source string         `json:"source"`
			Input  map[string]any `json:"input"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil || len(corpus.Cases) == 0 {
		t.Fatalf("empty or invalid compiler corpus: %v", err)
	}
	var results []map[string]any
	for _, artifact := range corpus.Cases {
		row := map[string]any{"request_id": artifact.ID, "source_bytes": len(artifact.Source)}
		results = append(results, row)
		r := Reflex{When: "Recorded capability", Decide: "Use current native evidence", Observe: artifact.Source}
		if err := r.validate(); err != nil {
			row["outer_syntax_error"] = err.Error()
			t.Logf("artifact %d outer syntax rejected: %v", artifact.ID, err)
			continue
		}
		history := []map[string]any{}
		for _, entry := range artifact.Input["history"].([]any) {
			history = append(history, entry.(map[string]any))
		}
		probes := []map[string]any{}
		for boundary := 0; boundary <= len(history); boundary++ {
			env := make(map[string]any, len(artifact.Input))
			for key, value := range artifact.Input {
				env[key] = value
			}
			env["history"] = history[:boundary]
			probe := map[string]any{"completed_results": boundary}
			probes = append(probes, probe)
			facts, candidates, err := probeReflexArguments(t.Context(), &r, env, nil, nil, false)
			if err != nil {
				probe["observation_error"] = err.Error()
				continue
			}
			probe["state"], probe["candidates"] = facts, candidates
			readers := []map[string]any{}
			for id, candidate := range candidates {
				var args struct{ Command string }
				if candidate.Name != "bash" || json.Unmarshal(candidate.Arguments, &args) != nil {
					continue
				}
				file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(args.Command), "candidate")
				if err != nil {
					probe["shell_syntax_error"] = err.Error()
					continue
				}
				syntax.Walk(file, func(node syntax.Node) bool {
					call, ok := node.(*syntax.CallExpr)
					if !ok || len(call.Args) < 4 {
						return true
					}
					argv := []string{}
					for _, word := range call.Args {
						// Literal expansion has no command-substitution callback and
						// never runs a shell or executes generated source.
						value, err := expand.Literal(nil, word)
						if err != nil {
							probe["argument_expansion_error"] = err.Error()
							return false
						}
						argv = append(argv, value)
					}
					if argv[0] == "playwright" && argv[1] == "evaluate" {
						source := strings.Join(argv[3:], " ")
						reader := map[string]any{"candidate": id, "bytes": len(source)}
						if _, err := goja.Compile("native-reader", source, false); err != nil {
							reader["syntax_error"] = err.Error()
						}
						readers = append(readers, reader)
					}
					return true
				})
			}
			probe["native_readers"] = readers
		}
		row["boundaries"] = probes
		t.Logf("artifact %d: %d pure boundary probes", artifact.ID, len(probes))
	}
	encoded, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(filepath.Dir(path), "compiler-probes.json")
	if err := os.WriteFile(output, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(os.Stdout, "Compiler diagnostic evidence: %s\n", output)
}
