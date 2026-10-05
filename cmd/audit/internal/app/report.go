package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chainreactors/cyber/cmd/audit/internal/toolchain"
	"github.com/chainreactors/cyber/pkg/console"
	"github.com/chainreactors/cyber/tools/okf"
)

type runReport struct {
	Directory       string             `json:"-"`
	ID              string             `json:"id"`
	Workspace       string             `json:"workspace"`
	Goal            string             `json:"goal,omitempty"`
	Revision        string             `json:"revision,omitempty"`
	Dirty           *bool              `json:"dirty,omitempty"`
	GitError        string             `json:"git_error,omitempty"`
	GitRoot         string             `json:"git_root,omitempty"`
	GitSubdir       string             `json:"git_subdir,omitempty"`
	GitTrackedFiles int                `json:"git_tracked_files"`
	Resume          string             `json:"resume,omitempty"`
	Started         time.Time          `json:"started"`
	Finished        *time.Time         `json:"finished,omitempty"`
	Status          string             `json:"status"`
	Error           string             `json:"error,omitempty"`
	Tools           []toolchain.Status `json:"tools"`
}
type coverage struct {
	Reviewed    bool          `json:"reviewed"`
	Scope       string        `json:"scope"`
	Examined    []string      `json:"examined"`
	Excluded    []string      `json:"excluded"`
	Unsupported []string      `json:"unsupported"`
	Unresolved  []string      `json:"unresolved"`
	Checks      []checkRecord `json:"checks"`
}
type checkRecord struct {
	Tool     string `json:"tool"`
	Status   string `json:"status"`
	Evidence string `json:"evidence"`
}
type finding struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Status         string   `json:"status"`
	Severity       string   `json:"severity"`
	Location       string   `json:"location"`
	Preconditions  string   `json:"preconditions"`
	Trace          []string `json:"trace"`
	Impact         string   `json:"impact"`
	Evidence       []string `json:"evidence"`
	Verification   string   `json:"verification"`
	Reproduction   string   `json:"reproduction"`
	Recommendation string   `json:"recommendation"`
}

func writeJSON(path string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0600)
}
func newReport(ctx context.Context, workDir, requested, task, resume string, tools []toolchain.Status) (*runReport, error) {
	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	id := now.Format("20060102T150405Z") + "-" + hex.EncodeToString(nonce[:])
	dir := requested
	if dir == "" {
		dir = filepath.Join(workDir, ".cyber", "audit", id)
	} else if !filepath.IsAbs(dir) {
		dir = filepath.Join(workDir, dir)
	}
	dir = filepath.Clean(dir)
	r := &runReport{Directory: dir, ID: id, Workspace: workDir, Goal: task, Resume: resume, Started: now, Status: "running", Tools: tools}
	r.captureGit(ctx)
	if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
		return nil, err
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return nil, fmt.Errorf("report directory must be new: %w", err)
	}
	for _, name := range []string{"raw", "evidence"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
			return nil, err
		}
	}
	if err := r.save(); err != nil {
		return nil, err
	}
	c := coverage{Scope: task, Examined: []string{}, Excluded: []string{".git", ".cyber", dir}, Unsupported: []string{}, Unresolved: []string{"Audit has not completed."}, Checks: []checkRecord{}}
	if err := writeJSON(filepath.Join(dir, "coverage.json"), c); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(dir, "findings.json"), []finding{}); err != nil {
		return nil, err
	}
	for name, body := range map[string]string{
		"index.md": "---\nokf_version: \"0.2\"\n---\n\n# Audit in progress\n\nThis report is incomplete. See [coverage](coverage.json), [findings](findings.json), [run metadata](run.json), [investigation log](log.md) and [session evidence](session.jsonl).\n",
		"log.md":   "# Investigation log\n\n- " + now.Format(time.RFC3339) + " Audit started.\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// A parent repository's HEAD is not provenance for an imported, untracked tree.
// Even for tracked scopes, revision identifies the Git base; dirty is scoped to
// the audited directory and captures local modifications before report creation.
func (r *runReport) captureGit(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	git := func(args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, "git", append([]string{"-C", r.Workspace}, args...)...).Output()
	}
	root, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		r.GitError = "Git/revision unavailable; auditing supplied files"
		return
	}
	r.GitRoot = strings.TrimSpace(string(root))
	prefix, err := git("rev-parse", "--show-prefix")
	if err != nil {
		r.GitError = "could not identify audited Git subdirectory"
		return
	}
	r.GitSubdir = strings.TrimSuffix(strings.TrimSpace(string(prefix)), "/")
	if r.GitSubdir == "" {
		r.GitSubdir = "."
	}
	tracked, err := git("ls-files", "-z", "--", ".")
	if err != nil {
		r.GitError = "could not identify tracked audit files"
		return
	}
	r.GitTrackedFiles = strings.Count(string(tracked), "\x00")
	if r.GitTrackedFiles == 0 {
		r.GitError = "audited directory has no Git-tracked files; parent repository revision does not identify this snapshot"
		return
	}
	revision, err := git("rev-parse", "HEAD")
	if err != nil {
		r.GitError = "Git revision unavailable; auditing supplied files"
		return
	}
	r.Revision = strings.TrimSpace(string(revision))
	status, err := git("status", "--porcelain", "--untracked-files=normal", "--", ".")
	if err != nil {
		r.GitError = "could not read audited worktree status"
		return
	}
	dirty := strings.TrimSpace(string(status)) != ""
	r.Dirty = &dirty
}
func (r *runReport) save() error { return writeJSON(filepath.Join(r.Directory, "run.json"), r) }
func (r *runReport) finish(ctx context.Context, runErr error) error {
	runErr = errors.Join(runErr, ctx.Err())
	r.Status = "completed"
	if runErr == nil {
		runErr = r.validate(ctx)
		if runErr != nil {
			r.Status = "incomplete"
		}
	} else {
		r.Status = "failed"
		if onlyReportValidationErrors(runErr) {
			r.Status = "incomplete"
		}
	}
	if runErr != nil {
		r.Error = runErr.Error()
		if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
			r.Status = "interrupted"
		}
	}
	now := time.Now().UTC()
	r.Finished = &now
	return errors.Join(runErr, r.save())
}

// Cleanup failures can be joined with a validation error. They still mean the
// run failed, even though its deliverable also needs repair.
func onlyReportValidationErrors(err error) bool {
	if _, ok := err.(*console.TaskValidationError); ok {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !onlyReportValidationErrors(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return onlyReportValidationErrors(wrapped.Unwrap())
	}
	return false
}

// searchExclusions is shared by rg's default config and the model's AST recipes.
func (r *runReport) searchExclusions() (string, string, error) {
	patterns := []string{"!.git/**", "!.cyber/**"}
	if rel, err := filepath.Rel(r.Workspace, r.Directory); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		// Escape glob metacharacters so a custom report path remains literal.
		rel = filepath.ToSlash(rel)
		rel = strings.NewReplacer("[", "[[]", "*", "[*]", "?", "[?]", "{", "[{]", "}", "[}]").Replace(rel)
		patterns = append(patterns, "!"+rel+"/**")
	}
	var config strings.Builder
	for _, pattern := range patterns {
		config.WriteString("--glob\n" + pattern + "\n")
	}
	path := filepath.Join(r.Directory, "ripgrep.conf")
	if err := os.WriteFile(path, []byte(config.String()), 0600); err != nil {
		return "", "", err
	}
	return path, strings.Join(patterns, "\n"), nil
}
func (r *runReport) validate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var c coverage
	body, err := os.ReadFile(filepath.Join(r.Directory, "coverage.json"))
	if err != nil {
		return err
	}
	if err = json.Unmarshal(body, &c); err != nil {
		return fmt.Errorf("coverage.json: %w", err)
	}
	if !c.Reviewed || strings.TrimSpace(c.Scope) == "" || len(c.Examined) == 0 || c.Excluded == nil || c.Unsupported == nil || c.Unresolved == nil {
		return fmt.Errorf("audit report incomplete: coverage must identify examined scope and remaining limits")
	}
	for field, values := range map[string][]string{"examined": c.Examined, "excluded": c.Excluded, "unsupported": c.Unsupported, "unresolved": c.Unresolved} {
		if !nonblankItems(values) {
			return fmt.Errorf("coverage.json: %s contains a blank entry", field)
		}
	}
	checks := map[string]bool{}
	for _, check := range c.Checks {
		name := strings.TrimSpace(check.Tool)
		if name == "" || checks[name] {
			return fmt.Errorf("coverage.json: check requires a unique non-empty tool name: %q", check.Tool)
		}
		switch check.Status {
		case "completed", "incomplete", "not_applicable":
		default:
			return fmt.Errorf("coverage.json: %s: invalid check status %q (expected completed, incomplete or not_applicable)", name, check.Status)
		}
		if strings.TrimSpace(check.Evidence) == "" {
			return fmt.Errorf("%s check requires evidence or a reason", check.Tool)
		}
		if check.Status == "completed" {
			if err := reportEvidence(r.Directory, check.Evidence); err != nil {
				return err
			}
		}
		if check.Status == "incomplete" && len(c.Unresolved) == 0 {
			return fmt.Errorf("incomplete check %s requires an unresolved limitation", check.Tool)
		}
		checks[name] = true
	}
	if !checks["osv-scanner"] || !checks["proton"] {
		return fmt.Errorf("coverage must record SCA and leak checks, including skipped/incomplete reasons")
	}
	body, err = os.ReadFile(filepath.Join(r.Directory, "findings.json"))
	if err != nil {
		return err
	}
	var findings []finding
	if err = json.Unmarshal(body, &findings); err != nil {
		return fmt.Errorf("findings.json: %w", err)
	}
	if findings == nil {
		return fmt.Errorf("findings.json must be an array")
	}
	ids := map[string]bool{}
	for _, f := range findings {
		if strings.TrimSpace(f.ID) == "" || strings.TrimSpace(f.Title) == "" || ids[f.ID] {
			return fmt.Errorf("finding requires a unique id and title")
		}
		ids[f.ID] = true
		switch f.Status {
		case "candidate", "confirmed", "dismissed", "inconclusive":
		default:
			return fmt.Errorf("%s: invalid finding status", f.ID)
		}
		switch f.Verification {
		case "static", "reproduced", "not_attempted":
		default:
			return fmt.Errorf("%s: invalid verification", f.ID)
		}
		if f.Status == "confirmed" && (strings.TrimSpace(f.Location) == "" || strings.TrimSpace(f.Preconditions) == "" || strings.TrimSpace(f.Impact) == "" || len(f.Trace) == 0 || !nonblankItems(f.Trace) || len(f.Evidence) == 0 || f.Verification == "not_attempted") {
			return fmt.Errorf("%s: confirmed finding lacks trace/evidence", f.ID)
		}
		if f.Verification == "reproduced" && (strings.TrimSpace(f.Reproduction) == "" || len(f.Evidence) == 0) {
			return fmt.Errorf("%s: reproduction evidence required", f.ID)
		}
		for _, path := range f.Evidence {
			if err := reportEvidence(r.Directory, path); err != nil {
				return err
			}
		}
	}
	body, err = os.ReadFile(filepath.Join(r.Directory, "index.md"))
	if err != nil {
		return err
	}
	if strings.Contains(string(body), "# Audit in progress") {
		return fmt.Errorf("audit report incomplete: index.md is still a draft")
	}
	if err := reportEvidence(r.Directory, "log.md"); err != nil {
		return fmt.Errorf("audit report incomplete: investigation log required: %w", err)
	}
	validation, err := okf.Validate(ctx, r.Directory, false)
	if err != nil {
		return err
	}
	for _, issue := range validation.Issues {
		if issue.Level == okf.Error {
			return fmt.Errorf("audit report incomplete: OKF %s: %s", issue.Path, issue.Message)
		}
	}
	return ctx.Err()
}

func nonblankItems(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

func reportEvidence(dir, path string) error {
	if !filepath.IsLocal(filepath.FromSlash(path)) {
		return fmt.Errorf("evidence must use a report-relative path: %s", path)
	}
	rel := filepath.Clean(filepath.FromSlash(path))
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("resolve report directory: %w", err)
	}
	defer root.Close()
	info, err := root.Stat(rel)
	if err != nil {
		return fmt.Errorf("missing evidence %s (requires a report-local file or contained relative symlink): %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("evidence is not a file: %s", path)
	}
	return nil
}
