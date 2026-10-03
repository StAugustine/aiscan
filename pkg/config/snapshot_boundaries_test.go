package config

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestSnapshotProgrammaticToolListsSurviveValidationAndReload(t *testing.T) {
	for _, tools := range [][]string{nil, {}, {"search", "browser"}} {
		c := isolatedContext(t)
		path := filepath.Join(c.Directory, DefaultConfigName)
		putConfig(t, path, "llm:\n  model: fixture\nagent:\n  tools: [old]\n")
		snapshot := resolvedFixture(t, c, "").Snapshot
		document := snapshot.TargetDocument()
		document["agent"] = map[string]any{"tools": tools}
		candidate, err := snapshot.WithTargetDocument(document, nil)
		if err != nil {
			t.Fatalf("typed list %v rejected: %v", tools, err)
		}
		option, err := candidate.FileOptions(nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(tools) != len(option.Tools) || len(tools) > 0 && !reflect.DeepEqual(tools, option.Tools) {
			t.Fatalf("tool list changed: %v -> %v", tools, option.Tools)
		}
	}
}
