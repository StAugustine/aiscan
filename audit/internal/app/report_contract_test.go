package app

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/chainreactors/cyber/pkg/console"
)

func TestCoverageRejectsEmptyAndContradictoryRecords(t *testing.T) {
	for name, mutate := range map[string]func(*coverage){
		"blank scope":            func(c *coverage) { c.Scope = " \t" },
		"blank examined path":    func(c *coverage) { c.Examined = []string{" "} },
		"blank unresolved limit": func(c *coverage) { c.Checks[1].Status = "incomplete"; c.Unresolved = []string{" "} },
		"blank skipped reason":   func(c *coverage) { c.Checks[0].Evidence = " \n" },
		"blank tool": func(c *coverage) {
			c.Checks = append(c.Checks, checkRecord{Tool: " ", Status: "not_applicable", Evidence: "skipped"})
		},
		"duplicate check": func(c *coverage) {
			c.Checks = append(c.Checks, checkRecord{Tool: "proton", Status: "not_applicable", Evidence: "contradicts completed"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			r, err := newReport(t.Context(), t.TempDir(), "", "fixture", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			completeReport(t, r)
			var c coverage
			body, err := os.ReadFile(filepath.Join(r.Directory, "coverage.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(body, &c); err != nil {
				t.Fatal(err)
			}
			mutate(&c)
			if err := writeJSON(filepath.Join(r.Directory, "coverage.json"), c); err != nil {
				t.Fatal(err)
			}
			if err := r.validate(t.Context()); err == nil {
				t.Fatal("invalid coverage accepted")
			}
		})
	}
}

func TestConfirmedFindingRejectsWhitespaceEvidenceDescription(t *testing.T) {
	r, err := newReport(t.Context(), t.TempDir(), "", "fixture", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	completeReport(t, r)
	f := finding{ID: "fixture", Title: "functional fixture", Status: "confirmed", Verification: "static", Location: " ", Preconditions: "\n", Impact: "\t", Trace: []string{" "}, Evidence: []string{"raw/proton.jsonl"}}
	if err := writeJSON(filepath.Join(r.Directory, "findings.json"), []finding{f}); err != nil {
		t.Fatal(err)
	}
	if err := r.validate(t.Context()); err == nil {
		t.Fatal("blank confirmation accepted")
	}
}

func TestReportEvidenceRequiresContainedResolvedFile(t *testing.T) {
	root, external := t.TempDir(), t.TempDir()
	linkDir := func(target, link string) {
		t.Helper()
		if runtime.GOOS == "windows" {
			if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
				t.Fatalf("junction: %v: %s", err, out)
			}
		} else if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(external, "outside.txt"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	linkDir(external, filepath.Join(root, "linked"))
	if err := reportEvidence(root, "linked/outside.txt"); err == nil {
		t.Fatal("external symlink evidence accepted")
	}
	if err := os.Mkdir(filepath.Join(root, "raw"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "raw", "inside.txt"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("raw", filepath.Join(root, "alias")); err != nil {
		if runtime.GOOS == "windows" {
			t.Logf("relative symlink privilege unavailable; external junction case was exercised: %v", err)
			return
		}
		t.Fatal(err)
	}
	if err := reportEvidence(root, "alias/inside.txt"); err != nil {
		t.Fatalf("internal symlink rejected: %v", err)
	}
}

func TestReportFinalizationDoesNotHideExecutionFailureBehindValidation(t *testing.T) {
	r, err := newReport(t.Context(), t.TempDir(), "", "fixture", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	diskErr := errors.New("recorder write failed")
	validationErr := &console.TaskValidationError{Err: errors.New("coverage incomplete")}
	err = r.finish(t.Context(), errors.Join(validationErr, diskErr))
	if !errors.Is(err, diskErr) || r.Status != "failed" {
		t.Fatalf("status=%s err=%v", r.Status, err)
	}
}
