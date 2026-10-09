package config

import (
	"path/filepath"
	"testing"
)

func TestToolRuntimeIgnoresStaleProfileAndPreservesPathPrecedence(t *testing.T) {
	for _, source := range []string{"file", "environment", "cli"} {
		t.Run(source, func(t *testing.T) {
			c := isolatedContext(t)
			putConfig(t, c.UserFile(), "llm:\n  active_profile: missing\nmisc:\n  data_dir: file-data\n")
			want := filepath.Join(filepath.Dir(c.UserFile()), "file-data")
			c.LookupEnv = func(key string) (string, bool) {
				if key == "CYBER_PROVIDER" {
					return "invalid-provider", true
				}
				if key == "CYBER_DATA_DIR" && source != "file" {
					return "env-data", true
				}
				return "", false
			}
			o := &Option{Context: c}
			if source == "environment" {
				want = filepath.Join(c.Directory, "env-data")
			}
			if source == "cli" {
				o.DataDir = "cli-data"
				o.MarkExplicit("data-dir")
				want = filepath.Join(c.Directory, "cli-data")
			}
			if _, err := ResolveToolRuntimeConfig(o); err != nil {
				t.Fatal(err)
			}
			if o.DataDir != want || o.Snapshot == nil {
				t.Fatalf("data directory = %q, want %q; snapshot present = %v", o.DataDir, want, o.Snapshot != nil)
			}
		})
	}
}
