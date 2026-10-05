package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitProvenanceIsScopedAndDoesNotAttributeImportedFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	for _, name := range []string{"tracked/file.txt", "imported/file.txt", "other.txt"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("add", "tracked", "other.txt")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
	revision := git("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(root, "other.txt"), []byte("unrelated change"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"tracked", "imported"} {
		r := &runReport{Workspace: filepath.Join(root, scope)}
		r.captureGit(t.Context())
		if r.GitRoot == "" || r.GitSubdir != scope {
			t.Fatalf("scope metadata: %+v", r)
		}
		if scope == "tracked" {
			if r.Revision != revision || r.GitTrackedFiles != 1 || r.Dirty == nil || *r.Dirty {
				t.Fatalf("tracked provenance: %+v", r)
			}
			if err := os.WriteFile(filepath.Join(root, scope, "file.txt"), []byte("changed"), 0600); err != nil {
				t.Fatal(err)
			}
			r.captureGit(t.Context())
			if r.Dirty == nil || !*r.Dirty {
				t.Fatal("scoped modification was not recorded")
			}
		} else if r.Revision != "" || r.GitTrackedFiles != 0 || r.Dirty != nil || !strings.Contains(r.GitError, "no Git-tracked files") {
			t.Fatalf("imported files attributed to parent HEAD: %+v", r)
		}
	}
}
