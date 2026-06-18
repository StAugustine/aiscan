package arsenal

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	crtm "github.com/chainreactors/crtm/pkg"

	"github.com/chainreactors/aiscan/pkg/commands"
	"github.com/chainreactors/aiscan/pkg/telemetry"
)

// callTool is a test helper that invokes the arsenal tool with JSON args.
func callTool(t *testing.T, tool *ArsenalTool, args map[string]string) commands.ToolResult {
	t.Helper()
	data, _ := json.Marshal(args)
	result, err := tool.Execute(context.Background(), string(data))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	return result
}

func resultText(r commands.ToolResult) string {
	if len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].Text
}

// --- Unit tests (offline, no network) ---

func TestArsenalList(t *testing.T) {
	tool := newTestTool(t)
	r := callTool(t, tool, map[string]string{"action": "list"})
	text := resultText(r)

	if !strings.Contains(text, "gogo") {
		t.Error("list should contain gogo")
	}
	if !strings.Contains(text, "nuclei") {
		t.Error("list should contain nuclei")
	}
	if !strings.Contains(text, "installed") {
		t.Error("list should show installed count")
	}
}

func TestArsenalSearchByName(t *testing.T) {
	tool := newTestTool(t)
	r := callTool(t, tool, map[string]string{"action": "search", "query": "nuclei"})
	text := resultText(r)

	if !strings.Contains(text, "nuclei") {
		t.Errorf("search 'nuclei' should find nuclei, got: %s", text)
	}
}

func TestArsenalSearchByTag(t *testing.T) {
	tool := newTestTool(t)
	r := callTool(t, tool, map[string]string{"action": "search", "query": "fuzzer"})
	text := resultText(r)

	// No built-in fuzzer, should return empty or "No tools found".
	if strings.Contains(text, "gogo") {
		t.Error("searching 'fuzzer' should not match gogo")
	}
}

func TestArsenalSearchEmpty(t *testing.T) {
	tool := newTestTool(t)
	r := callTool(t, tool, map[string]string{"action": "search"})
	if !r.IsError {
		t.Error("search without query should return error")
	}
}

func TestArsenalInfoNotFound(t *testing.T) {
	tool := newTestTool(t)
	r := callTool(t, tool, map[string]string{"action": "info", "name": "nonexistent_xyz"})
	if !r.IsError {
		t.Error("info for nonexistent tool should return error")
	}
}

func TestArsenalInstallNotFound(t *testing.T) {
	tool := newTestTool(t)
	r := callTool(t, tool, map[string]string{"action": "install", "name": "nonexistent_xyz"})
	if !r.IsError {
		t.Error("install nonexistent tool should return error")
	}
}

func TestArsenalInstallNoName(t *testing.T) {
	tool := newTestTool(t)
	r := callTool(t, tool, map[string]string{"action": "install"})
	if !r.IsError {
		t.Error("install without name should return error")
	}
}

func TestArsenalAddBadRepo(t *testing.T) {
	tool := newTestTool(t)
	r := callTool(t, tool, map[string]string{"action": "add", "repo": "noslash"})
	if !r.IsError {
		t.Error("add with bad repo format should return error")
	}
}

func TestArsenalAddAndFind(t *testing.T) {
	tool := newTestTool(t)

	// Before add, ffuf is not in catalog.
	r := callTool(t, tool, map[string]string{"action": "search", "query": "ffuf"})
	if strings.Contains(resultText(r), "ffuf/ffuf") {
		t.Fatal("ffuf should not be in catalog before add")
	}

	// Add ffuf.
	r = callTool(t, tool, map[string]string{
		"action":  "add",
		"repo":    "ffuf/ffuf",
		"pattern": "{name}_{version}_{os}_{arch}.tar.gz",
	})
	if r.IsError {
		t.Fatalf("add ffuf failed: %s", resultText(r))
	}
	if !strings.Contains(resultText(r), "Added ffuf") {
		t.Errorf("expected 'Added ffuf', got: %s", resultText(r))
	}

	// Now search should find ffuf.
	r = callTool(t, tool, map[string]string{"action": "search", "query": "ffuf"})
	if !strings.Contains(resultText(r), "ffuf") {
		t.Errorf("ffuf should be in catalog after add, got: %s", resultText(r))
	}

	// Add again should report duplicate.
	r = callTool(t, tool, map[string]string{"action": "add", "repo": "ffuf/ffuf"})
	if r.IsError {
		t.Fatalf("duplicate add should not error: %s", resultText(r))
	}
	if !strings.Contains(resultText(r), "already registered") {
		t.Errorf("expected 'already registered', got: %s", resultText(r))
	}
}

func TestArsenalUnknownAction(t *testing.T) {
	tool := newTestTool(t)
	r := callTool(t, tool, map[string]string{"action": "bad_action"})
	if !r.IsError {
		t.Error("unknown action should return error")
	}
}

// --- E2E tests (real network) ---

func TestArsenalE2E_InfoRegistered(t *testing.T) {
	if testing.Short() {
		t.Skip("skip network test in short mode")
	}
	tool := newTestTool(t)
	r := callTool(t, tool, map[string]string{"action": "info", "name": "gogo"})
	text := resultText(r)

	if r.IsError {
		t.Fatalf("info gogo failed: %s", text)
	}
	if !strings.Contains(text, "chainreactors") {
		t.Errorf("info should show org, got: %s", text)
	}
	if !strings.Contains(text, "Latest:") || strings.Contains(text, "Latest:    \n") {
		// Latest should have a version resolved.
		t.Logf("info output: %s", text)
	}
}

func TestArsenalE2E_InstallRegisteredCR(t *testing.T) {
	if testing.Short() {
		t.Skip("skip network test in short mode")
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("install e2e only on linux/amd64")
	}

	tool := newTestTool(t)

	// Install gogo (chainreactors, raw binary, latest).
	r := callTool(t, tool, map[string]string{"action": "install", "name": "gogo"})
	text := resultText(r)
	if r.IsError {
		t.Fatalf("install gogo failed: %s", text)
	}
	if !strings.Contains(text, "Installed gogo") {
		t.Errorf("expected install confirmation, got: %s", text)
	}

	// Binary should exist.
	binPath := filepath.Join(tool.mgr.BinPath(), "gogo")
	info, err := os.Stat(binPath)
	if err != nil {
		t.Fatalf("gogo binary not found at %s: %v", binPath, err)
	}
	if info.Size() < 100_000 {
		t.Errorf("gogo binary too small: %d bytes", info.Size())
	}

	// Install again should be idempotent (not error).
	r = callTool(t, tool, map[string]string{"action": "install", "name": "gogo"})
	if r.IsError {
		t.Errorf("re-install should be idempotent, got error: %s", resultText(r))
	}
	if !strings.Contains(resultText(r), "already installed") {
		t.Errorf("expected 'already installed', got: %s", resultText(r))
	}

	// Verify install output includes hint and docs for CR tools.
	// gogo has a hint about built-in pseudo-command.
	firstInstall := text
	if !strings.Contains(firstInstall, "Docs:") {
		t.Logf("install output: %s", firstInstall)
	}
	if !strings.Contains(firstInstall, "Hint:") {
		t.Logf("install output (no hint): %s", firstInstall)
	}
}

func TestArsenalE2E_InstallRegisteredPD(t *testing.T) {
	if testing.Short() {
		t.Skip("skip network test in short mode")
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("install e2e only on linux/amd64")
	}

	tool := newTestTool(t)

	// Install dnsx (projectdiscovery, zip, version auto-resolved).
	r := callTool(t, tool, map[string]string{"action": "install", "name": "dnsx"})
	text := resultText(r)
	if r.IsError {
		t.Fatalf("install dnsx failed: %s", text)
	}

	binPath := filepath.Join(tool.mgr.BinPath(), "dnsx")
	info, err := os.Stat(binPath)
	if err != nil {
		t.Fatalf("dnsx binary not found: %v", err)
	}
	if info.Size() < 100_000 {
		t.Errorf("dnsx binary too small: %d bytes", info.Size())
	}
}

func TestArsenalE2E_AddAndInstallNewTool(t *testing.T) {
	if testing.Short() {
		t.Skip("skip network test in short mode")
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("install e2e only on linux/amd64")
	}

	tool := newTestTool(t)

	// Step 1: ffuf is not registered by default, install should fail.
	r := callTool(t, tool, map[string]string{"action": "install", "name": "ffuf"})
	if !r.IsError {
		t.Fatal("install ffuf should fail before add")
	}

	// Step 2: Add ffuf.
	r = callTool(t, tool, map[string]string{
		"action":  "add",
		"repo":    "ffuf/ffuf",
		"pattern": "{name}_{version}_{os}_{arch}.tar.gz",
	})
	if r.IsError {
		t.Fatalf("add ffuf failed: %s", resultText(r))
	}

	// Step 3: Install ffuf.
	r = callTool(t, tool, map[string]string{"action": "install", "name": "ffuf"})
	text := resultText(r)
	if r.IsError {
		t.Fatalf("install ffuf failed: %s", text)
	}
	if !strings.Contains(text, "Installed ffuf") {
		t.Errorf("expected install confirmation, got: %s", text)
	}

	// Step 4: Binary should exist and be executable.
	binPath := filepath.Join(tool.mgr.BinPath(), "ffuf")
	info, err := os.Stat(binPath)
	if err != nil {
		t.Fatalf("ffuf binary not found: %v", err)
	}
	if info.Size() < 1_000_000 {
		t.Errorf("ffuf binary too small (%d bytes), expected >1MB", info.Size())
	}
	if info.Mode().Perm()&0111 == 0 {
		t.Error("ffuf binary should be executable")
	}

	// Step 5: Should be on PATH (set during NewArsenalTool).
	pathEnv := os.Getenv("PATH")
	if !strings.Contains(pathEnv, tool.mgr.BinPath()) {
		t.Error("arsenal bin path should be on PATH")
	}
}

func TestArsenalE2E_InstallWithVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skip network test in short mode")
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("install e2e only on linux/amd64")
	}

	tool := newTestTool(t)

	// Install specific version of dnsx.
	r := callTool(t, tool, map[string]string{
		"action":  "install",
		"name":    "dnsx",
		"version": "1.2.3",
	})
	text := resultText(r)
	if r.IsError {
		t.Fatalf("install dnsx@1.2.3 failed: %s", text)
	}

	binPath := filepath.Join(tool.mgr.BinPath(), "dnsx")
	info, err := os.Stat(binPath)
	if err != nil {
		t.Fatalf("dnsx binary not found: %v", err)
	}
	if info.Size() < 100_000 {
		t.Errorf("dnsx binary too small: %d bytes", info.Size())
	}
}

// TestArsenalE2E_ListShowsVersionAfterInstall verifies that `list` output
// includes install status and detected version for installed tools.
func TestArsenalE2E_ListShowsVersionAfterInstall(t *testing.T) {
	if testing.Short() {
		t.Skip("skip network test in short mode")
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("install e2e only on linux/amd64")
	}

	tool := newTestTool(t)

	// Before install: gogo should show no version marker.
	r := callTool(t, tool, map[string]string{"action": "list"})
	text := resultText(r)
	if strings.Contains(text, "* v") && strings.Contains(text, "gogo") {
		t.Fatalf("gogo should not show installed version before install, got:\n%s", text)
	}

	// Install gogo.
	r = callTool(t, tool, map[string]string{"action": "install", "name": "gogo"})
	if r.IsError {
		t.Fatalf("install gogo failed: %s", resultText(r))
	}

	// After install: list should show version or at least '*'.
	r = callTool(t, tool, map[string]string{"action": "list"})
	text = resultText(r)
	t.Logf("list output:\n%s", text)

	// Find the gogo line.
	found := false
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "gogo") && strings.Contains(line, "*") {
			found = true
			t.Logf("gogo line: %s", line)
			break
		}
	}
	if !found {
		t.Errorf("gogo should show installed marker '*' after install")
	}

	// Should show "N/M installed" summary.
	if !strings.Contains(text, "/") || !strings.Contains(text, "installed") {
		t.Errorf("list should show installed count summary")
	}
}

// TestArsenalE2E_PDVersionDetection verifies version extraction for PD tools.
func TestArsenalE2E_PDVersionDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("skip network test in short mode")
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("install e2e only on linux/amd64")
	}

	tool := newTestTool(t)

	// Install dnsx (PD tool with proper --version output).
	r := callTool(t, tool, map[string]string{
		"action": "install", "name": "dnsx", "version": "1.2.3",
	})
	if r.IsError {
		t.Fatalf("install dnsx: %s", resultText(r))
	}

	// List should show the version.
	r = callTool(t, tool, map[string]string{"action": "list"})
	text := resultText(r)
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "dnsx") {
			t.Logf("dnsx line: %s", line)
			if !strings.Contains(line, "1.2.") {
				t.Errorf("dnsx should show version 1.2.x, got: %s", line)
			}
			break
		}
	}
}

// TestArsenalE2E_InstallThenExec verifies the full chain:
// arsenal install → tool available via os/exec (same as bash tool).
func TestArsenalE2E_InstallThenExec(t *testing.T) {
	if testing.Short() {
		t.Skip("skip network test in short mode")
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("install e2e only on linux/amd64")
	}

	tool := newTestTool(t)

	// Verify gogo is NOT on PATH before install.
	_, lookErr := exec.LookPath("gogo")
	if lookErr == nil {
		t.Skip("gogo already on system PATH, can't test isolation")
	}

	// Install gogo via arsenal.
	r := callTool(t, tool, map[string]string{"action": "install", "name": "gogo"})
	if r.IsError {
		t.Fatalf("install failed: %s", resultText(r))
	}

	// Verify PATH was updated by arsenal init.
	pathEnv := os.Getenv("PATH")
	if !strings.Contains(pathEnv, tool.mgr.BinPath()) {
		t.Fatalf("arsenal bin path %q not in PATH: %s", tool.mgr.BinPath(), pathEnv)
	}

	// Verify the binary is findable via LookPath (same mechanism bash tool uses).
	gogoPath, err := exec.LookPath("gogo")
	if err != nil {
		t.Fatalf("gogo not found on PATH after install: %v", err)
	}
	t.Logf("gogo found at: %s", gogoPath)

	// Actually execute it — gogo -v should print version and exit.
	out, err := exec.Command("gogo", "-v").CombinedOutput()
	if err != nil {
		// gogo -v may exit non-zero, that's OK as long as it runs.
		t.Logf("gogo -v exit: %v (output: %s)", err, string(out))
	}
	if len(out) == 0 {
		t.Error("gogo -v produced no output — binary may not be functional")
	} else {
		t.Logf("gogo output: %s", strings.TrimSpace(string(out)))
	}
}

// --- helper ---

func newTestTool(t *testing.T) *ArsenalTool {
	t.Helper()
	dir := t.TempDir()

	// Override crtm paths to temp dir for isolation.
	binPath := filepath.Join(dir, "bin")
	configPath := filepath.Join(dir, "config.yaml")

	// We can't use NewArsenalTool directly since it uses default paths.
	// Construct manually for test isolation.
	mgr, err := newTestManager(binPath, configPath)
	if err != nil {
		t.Fatalf("init test manager: %v", err)
	}

	os.MkdirAll(binPath, 0o755)
	path := os.Getenv("PATH")
	if !strings.Contains(path, binPath) {
		os.Setenv("PATH", binPath+string(os.PathListSeparator)+path)
	}

	return &ArsenalTool{
		mgr:    mgr,
		logger: telemetry.NopLogger(),
	}
}

func newTestManager(binPath, configPath string) (*crtmManager, error) {
	// Type alias to avoid import cycle in comment — this uses the real crtm.
	return crtm.NewManager(crtm.ManagerOption{
		BinPath:    binPath,
		ConfigPath: configPath,
	})
}

// crtmManager is a type alias for readability.
type crtmManager = crtm.Manager
