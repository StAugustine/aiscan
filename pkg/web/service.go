package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chainreactors/aiscan/core/output"
	"github.com/chainreactors/aiscan/core/runner"
	"github.com/chainreactors/aiscan/pkg/slashcmd"
	"github.com/chainreactors/aiscan/pkg/webproto"
)

type ConfigStore interface {
	GetDistributeConfig(ctx context.Context) (path string, loaded bool, cfg webproto.DistributeConfig, err error)
	SaveDistributeConfig(ctx context.Context, cfg webproto.DistributeConfig) error
}

type ServiceConfig struct {
	Store         *SQLiteStore
	App           *runner.App
	ConfigStore   ConfigStore
	AppFactory    func(ctx context.Context) (*runner.App, error)
	AgentPool     *AgentPool
	MaxConcurrent int
	ScanTimeout   time.Duration
}

type Service struct {
	store   *SQLiteStore
	appMu   sync.RWMutex
	app     *runner.App
	config  ConfigStore
	reload  func(ctx context.Context) (*runner.App, error)
	agents  *AgentPool
	hub     *Hub
	sem     chan struct{}
	timeout time.Duration

	mu           sync.Mutex
	cancels      map[string]context.CancelFunc
	taskSessions map[string]string // taskID → sessionID
	taskAgents   map[string]string // taskID → agentID
	taskCanceled map[string]bool
}

func NewService(cfg ServiceConfig) *Service {
	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 3
	}
	timeout := cfg.ScanTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	svc := &Service{
		store:        cfg.Store,
		app:          cfg.App,
		config:       cfg.ConfigStore,
		reload:       cfg.AppFactory,
		agents:       cfg.AgentPool,
		hub:          NewHub(),
		sem:          make(chan struct{}, maxConcurrent),
		timeout:      timeout,
		cancels:      make(map[string]context.CancelFunc),
		taskSessions: make(map[string]string),
		taskAgents:   make(map[string]string),
		taskCanceled: make(map[string]bool),
	}
	if cfg.AgentPool != nil {
		cfg.AgentPool.SetSessionLookup(svc)
	}
	return svc
}

func (s *Service) Hub() *Hub { return s.hub }

func (s *Service) SetAgentPool(pool *AgentPool) {
	s.agents = pool
	pool.SetSessionLookup(s)
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	s.appMu.Lock()
	app := s.app
	s.app = nil
	s.appMu.Unlock()
	if app != nil {
		app.Close()
	}
}

func (s *Service) Status() ServiceStatus {
	app := s.appSnapshot()
	status := ServiceStatus{
		LLMAvailable: app != nil && app.Provider != nil,
	}
	if app != nil {
		status.LLMProvider = app.ProviderConfig.Provider
		status.LLMModel = app.ProviderConfig.Model
		status.LLMAPIKeyConfigured = strings.TrimSpace(app.ProviderConfig.APIKey) != ""
	}
	if s.config != nil {
		if path, loaded, dc, err := s.config.GetDistributeConfig(context.Background()); err == nil {
			status.ConfigPath = path
			status.ConfigLoaded = loaded
			if status.LLMProvider == "" {
				status.LLMProvider = dc.LLM.Provider
			}
			if status.LLMModel == "" {
				status.LLMModel = dc.LLM.Model
			}
			status.LLMAPIKeyConfigured = status.LLMAPIKeyConfigured || dc.LLM.APIKey != ""
		}
	}
	return status
}

func (s *Service) GetConfigStatus(ctx context.Context) (ConfigStatus, error) {
	if s.config == nil {
		return ConfigStatus{}, fmt.Errorf("config store is not configured")
	}
	path, loaded, dc, err := s.config.GetDistributeConfig(ctx)
	if err != nil {
		return ConfigStatus{}, err
	}
	return ConfigStatusFromDistribute(&dc, path, loaded), nil
}

func (s *Service) SaveConfig(ctx context.Context, cfg webproto.DistributeConfig) (ConfigStatus, error) {
	if s.config == nil {
		return ConfigStatus{}, fmt.Errorf("config store is not configured")
	}
	if err := s.config.SaveDistributeConfig(ctx, cfg); err != nil {
		return ConfigStatus{}, err
	}
	if s.reload != nil {
		app, err := s.reload(ctx)
		if err != nil {
			cs, _ := s.GetConfigStatus(ctx)
			return cs, fmt.Errorf("reload aiscan runtime: %w", err)
		}
		s.swapApp(app)
	}
	// Tell connected agents to hot-swap their own provider too — the hub reload
	// above only refreshes the hub's in-process runtime, not the agent subprocesses.
	if s.agents != nil {
		s.agents.BroadcastConfigReload()
	}
	return s.GetConfigStatus(ctx)
}

func (s *Service) GetDistributeConfig(ctx context.Context) (webproto.DistributeConfig, error) {
	if s.config == nil {
		return webproto.DistributeConfig{}, fmt.Errorf("config store is not configured")
	}
	_, _, dc, err := s.config.GetDistributeConfig(ctx)
	return dc, err
}

func (s *Service) SubmitScan(ctx context.Context, target, mode string, verify, sniper, deep bool) (*ScanJob, error) {
	target, err := ValidateTarget(target)
	if err != nil {
		return nil, err
	}
	mode, err = ValidateMode(mode)
	if err != nil {
		return nil, err
	}
	if (verify || sniper || deep) && !s.aiAvailable() {
		return nil, fmt.Errorf("selected analysis options require an LLM provider")
	}

	now := time.Now()
	job := &ScanJob{
		ID:        generateID(),
		Target:    target,
		Mode:      mode,
		Verify:    verify,
		Sniper:    sniper,
		AI:        verify || sniper,
		Deep:      deep,
		Status:    StatusQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.store.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("store create: %w", err)
	}

	go s.runScan(job.ID) //nolint:gosec // G118: background scan outlives the request

	return job, nil
}

func (s *Service) GetScan(ctx context.Context, id string) (*ScanJob, error) {
	return s.store.Get(ctx, id)
}

func (s *Service) ListScans(ctx context.Context) ([]*ScanJob, error) {
	return s.store.List(ctx, 100)
}

func (s *Service) CancelScan(id string) error {
	s.mu.Lock()
	cancel, ok := s.cancels[id]
	s.mu.Unlock()
	if ok {
		cancel()
	}
	ctx := context.Background()
	job, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if job.Status == StatusRunning || job.Status == StatusQueued {
		job.Status = StatusCanceled
		job.UpdatedAt = time.Now()
		return s.store.Update(ctx, job)
	}
	return nil
}

func (s *Service) GetReport(ctx context.Context, id string) (string, error) {
	job, err := s.store.Get(ctx, id)
	if err != nil {
		return "", err
	}
	return job.Report, nil
}

func (s *Service) runScan(jobID string) {
	s.sem <- struct{}{}
	defer func() { <-s.sem }()

	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	s.mu.Lock()
	s.cancels[jobID] = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.cancels, jobID)
		s.mu.Unlock()
	}()

	job, err := s.store.Get(ctx, jobID)
	if err != nil {
		return
	}
	if job.Status == StatusCanceled {
		return
	}

	job.Status = StatusRunning
	job.UpdatedAt = time.Now()
	_ = s.store.Update(ctx, job)

	s.hub.Broadcast(jobID, HubEvent{
		Type: "status",
		Data: mustJSON(map[string]string{"scan_id": jobID, "status": string(StatusRunning)}),
	})

	// Try agent dispatch first, fall back to local execution.
	if s.agents != nil && s.agents.Count() > 0 {
		s.runScanViaAgent(ctx, job)
		return
	}
	s.runScanLocally(ctx, job)
}

func (s *Service) runScanViaAgent(ctx context.Context, job *ScanJob) {
	agent := s.agents.Pick()
	if agent == nil {
		s.failJob(job, "no agents available")
		return
	}

	cmd := "scan " + strings.Join(scanArgsForJob(job), " ")
	resultCh, err := s.agents.DispatchCommand(agent.id, job.ID, cmd)
	if err != nil {
		s.failJob(job, err.Error())
		return
	}

	// Wait for agent to complete. Output is forwarded to SSE hub by
	// AgentPool.HandleOutput as the agent POSTs progress lines.
	res, ok := <-resultCh
	if !ok {
		s.failJob(job, "agent disconnected")
		return
	}
	if res.Err != "" {
		s.failJob(job, res.Err)
		return
	}
	if progress := lastOutputLine(res.Output); progress != "" {
		job.Progress = progress
	}

	var result *output.Result
	if len(res.Result) > 0 {
		result = &output.Result{}
		_ = json.Unmarshal(res.Result, result)
	}

	report := buildMarkdownReport(job.Target, job.Mode, result)
	job.Status = StatusCompleted
	job.Report = report
	job.Result = result
	job.UpdatedAt = time.Now()
	_ = s.store.Update(ctx, job)

	s.persistResultRecords(job.ID, agent.id, result)

	s.hub.Broadcast(job.ID, HubEvent{
		Type:     "complete",
		Data:     mustJSON(map[string]any{"scan_id": job.ID, "status": "completed", "result": result}),
		Reliable: true,
	})
	s.broadcastScanComplete(job.ID, result)
}

func (s *Service) runScanLocally(ctx context.Context, job *ScanJob) {
	streamWriter := &sseStreamWriter{
		hub:    s.hub,
		scanID: job.ID,
		store:  s.store,
		job:    job,
		ctx:    ctx,
	}

	args := scanArgsForJob(job)
	_, result, err := s.executeScan(ctx, args, streamWriter)
	if err != nil {
		s.failJob(job, err.Error())
		return
	}
	if streamWriter.job != nil {
		job = streamWriter.job
	}

	report := buildMarkdownReport(job.Target, job.Mode, result)
	job.Status = StatusCompleted
	job.Report = report
	job.Result = result
	job.UpdatedAt = time.Now()
	_ = s.store.Update(ctx, job)

	s.persistResultRecords(job.ID, "", result)

	s.hub.Broadcast(job.ID, HubEvent{
		Type:     "complete",
		Data:     mustJSON(map[string]any{"scan_id": job.ID, "status": "completed", "result": result}),
		Reliable: true,
	})
	s.broadcastScanComplete(job.ID, result)
}

func (s *Service) persistResultRecords(scanID, agentID string, result *output.Result) {
	recs := resultToRecords(scanID, agentID, result)
	if len(recs) > 0 {
		_ = s.store.InsertRecords(context.Background(), recs)
	}
}

func (s *Service) failJob(job *ScanJob, errMsg string) {
	job.Status = StatusFailed
	job.Error = errMsg
	job.UpdatedAt = time.Now()
	_ = s.store.Update(context.Background(), job)
	s.hub.Broadcast(job.ID, HubEvent{
		Type:     "error",
		Data:     mustJSON(map[string]string{"scan_id": job.ID, "error": errMsg}),
		Reliable: true,
	})
}

func (s *Service) aiAvailable() bool {
	app := s.appSnapshot()
	return app != nil && app.Provider != nil
}

func (s *Service) appSnapshot() *runner.App {
	if s == nil {
		return nil
	}
	s.appMu.RLock()
	defer s.appMu.RUnlock()
	return s.app
}

func (s *Service) swapApp(next *runner.App) {
	if s == nil || next == nil {
		return
	}
	s.appMu.Lock()
	prev := s.app
	s.app = next
	s.appMu.Unlock()
	if prev != nil && prev != next {
		prev.Close()
	}
}

func scanArgsForJob(job *ScanJob) []string {
	args := []string{"-i", job.Target, "--mode", job.Mode}
	if job.Verify {
		args = append(args, "--verify=high")
	}
	if job.Sniper {
		args = append(args, "--sniper")
	}
	if job.Deep {
		args = append(args, "--deep")
	}
	return args
}

type structuredScanCommand interface {
	ExecuteStructured(ctx context.Context, args []string, stream io.Writer) (string, *output.Result, error)
}

func (s *Service) executeScan(ctx context.Context, args []string, stream io.Writer) (string, *output.Result, error) {
	app := s.appSnapshot()
	if app == nil || app.Commands == nil {
		return "", nil, fmt.Errorf("aiscan runtime is not ready")
	}
	cmd, ok := app.Commands.Get("scan")
	if !ok {
		return "", nil, fmt.Errorf("scan command is not registered")
	}
	structured, ok := cmd.(structuredScanCommand)
	if !ok {
		return "", nil, fmt.Errorf("scan command does not support structured results")
	}
	return structured.ExecuteStructured(ctx, args, stream)
}

type sseStreamWriter struct {
	hub    *Hub
	scanID string
	store  *SQLiteStore
	job    *ScanJob
	ctx    context.Context
	buf    []byte
}

func (w *sseStreamWriter) Write(p []byte) (int, error) {
	if w.ctx != nil {
		select {
		case <-w.ctx.Done():
			return 0, w.ctx.Err()
		default:
		}
	}
	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		line := string(w.buf[:idx])
		w.buf = w.buf[idx+1:]

		line = stripANSI(line)
		if line == "" {
			continue
		}

		fmt.Fprintf(os.Stderr, "[scan:%s] %s\n", w.scanID, line)

		current, err := w.store.Get(context.Background(), w.scanID)
		if err != nil {
			return 0, err
		}
		if current.Status == StatusCanceled {
			return 0, context.Canceled
		}
		current.Progress = line
		current.UpdatedAt = time.Now()
		if err := w.store.Update(context.Background(), current); err != nil {
			return 0, err
		}
		w.job = current

		w.hub.Broadcast(w.scanID, HubEvent{
			Type: "progress",
			Data: mustJSON(map[string]string{"scan_id": w.scanID, "data": line}),
		})
	}
	return len(p), nil
}

func buildMarkdownReport(target, mode string, result *output.Result) string {
	var sb strings.Builder
	sb.WriteString("# Penetration Test Report\n\n")
	sb.WriteString(fmt.Sprintf("**Target:** `%s`  \n", target))
	sb.WriteString(fmt.Sprintf("**Mode:** %s  \n", mode))
	sb.WriteString(fmt.Sprintf("**Date:** %s\n\n", time.Now().Format("2006-01-02 15:04:05")))
	sb.WriteString("---\n\n")

	if result == nil {
		sb.WriteString("No structured result was returned.\n")
		return sb.String()
	}

	sb.WriteString("## Summary\n\n")
	sb.WriteString("| Metric | Value |\n|---|---:|\n")
	sb.WriteString(fmt.Sprintf("| Targets | %d |\n", result.Summary.Targets))
	sb.WriteString(fmt.Sprintf("| Services | %d |\n", result.Summary.Services))
	sb.WriteString(fmt.Sprintf("| Web | %d |\n", result.Summary.Webs))
	sb.WriteString(fmt.Sprintf("| Probes | %d |\n", result.Summary.Probes))
	sb.WriteString(fmt.Sprintf("| Fingerprints | %d |\n", resultFingerprintCount(result)))
	sb.WriteString(fmt.Sprintf("| Loots | %d |\n", result.Summary.Loots))
	sb.WriteString(fmt.Sprintf("| Errors | %d |\n", result.Summary.Errors))
	if result.Summary.Duration != "" {
		sb.WriteString(fmt.Sprintf("| Duration | %s |\n", result.Summary.Duration))
	}
	sb.WriteString("\n")

	if len(result.Assets) == 0 {
		return sb.String()
	}

	sb.WriteString("## Assets\n\n")
	for _, asset := range result.Assets {
		title := output.FirstNonEmpty(asset.Title, asset.Target, asset.Key, "Asset")
		sb.WriteString(fmt.Sprintf("### %s\n\n", title))
		if asset.Target != "" && asset.Target != title {
			sb.WriteString(fmt.Sprintf("- **Target:** %s\n", markdownCode(asset.Target)))
		}
		if asset.Status != "" {
			sb.WriteString(fmt.Sprintf("- **State:** %s\n", markdownCode(asset.Status)))
		}
		writeMarkdownList(&sb, "Services", assetServiceFacts(asset.Items))
		writeMarkdownList(&sb, "HTTP", assetHTTPStatuses(asset.Items))
		writeMarkdownList(&sb, "Fingers", assetFingers(asset.Items))
		writeMarkdownList(&sb, "Sources", assetSources(asset.Items))
		if paths := assetPathCount(asset.Items); paths > 0 {
			sb.WriteString(fmt.Sprintf("- **Paths:** %d\n", paths))
		}
		writeAssetLootMarkdown(&sb, asset.Items)
		sb.WriteString("\n")
	}

	return sb.String()
}

func writeMarkdownList(sb *strings.Builder, label string, values []string) {
	if len(values) == 0 {
		return
	}
	coded := make([]string, 0, len(values))
	for _, value := range values {
		coded = append(coded, markdownCode(value))
	}
	sb.WriteString(fmt.Sprintf("- **%s:** %s\n", label, strings.Join(coded, ", ")))
}

func writeAssetLootMarkdown(sb *strings.Builder, items []output.AssetItem) {
	wrote := false
	for _, item := range items {
		switch item.Kind {
		case output.AssetItemLoot, output.AssetItemNote, output.AssetItemResponse, output.AssetItemError:
			summary := output.FirstNonEmpty(item.Summary, item.Title)
			detail := output.AssetItemDetail(item)
			if summary == "" && detail == "" {
				continue
			}
			prefix := output.FirstNonEmpty(item.Source, item.Kind)
			if item.Status != "" {
				prefix += ":" + item.Status
			}
			if !wrote {
				sb.WriteString("\n#### Analysis\n\n")
				wrote = true
			}
			if summary == "" {
				summary = firstMarkdownLine(detail)
			}
			sb.WriteString(fmt.Sprintf("##### %s\n\n", markdownHeading(summary)))
			sb.WriteString(fmt.Sprintf("**Source:** %s\n\n", markdownCode(prefix)))
			if detail != "" && !sameMarkdownText(summary, detail) {
				writeMarkdownBlock(sb, detail)
			} else if detail == "" && summary != "" {
				sb.WriteString(summary)
				sb.WriteString("\n\n")
			}
		}
	}
}

func firstMarkdownLine(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if idx := strings.IndexByte(value, '\n'); idx >= 0 {
		return strings.TrimSpace(value[:idx])
	}
	return value
}

func sameMarkdownText(left, right string) bool {
	return strings.TrimSpace(left) == strings.TrimSpace(right)
}

func writeMarkdownBlock(sb *strings.Builder, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	sb.WriteString(value)
	sb.WriteString("\n\n")
}

func assetServiceFacts(items []output.AssetItem) []string {
	var values []string
	for _, item := range items {
		if item.Kind != output.AssetItemService {
			continue
		}
		values = append(values, strings.Join(output.CompactStrings(
			output.AssetDataString(item.Data, "protocol"),
			output.AssetDataString(item.Data, "service"),
			output.AssetDataString(item.Data, "port"),
		), " "))
	}
	return output.CompactStrings(values...)
}

func assetHTTPStatuses(items []output.AssetItem) []string {
	var values []string
	for _, item := range items {
		if item.Kind == output.AssetItemPath && item.Status != "" {
			values = append(values, item.Status)
		}
	}
	return output.CompactStrings(values...)
}

func assetFingers(items []output.AssetItem) []string {
	var values []string
	for _, item := range items {
		switch item.Kind {
		case output.AssetItemFingerprint:
			values = append(values, output.FirstNonEmpty(item.Title, output.AssetDataString(item.Data, "name")))
		case output.AssetItemPath:
			values = append(values, output.AssetDataStrings(item.Data, "fingers")...)
		}
	}
	return output.CompactStrings(values...)
}

func assetSources(items []output.AssetItem) []string {
	var values []string
	for _, item := range items {
		values = append(values, item.Source)
	}
	return output.CompactStrings(values...)
}

func assetPathCount(items []output.AssetItem) int {
	count := 0
	for _, item := range items {
		if item.Kind == output.AssetItemPath {
			count++
		}
	}
	return count
}

func resultFingerprintCount(result *output.Result) int {
	if result == nil {
		return 0
	}
	seen := make(map[string]struct{})
	for _, asset := range result.Assets {
		for _, finger := range assetFingers(asset.Items) {
			seen[strings.ToLower(finger)] = struct{}{}
		}
	}
	return len(seen)
}

func markdownCode(value string) string {
	value = strings.ReplaceAll(value, "`", "'")
	return "`" + value + "`"
}

func markdownHeading(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\n", " ")
	if value == "" {
		return "Analysis"
	}
	return strings.TrimLeft(value, "# ")
}

func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func stripANSI(s string) string {
	return output.StripANSI(s)
}

func lastOutputLine(output string) string {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(stripANSI(lines[i]))
		if line != "" {
			return line
		}
	}
	return ""
}

// --- Chat session service methods ---

func sessionTopic(id string) string {
	return "session:" + id
}

func (s *Service) TaskSession(taskID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sid, ok := s.taskSessions[taskID]
	return sid, ok
}

func (s *Service) registerSessionTask(taskID, sessionID, agentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.taskSessions[taskID] = sessionID
	if agentID != "" {
		s.taskAgents[taskID] = agentID
	}
	delete(s.taskCanceled, taskID)
}

func (s *Service) finishSessionTask(taskID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	canceled := s.taskCanceled[taskID]
	delete(s.taskSessions, taskID)
	delete(s.taskAgents, taskID)
	delete(s.taskCanceled, taskID)
	return canceled
}

func (s *Service) CancelSession(ctx context.Context, sessionID string) error {
	if _, err := s.store.GetSession(ctx, sessionID); err != nil {
		return err
	}

	type activeTask struct {
		taskID  string
		agentID string
	}
	var tasks []activeTask
	s.mu.Lock()
	for taskID, sid := range s.taskSessions {
		if sid != sessionID {
			continue
		}
		tasks = append(tasks, activeTask{taskID: taskID, agentID: s.taskAgents[taskID]})
		s.taskCanceled[taskID] = true
	}
	s.mu.Unlock()

	if len(tasks) == 0 {
		s.broadcastSystemMessage(sessionID, SysNoRunningTask, "No running task.", nil)
		return nil
	}
	if s.agents != nil {
		for _, task := range tasks {
			if task.agentID != "" {
				s.agents.CancelTask(task.agentID, task.taskID)
			}
		}
	}
	s.broadcastSystemMessage(sessionID, SysPaused, "Paused.", nil)
	return nil
}

func (s *Service) HandleFileUpload(ctx context.Context, sessionID, filename string, data []byte) (*webproto.FileUploadResult, error) {
	session, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}
	if s.agents == nil {
		return nil, fmt.Errorf("no agent pool available")
	}
	agentID := session.AgentID
	if agentID == "" {
		return nil, fmt.Errorf("session has no assigned agent")
	}

	payload := webproto.FileUploadPayload{
		Filename:  filename,
		FileSize:  int64(len(data)),
		MimeType:  http.DetectContentType(data),
		SessionID: sessionID,
	}
	payloadJSON, _ := json.Marshal(payload)

	taskID := generateID()
	msg := WSMessage{
		Type:    "upload",
		TaskID:  taskID,
		DataB64: base64.StdEncoding.EncodeToString(data),
		Payload: payloadJSON,
	}

	resultCh, err := s.agents.dispatchMessage(agentID, taskID, msg)
	if err != nil {
		return nil, fmt.Errorf("agent dispatch failed: %w", err)
	}

	select {
	case res, ok := <-resultCh:
		if !ok {
			return nil, fmt.Errorf("agent disconnected during upload")
		}
		var result webproto.FileUploadResult
		if len(res.Result) > 0 {
			if err := json.Unmarshal(res.Result, &result); err != nil {
				return &webproto.FileUploadResult{
					Filename: filename,
					Path:     res.Output,
					Size:     int64(len(data)),
				}, nil
			}
		} else {
			result.Filename = filename
			result.Path = res.Output
			result.Size = int64(len(data))
		}
		if result.Error != "" {
			return nil, fmt.Errorf("agent upload error: %s", result.Error)
		}
		s.broadcastSystemMessage(sessionID, SysFileUploaded,
			fmt.Sprintf("File uploaded: %s → %s", filename, result.Path),
			map[string]any{"filename": filename, "path": result.Path})
		return &result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Service) CreateSession(ctx context.Context, agentID, title string) (*ChatSession, error) {
	var agentName string
	if s.agents != nil {
		if info := s.agents.get(agentID); info != nil {
			agentName = info.name
		}
	}
	now := time.Now()
	session := &ChatSession{
		ID:        generateID(),
		AgentID:   agentID,
		AgentName: agentName,
		Title:     title,
		Status:    SessionActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.CreateSession(ctx, session); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return session, nil
}

func (s *Service) GetSession(ctx context.Context, id string) (*ChatSession, error) {
	return s.store.GetSession(ctx, id)
}

func (s *Service) ListSessions(ctx context.Context) ([]*ChatSession, error) {
	return s.store.ListSessions(ctx, 100)
}

func (s *Service) DeleteSession(ctx context.Context, id string) error {
	return s.store.DeleteSession(ctx, id)
}

func (s *Service) GetMessages(ctx context.Context, sessionID string) ([]*ChatMessage, error) {
	return s.store.ListMessages(ctx, sessionID, 500)
}

func (s *Service) BroadcastChatEvent(sessionID string, event ChatEvent) {
	event.SessionID = sessionID
	if !event.Transient {
		s.persistRuntimeChatEvent(sessionID, event)
	}
	s.hub.Broadcast(sessionTopic(sessionID), HubEvent{
		Type: event.Type,
		Data: mustJSON(event),
		// Terminal events must never be dropped (see isTerminalChatEvent). Eval
		// verdicts are rare, non-terminal, but each one is a discrete round marker
		// the client can't reconstruct if lost under backpressure — send reliably.
		Reliable: isTerminalChatEvent(event.Type) || event.Type == ChatEventEval,
	})
}

// isTerminalChatEvent reports whether an event ends a run (or its scan) on the
// client — the signals that release the composer and stop the streaming
// indicators. These are broadcast reliably so the SSE hub never drops them under
// backpressure: a lost token delta is invisible (a later delta and the final
// message resend the full text), but a lost terminal event leaves the UI stuck
// "streaming" forever — a busy composer and a blinking cursor that never clears.
func isTerminalChatEvent(t string) bool {
	switch t {
	case ChatEventMessage, ChatEventMessageEnd, ChatEventError,
		ChatEventScanComplete, ChatEventScanError:
		return true
	}
	return false
}

func (s *Service) persistRuntimeChatEvent(sessionID string, event ChatEvent) {
	if s == nil || s.store == nil || sessionID == "" {
		return
	}

	now := time.Now()
	msg := &ChatMessage{
		ID:        generateID(),
		SessionID: sessionID,
		AgentID:   event.AgentID,
		AgentName: event.AgentName,
		CreatedAt: now,
	}
	metadata := map[string]any{
		"event_type": event.Type,
	}
	if event.Turn > 0 {
		metadata["turn"] = event.Turn
	}

	switch event.Type {
	case ChatEventMessageEnd:
		// The finalized assistant text for one turn — the commentary the model
		// emits before (or between) its tool calls. Only the run's LAST turn used
		// to survive a reload, persisted as the aggregate reply by
		// completeAssistantRun; every earlier turn's text streamed live but was
		// dropped from the store, so it vanished from any timeline rebuilt from it:
		// a page reload, an SSE reconnect, or a session switch that revalidates
		// against the store. Persist each turn's text as an assistant message keyed
		// by its turn so buildTimelineFromMessages reconstructs it. The final turn
		// shares a turn key with the aggregate reply, so on rebuild the two merge
		// into one bubble instead of doubling. (message_start / message_delta stay
		// unpersisted — they are streaming partials of this same finalized text.)
		msg.Role = "assistant"
		msg.Content = strings.TrimSpace(event.Content)
		if msg.Content == "" {
			return
		}

	case ChatEventThinking:
		msg.Role = "system"
		msg.Content = strings.TrimSpace(event.Content)
		if msg.Content == "" {
			msg.Content = "thinking"
		}

	case ChatEventAgentJoined:
		msg.Role = "system"
		msg.Content = strings.TrimSpace(event.AgentName + " joined")

	case ChatEventToolCall:
		msg.Role = "tool_call"
		msg.Content = event.ToolArgs
		metadata["tool_call_id"] = event.ToolCallID
		metadata["tool_name"] = event.ToolName
		metadata["tool_args"] = event.ToolArgs

	case ChatEventToolResult:
		msg.Role = "tool_result"
		msg.Content = event.Content
		metadata["tool_call_id"] = event.ToolCallID

	case ChatEventEval:
		// Persist the round verdict so the eval badge survives a reload / session
		// switch — buildTimelineFromMessages reconstructs it from content (reason)
		// plus this metadata (round/pass).
		msg.Role = "system"
		msg.Content = event.EvalReason
		metadata["eval_round"] = event.EvalRound
		metadata["eval_pass"] = event.EvalPass
		metadata["eval_reason"] = event.EvalReason

	default:
		return
	}

	if data, err := json.Marshal(metadata); err == nil {
		msg.Metadata = data
	}
	_ = s.store.AddMessage(context.Background(), msg)
}

func (s *Service) HandleUserMessage(ctx context.Context, sessionID, content string, opts webproto.ChatPayload) (*ChatMessage, error) {
	now := time.Now()
	msg := &ChatMessage{
		ID:        generateID(),
		SessionID: sessionID,
		Role:      "user",
		Content:   content,
		CreatedAt: now,
	}
	if err := s.store.AddMessage(ctx, msg); err != nil {
		return nil, fmt.Errorf("store message: %w", err)
	}

	// Update session timestamp and auto-title from first message.
	session, err := s.store.GetSession(ctx, sessionID)
	if err == nil {
		session.UpdatedAt = now
		if session.Title == "" {
			title := content
			if len(title) > 60 {
				title = title[:60] + "..."
			}
			session.Title = title
		}
		_ = s.store.UpdateSession(ctx, session)
	}

	go s.dispatchUserMessage(sessionID, msg, opts)

	return msg, nil
}

func (s *Service) dispatchUserMessage(sessionID string, msg *ChatMessage, opts webproto.ChatPayload) {
	content := strings.TrimSpace(msg.Content)

	// A typed "/verb" is routed by its slashcmd catalog Scope. Hub-scope commands
	// (scan pipeline, agent roster, merged help) run here. Agent-scope commands
	// (/status, /provider, /<skill>, ...) and unknown verbs fall through to the
	// agent, where the AgentConsole bridge runs the real REPL — so the full REPL
	// command set and `!bash` work from the browser without a parallel switch.
	if verb, args, ok := parseSlashCommand(content); ok {
		// /clear is a true "clear conversation" on the web: it must wipe the
		// visible+persisted transcript, not just reset the agent's model context.
		// Owned end-to-end by the hub so it does both (see handleClearCommand).
		if verb == "clear" {
			s.handleClearCommand(sessionID, opts)
			return
		}
		if spec, known := slashcmd.Lookup(verb); known && spec.Scope == slashcmd.ScopeHub {
			s.runHubCommand(sessionID, strings.TrimPrefix(spec.Name, "/"), args)
			return
		}
	}

	s.handleChatMessage(sessionID, content, opts)
}

// handleClearCommand implements web /clear as "clear conversation": it deletes the
// session's persisted messages (incl. the "/clear" message itself) and signals the
// open UI to empty its timeline, then forwards /clear to the bound agent so its
// in-memory model context resets too. The agent's "Context cleared." reply lands in
// the now-empty transcript as the sole confirmation line; with no agent bound, the
// emptied view is itself the confirmation.
func (s *Service) handleClearCommand(sessionID string, opts webproto.ChatPayload) {
	_ = s.store.ClearMessages(context.Background(), sessionID)
	// Transient: a live-only signal to connected clients — the cleared state is
	// already durable in the store, so a reconnecting client re-derives it on load.
	s.BroadcastChatEvent(sessionID, ChatEvent{Type: ChatEventSessionCleared, Transient: true})
	if s.sessionAgent(sessionID) != nil {
		s.handleChatMessage(sessionID, "/clear", opts)
	}
}

// runHubCommand executes a hub-scope slash command — one that needs hub state
// (the scan pipeline, the connected-agent roster, or the merged help catalog).
// name is the canonical catalog name without its leading slash. Agent-scope
// commands never reach here; they fall through to the agent bridge.
func (s *Service) runHubCommand(sessionID, name, args string) {
	switch name {
	case "scan":
		s.handleScanCommand(sessionID, args)
	case "agents":
		s.handleAgentsCommand(sessionID)
	case "help":
		s.handleHelpCommand(sessionID)
	}
}

// parseSlashCommand splits a leading "/verb args..." into its lowercased verb
// and the trimmed remainder. ok is false when content does not begin with a
// non-empty "/verb".
func parseSlashCommand(content string) (cmd, args string, ok bool) {
	if !strings.HasPrefix(content, "/") {
		return "", "", false
	}
	rest := strings.TrimSpace(content[1:])
	if rest == "" {
		return "", "", false
	}
	if i := strings.IndexAny(rest, " \t\r\n"); i >= 0 {
		return strings.ToLower(rest[:i]), strings.TrimSpace(rest[i:]), true
	}
	return strings.ToLower(rest), "", true
}

// handleHelpCommand renders the merged "/" command catalog (hub-scope plus the
// bound agent's reported agent-scope commands) as a system message. Broadcast
// with an empty code so the frontend shows this dynamic, already-localized text
// verbatim instead of translating it.
func (s *Service) handleHelpCommand(sessionID string) {
	var b strings.Builder
	b.WriteString("**Commands**\n")
	for _, c := range s.SessionMenu(sessionID) {
		syntax := c.Usage
		if syntax == "" {
			syntax = c.Name
		}
		if c.Description != "" {
			fmt.Fprintf(&b, "- `%s` — %s\n", syntax, c.Description)
		} else {
			fmt.Fprintf(&b, "- `%s`\n", syntax)
		}
	}
	b.WriteString("\n`!<command>` 直接在 agent 上执行 shell/伪命令;其他文本作为对话发送给 agent。")
	s.broadcastSystemMessage(sessionID, "", b.String(), nil)
}

// SessionMenu is the web "/" command catalog for a session: the hub-scope
// commands plus the bound agent's reported agent-scope commands (its skills
// included). It falls back to the static agent-scope menu when no agent is
// bound, so the menu is populated even before an agent connects. This is the
// single source both the "/" menu (GET .../commands) and /help render from.
func (s *Service) SessionMenu(sessionID string) []slashcmd.Spec {
	agentSpecs := s.sessionAgent(sessionID).slashSpecs()
	if len(agentSpecs) == 0 {
		agentSpecs = slashcmd.AgentWebMenu()
	}
	return append(slashcmd.HubWebMenu(), agentSpecs...)
}

func (s *Service) handleScanCommand(sessionID, args string) {
	ctx := context.Background()
	parts := strings.Fields(args)
	if len(parts) == 0 {
		s.BroadcastChatEvent(sessionID, ChatEvent{
			Type:  ChatEventError,
			Error: "usage: /scan <target> [--mode full] [--verify] [--sniper] [--deep]",
		})
		return
	}

	target := parts[0]
	mode := "quick"
	var verify, sniper, deep bool
	for _, p := range parts[1:] {
		switch p {
		case "--mode":
			// next arg handled below
		case "full":
			mode = "full"
		case "--verify":
			verify = true
		case "--sniper":
			sniper = true
		case "--deep":
			deep = true
		}
	}
	for i, p := range parts {
		if p == "--mode" && i+1 < len(parts) {
			mode = parts[i+1]
		}
	}

	job, err := s.SubmitScan(ctx, target, mode, verify, sniper, deep)
	if err != nil {
		s.BroadcastChatEvent(sessionID, ChatEvent{
			Type:  ChatEventError,
			Error: fmt.Sprintf("scan failed: %s", err),
		})
		return
	}

	_ = s.store.LinkScanToSession(ctx, sessionID, job.ID)

	s.registerSessionTask(job.ID, sessionID, "")

	s.BroadcastChatEvent(sessionID, ChatEvent{
		Type:   ChatEventScanStarted,
		ScanID: job.ID,
		Data:   fmt.Sprintf("Scan started: %s (%s)", target, mode),
	})
}

func (s *Service) handleAgentsCommand(sessionID string) {
	if s.agents == nil || s.agents.Count() == 0 {
		s.broadcastSystemMessage(sessionID, SysNoAgentsConnected, "No agents connected.", nil)
		return
	}
	agents := s.agents.List()
	list := make([]map[string]any, 0, len(agents))
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d agent(s) connected:\n", len(agents)))
	for _, a := range agents {
		status := "idle"
		if a.Busy {
			status = "busy"
		}
		sb.WriteString(fmt.Sprintf("- **%s** (%s) — %s", a.Name, a.ID[:8], status))
		entry := map[string]any{"name": a.Name, "id": a.ID[:8], "busy": a.Busy}
		if a.Identity.Model != "" {
			sb.WriteString(fmt.Sprintf(" — %s/%s", a.Identity.Provider, a.Identity.Model))
			entry["provider"] = a.Identity.Provider
			entry["model"] = a.Identity.Model
		}
		sb.WriteString("\n")
		list = append(list, entry)
	}
	s.broadcastSystemMessage(sessionID, SysAgentsList, sb.String(),
		map[string]any{"count": len(agents), "agents": list})
}

func (s *Service) sessionAgent(sessionID string) *remoteAgent {
	session, err := s.store.GetSession(context.Background(), sessionID)
	if err != nil || session.AgentID == "" {
		return nil
	}
	if s.agents == nil {
		return nil
	}
	return s.agents.get(session.AgentID)
}

func (s *Service) handleShellCommand(sessionID, command string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}

	agent := s.sessionAgent(sessionID)
	if agent == nil {
		s.BroadcastChatEvent(sessionID, ChatEvent{
			Type:  ChatEventError,
			Error: "agent is not connected",
		})
		return
	}

	taskID := generateID()
	s.registerSessionTask(taskID, sessionID, agent.id)

	s.BroadcastChatEvent(sessionID, ChatEvent{
		Type:      ChatEventAgentJoined,
		AgentID:   agent.id,
		AgentName: agent.name,
	})

	resultCh, err := s.agents.DispatchCommand(agent.id, taskID, command)
	if err != nil {
		s.finishSessionTask(taskID)
		s.BroadcastChatEvent(sessionID, ChatEvent{
			Type:  ChatEventError,
			Error: err.Error(),
		})
		return
	}

	go func() {
		res, ok := <-resultCh
		canceled := s.finishSessionTask(taskID)
		if !ok {
			s.BroadcastChatEvent(sessionID, ChatEvent{
				Type:  ChatEventError,
				Error: "agent disconnected",
			})
			return
		}
		if canceled {
			return
		}
		content := res.Output
		if res.Err != "" {
			content = "Error: " + res.Err
		}
		s.completeAssistantRun(sessionID, agent.id, agent.name, content, res.Turn)
	}()
}

func (s *Service) handleChatMessage(sessionID, content string, opts webproto.ChatPayload) {
	agent := s.sessionAgent(sessionID)
	if agent == nil {
		s.broadcastSystemMessage(sessionID, SysAgentNotConnected,
			"Agent is not connected. Reconnect the agent to continue chatting.", nil)
		return
	}

	taskID := generateID()
	s.registerSessionTask(taskID, sessionID, agent.id)

	s.BroadcastChatEvent(sessionID, ChatEvent{
		Type:      ChatEventAgentJoined,
		AgentID:   agent.id,
		AgentName: agent.name,
	})

	resultCh, err := s.agents.DispatchChatSession(agent.id, taskID, sessionID, content, opts)
	if err != nil {
		s.finishSessionTask(taskID)
		s.BroadcastChatEvent(sessionID, ChatEvent{
			Type:  ChatEventError,
			Error: err.Error(),
		})
		return
	}

	go func() {
		res, ok := <-resultCh
		canceled := s.finishSessionTask(taskID)
		if !ok {
			// Agent dropped mid-run: signal completion so the composer releases
			// instead of hanging on the streaming indicator (mirrors the command
			// path above).
			s.BroadcastChatEvent(sessionID, ChatEvent{
				Type:  ChatEventError,
				Error: "agent disconnected",
			})
			return
		}
		if canceled {
			return
		}
		reply := res.Output
		if res.Err != "" {
			reply = "Error: " + res.Err
		}
		s.completeAssistantRun(sessionID, agent.id, agent.name, reply, res.Turn)
	}()
}

// broadcastSystemMessage persists + broadcasts a system message. code names a
// translatable template rendered client-side via i18n (see the Sys* codes);
// fallback is the English text kept in Content for non-i18n consumers, logs and
// tests. params feeds i18n interpolation and is stored next to code so the
// message stays localizable after a reload.
func (s *Service) broadcastSystemMessage(sessionID, code, fallback string, params map[string]any) {
	now := time.Now()
	var meta json.RawMessage
	if code != "" {
		meta, _ = json.Marshal(map[string]any{"code": code, "params": params})
	}
	msg := &ChatMessage{
		ID:        generateID(),
		SessionID: sessionID,
		Role:      "system",
		Content:   fallback,
		Metadata:  meta,
		CreatedAt: now,
	}
	_ = s.store.AddMessage(context.Background(), msg)
	s.BroadcastChatEvent(sessionID, ChatEvent{
		Type:      ChatEventMessage,
		MessageID: msg.ID,
		Role:      "system",
		Content:   fallback,
		Code:      code,
		Params:    params,
	})
}

func (s *Service) broadcastScanComplete(scanID string, result *output.Result) {
	s.mu.Lock()
	sid, ok := s.taskSessions[scanID]
	s.mu.Unlock()
	if !ok {
		return
	}
	if s.finishSessionTask(scanID) {
		return
	}
	s.BroadcastChatEvent(sid, ChatEvent{
		Type:   ChatEventScanComplete,
		ScanID: scanID,
		Result: result,
	})
}

// completeAssistantRun is a run's terminal signal to the client. It always
// broadcasts the aggregate assistant message so the UI finalizes the turn and
// releases the composer — even when the run produced no final text (a tool-only
// turn, or an eval run that hit its round cap). Skipping the broadcast on empty
// content was what stranded the streaming indicator — the blinking cursor or the
// "working" dots — forever. The reply is persisted only when it carries text, so
// an empty completion never leaves a blank row in the transcript.
func (s *Service) completeAssistantRun(sessionID, agentID, agentName, content string, turn int) {
	content = strings.TrimRight(content, " \t\r\n")
	event := ChatEvent{
		Type:      ChatEventMessage,
		Role:      "assistant",
		AgentID:   agentID,
		AgentName: agentName,
		Turn:      turn,
		Content:   content,
	}
	if content != "" {
		msg := &ChatMessage{
			ID:        generateID(),
			SessionID: sessionID,
			Role:      "assistant",
			AgentID:   agentID,
			AgentName: agentName,
			Content:   content,
			CreatedAt: time.Now(),
		}
		if turn > 0 {
			if data, err := json.Marshal(map[string]any{"turn": turn}); err == nil {
				msg.Metadata = data
			}
		}
		_ = s.store.AddMessage(context.Background(), msg)
		event.MessageID = msg.ID
	}
	s.BroadcastChatEvent(sessionID, event)
}
