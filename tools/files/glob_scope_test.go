package files

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestGlobDoesNotTraverseUnrelatedOrDeeperDirectories(t *testing.T) {
	dir := t.TempDir()
	noise := filepath.Join(dir, "aaa-noise")
	for _, name := range []string{noise, filepath.Join(dir, "target")} {
		if err := os.Mkdir(name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// A dependency tree outside the pattern must not consume the traversal
	// budget or cause a bounded source-file query to fail.
	for i := range maxListEntries + 1 {
		if err := os.WriteFile(filepath.Join(noise, fmt.Sprintf("%05d", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"z.txt", "target/a.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	resource, err := New(Config{Directory: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	set := filesystemSet(t, resource)
	if err := set.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer set.Close(context.Background())
	check := func(pattern string, want []string) {
		t.Helper()
		got, err := resource.Files.Glob(t.Context(), pattern, 10)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("glob %q: got %v, err=%v, want %v", pattern, got, err, want)
		}
	}
	check("*.txt", []string{"z.txt"})
	check("target/*.txt", []string{"target/a.txt"})
	check("[t]arget/*.txt", []string{"target/a.txt"})
	check("missing/*.txt", nil)
	if err := os.Rename(noise, filepath.Join(dir, "target", "noise")); err != nil {
		t.Fatal(err)
	}
	check("target/*", []string{"target/a.txt", "target/noise"})
	// A pattern that really addresses the oversized directory remains bounded.
	if _, err := resource.Files.Glob(t.Context(), "target/noise/*", 10); err == nil {
		t.Fatal("glob lost its traversal bound")
	}
}
