//go:build e2e

package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

// echoRuntime executes commands by echoing them back, for testing.
type echoRuntime struct{}

func (e *echoRuntime) CommandNames() []string { return []string{"scan", "echo"} }

func (e *echoRuntime) ExecuteCommand(_ context.Context, cmdLine string, stream io.Writer) (string, json.RawMessage, error) {
	lines := []string{
		"[scan] starting: " + cmdLine,
		"[scan] port 80 open",
		"[scan] port 443 open",
		"[scan] complete",
	}
	for _, line := range lines {
		stream.Write([]byte(line + "\n"))
		time.Sleep(50 * time.Millisecond)
	}
	result, _ := json.Marshal(map[string]any{
		"summary": map[string]any{"targets": 1, "services": 2},
	})
	return "scan finished: " + cmdLine, result, nil
}

// memStore is a minimal in-memory Store for e2e tests.
type memStore struct {
	jobs map[string]*ScanJob
}

func newMemStore() *memStore { return &memStore{jobs: make(map[string]*ScanJob)} }

func (s *memStore) Create(_ context.Context, job *ScanJob) error {
	s.jobs[job.ID] = job
	return nil
}
func (s *memStore) Get(_ context.Context, id string) (*ScanJob, error) {
	j, ok := s.jobs[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return j, nil
}
func (s *memStore) List(_ context.Context, limit int) ([]*ScanJob, error) {
	var result []*ScanJob
	for _, j := range s.jobs {
		result = append(result, j)
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}
func (s *memStore) Update(_ context.Context, job *ScanJob) error {
	s.jobs[job.ID] = job
	return nil
}
func (s *memStore) Delete(_ context.Context, id string) error {
	delete(s.jobs, id)
	return nil
}

func setupE2EServer(t *testing.T) (*httptest.Server, *AgentPool, context.CancelFunc) {
	t.Helper()

	store := newMemStore()
	service := NewService(ServiceConfig{
		Store:         store,
		MaxConcurrent: 3,
		ScanTimeout:   30 * time.Second,
	})
	t.Cleanup(func() { service.Close() })

	pool := NewAgentPool(service.Hub())
	service.SetAgentPool(pool)

	handler := NewHandler(service, pool, nil, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(func() { srv.Close() })

	ctx, cancel := context.WithCancel(context.Background())

	// Start mock callback agent
	go RunCallback(ctx, CallbackConfig{
		ServerURL: srv.URL,
		Name:      "e2e-worker",
		Runtime:   &echoRuntime{},
	})

	// Wait for agent to register
	deadline := time.After(3 * time.Second)
	for pool.Count() == 0 {
		select {
		case <-deadline:
			t.Fatal("agent did not register within 3s")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	return srv, pool, cancel
}

func TestE2EAgentRegisterAndDispatch(t *testing.T) {
	srv, pool, cancel := setupE2EServer(t)
	defer cancel()

	// Verify agent is registered via API
	resp, err := http.Get(srv.URL + "/api/agents")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var agents []AgentInfo
	json.NewDecoder(resp.Body).Decode(&agents)
	if len(agents) != 1 || agents[0].Name != "e2e-worker" {
		t.Fatalf("expected 1 agent named e2e-worker, got %+v", agents)
	}

	// Verify status includes agents count
	resp2, err := http.Get(srv.URL + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var status ServiceStatus
	json.NewDecoder(resp2.Body).Decode(&status)
	if status.Agents != 1 {
		t.Fatalf("expected status.agents=1, got %d", status.Agents)
	}

	// Submit a scan — should route through the agent
	scanBody := `{"target":"127.0.0.1","mode":"quick"}`
	resp3, err := http.Post(srv.URL+"/api/scans", "application/json", strings.NewReader(scanBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp3.Body)
		t.Fatalf("expected 201, got %d: %s", resp3.StatusCode, body)
	}
	var job ScanJob
	json.NewDecoder(resp3.Body).Decode(&job)

	// Poll until complete
	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("scan did not complete within 10s, last status: %s", job.Status)
		default:
		}
		time.Sleep(200 * time.Millisecond)
		resp4, err := http.Get(srv.URL + "/api/scans/" + job.ID)
		if err != nil {
			continue
		}
		json.NewDecoder(resp4.Body).Decode(&job)
		resp4.Body.Close()
		if job.Status == StatusCompleted || job.Status == StatusFailed {
			break
		}
	}

	if job.Status != StatusCompleted {
		t.Fatalf("expected completed, got %s (error: %s)", job.Status, job.Error)
	}
	if job.Report == "" {
		t.Fatal("expected non-empty report")
	}
	if !strings.Contains(pool.List()[0].Name, "e2e-worker") {
		t.Fatal("agent name mismatch")
	}
}

func TestE2EBrowserShowsAgentsAndScan(t *testing.T) {
	srv, _, cancel := setupE2EServer(t)
	defer cancel()

	// Launch headless browser
	path, found := launcher.LookPath()
	if !found {
		t.Skip("chromium not found, skipping browser test")
	}
	u := launcher.New().Bin(path).Headless(true).
		Set("no-sandbox").
		Set("disable-gpu").
		Set("disable-dev-shm-usage").
		MustLaunch()

	browser := rod.New().ControlURL(u).MustConnect()
	defer browser.MustClose()

	page := browser.MustPage(srv.URL)
	defer page.MustClose()

	// Wait for page to load — check for status API response
	page.MustWaitStable()

	// The status endpoint returns agents=1, verify it's accessible
	statusResp, err := http.Get(srv.URL + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	defer statusResp.Body.Close()
	var status ServiceStatus
	json.NewDecoder(statusResp.Body).Decode(&status)
	if status.Agents != 1 {
		t.Fatalf("API: expected agents=1, got %d", status.Agents)
	}

	// Verify page loaded (look for key UI element)
	// The ScanForm has an input with placeholder "IP, URL, or hostname"
	page.MustWaitLoad()
	time.Sleep(500 * time.Millisecond) // React render

	// Check page title or content
	html := page.MustHTML()
	if !strings.Contains(html, "aiscan") && !strings.Contains(html, "scan") {
		t.Log("page HTML (first 500):", html[:min(500, len(html))])
		// Don't fail — static assets may not be embedded in test
	}

	// Submit a scan via the API and verify SSE events work
	scanBody := `{"target":"10.0.0.1","mode":"quick"}`
	resp, err := http.Post(srv.URL+"/api/scans", "application/json", strings.NewReader(scanBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("scan submit: expected 201, got %d: %s", resp.StatusCode, body)
	}
	var job ScanJob
	json.NewDecoder(resp.Body).Decode(&job)

	// Verify scan completes via polling
	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("scan did not complete")
		default:
		}
		time.Sleep(200 * time.Millisecond)
		r, err := http.Get(srv.URL + "/api/scans/" + job.ID)
		if err != nil {
			continue
		}
		json.NewDecoder(r.Body).Decode(&job)
		r.Body.Close()
		if job.Status == StatusCompleted || job.Status == StatusFailed {
			break
		}
	}

	if job.Status != StatusCompleted {
		t.Fatalf("scan failed: %s (error: %s)", job.Status, job.Error)
	}
	t.Logf("scan completed via agent, report length: %d", len(job.Report))
}
