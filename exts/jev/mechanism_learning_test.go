//go:build full

package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"github.com/chainreactors/cyber/aop"
)

func (s *mechanismSuite) liveClient(t *testing.T, dir string, foregroundOnly bool) *jevapi.Client {
	t.Helper()
	upstream := jevapi.New(os.Getenv("TYPESAFE_API_KEY"), "", 20*time.Second)
	t.Cleanup(upstream.Close)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request jevapi.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		_, entry := request.Questions["entry"]
		_, route := request.Questions["route"]
		if foregroundOnly && !entry && !route {
			answers := map[string]jevapi.Answer{}
			for id := range request.Questions {
				answers[id] = answer(Defer)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"answers": answers, "usage": map[string]int{"input_tokens": 0, "output_tokens": 0}})
			return
		}
		started := time.Now()
		beforeUsage := upstream.Usage()
		response, err := upstream.Exchange(r.Context(), request)
		mu.Lock()
		file, fileErr := os.OpenFile(filepath.Join(dir, "jev-wire.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if fileErr == nil {
			fileErr = json.NewEncoder(file).Encode(map[string]any{"request": request, "response": response, "error": errorText(err), "elapsed_ms": time.Since(started).Milliseconds(), "upstream_usage": subtractUsage(upstream.Usage(), beforeUsage)})
			_ = file.Close()
		}
		mu.Unlock()
		if fileErr != nil {
			http.Error(w, "evidence write failed", 500)
			return
		}
		if err != nil {
			http.Error(w, "upstream request failed", http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(server.Close)
	client := jevapi.New("local-test-proxy", "", 60*time.Second)
	client.Endpoint = server.URL
	t.Cleanup(client.Close)
	return client
}

func mechanismState(user string, r mechanismRun) json.RawMessage {
	messages := []map[string]any{{"role": "user", "text": user}}
	for _, event := range r.Events {
		if event.Background {
			continue
		}
		if d := event.GetDispatch(); d != nil {
			messages = append(messages, map[string]any{"role": "assistant", "calls": []any{map[string]any{"id": d.Call.Id, "name": d.Call.Name, "arguments": json.RawMessage(d.Call.GetArguments().GetData())}}})
		}
		if v := event.GetResult(); v != nil {
			messages = append(messages, map[string]any{"role": "tool", "call_id": v.Result.CallId, "text": coreResultText(v.Result), "is_error": v.Result.IsError})
		}
	}
	messages = append(messages, map[string]any{"role": "assistant", "text": r.Output})
	return json.RawMessage(jsonText(map[string]any{"messages": messages, "omitted_evidence": 0}))
}

func mechanismManualClaim() Claim {
	return Claim{When: "The user requests creating an asynchronous native record and observing its final status", Question: "What progress is grounded by the current record state?", Options: map[string]string{"create": "Create once when no prior effect exists", "inspect": "Inspect current status after creation or a lost response", "report": "Report the current confirmed receipt", Defer: "Missing input or unsupported recovery"}}
}

type mechanismPromptProvider struct {
	provider.Provider
	guide string
	path  string
}

func (p mechanismPromptProvider) ChatCompletion(ctx context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
	if len(req.Messages) > 0 && strings.HasPrefix(provider.MessageText(req.Messages[0]), compilePrompt) {
		copyReq := *req
		copyReq.Messages = append([]*aop.Message(nil), req.Messages...)
		copyReq.Messages[0] = provider.TextMessage("system", provider.MessageText(req.Messages[0])+"\n\n"+p.guide)
		response, err := p.Provider.ChatCompletion(ctx, &copyReq)
		f, writeErr := os.OpenFile(p.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if writeErr == nil {
			writeErr = json.NewEncoder(f).Encode(map[string]any{"request": copyReq, "response": response, "error": errorText(err)})
			_ = f.Close()
		}
		if writeErr != nil {
			return nil, writeErr
		}
		return response, err
	}
	return p.Provider.ChatCompletion(ctx, req)
}

func (s *mechanismSuite) learning(t *testing.T) {
	if os.Getenv("TYPESAFE_API_KEY") == "" || os.Getenv("CYBER_API_KEY") == "" {
		t.Fatal("explicit learning run requires JEV and model credentials")
	}
	for _, row := range s.Rows {
		if row.Experiment == "E2-E4" && row.Condition == "normal" && !row.Accepted {
			s.Stages["learning"] = "blocked: handwritten normal workflow failed"
			return
		}
	}
	guide, err := os.ReadFile("testdata/mechanism-compile-guide.md")
	if err != nil {
		t.Fatal(err)
	}
	dir := s.directory("learning", "golden", 0)
	l := newMechanismLedger(0, "normal")
	if os.Getenv("JEV_MECHANISM_TRAINING") == "plain" {
		l.reference = "reference-0"
		s.Stages["training"] = "plain reference isolates replay quoting from semantic review/generation"
	} else {
		s.Stages["training"] = "quoted Unicode reference"
	}
	client := mechanismFake(t, "run")
	e, cfg, _ := testInstallation(t, Config{Mode: "auto", Directory: dir}, client, l.command())
	cfg.SessionID = "learning-golden"
	user := "Create an asynchronous record for " + l.resource + " with reference " + l.reference + "; query status after submission, including transient failures; report the actual final receipt."
	run := mechanismRunAgent(t, e, cfg, mechanismAsyncSource, l.arguments(), user, t.Context(), false)
	if run.Reports != 1 || l.effects != 1 || l.wrong != 0 || !mechanismHasReceipt(run, l.receipt) {
		s.Stages["learning"] = "blocked: golden trace failed"
		return
	}
	state := mechanismState(user, run)
	caps, err := e.capabilities(cfg, state)
	if err != nil {
		t.Fatal(err)
	}
	writeLiveReport(t, filepath.Join(dir, "training.json"), map[string]any{"state": state, "capabilities": caps, "arguments": l.arguments(), "source": mechanismAsyncSource})
	variants := map[string]string{
		"valid":              mechanismAsyncSource,
		"effect_as_read":     strings.Replace(mechanismAsyncSource, "quote(args.reference),false)", "quote(args.reference),true)", 1),
		"stale_poll":         strings.Replace(mechanismAsyncSource, "quote(args.resource),true)", "quote(args.resource),false)", 1),
		"old_parameter":      strings.Replace(mechanismAsyncSource, "quote(args.reference)", `quote('recorded-example')`, 1),
		"wrong_result_field": strings.Replace(mechanismAsyncSource, "r.data.state==='complete'", "r.data.Status==='complete'", 1),
		"skip_submit":        strings.Replace(mechanismAsyncSource, "const submitted=call('mechanism submit '+quote(args.resource)+' '+quote(args.reference),false);", "const submitted={};", 1),
		"fabricated_report":  `js:function(context,args){return {report:{receipt:'receipt-fabricated'}};}`,
		"missing_command":    strings.Replace(mechanismAsyncSource, "'mechanism submit '", "'mechanism nonexistent '", 1),
	}
	// Oracle first: every mutant is actually exercised through the same runtime.
	for _, name := range []string{"valid", "effect_as_read", "stale_poll", "old_parameter", "wrong_result_field", "skip_submit", "fabricated_report", "missing_command"} {
		source := variants[name]
		for trial := 0; trial < 3; trial++ {
			t.Run(fmt.Sprintf("review/%s/%d", name, trial), func(t *testing.T) {
				path := s.directory("review-"+name, "real", trial)
				oracleLab := newMechanismLedger(trial+100, "normal")
				if name == "effect_as_read" {
					oracleLab.mode = "handoff"
				}
				oe, ocfg, _ := testInstallation(t, Config{Mode: "auto", Directory: filepath.Join(path, "oracle")}, mechanismFake(t, "run"), oracleLab.command())
				ocfg.SessionID = fmt.Sprintf("mutant-%s-%d", name, trial)
				orun := mechanismRunAgent(t, oe, ocfg, source, oracleLab.arguments(), "Create the requested record", t.Context(), name == "effect_as_read")
				business := oracleLab.effects == 1 && oracleLab.reads == 3 && oracleLab.wrong == 0 && orun.Reports == 1 && mechanismHasReceipt(orun, oracleLab.receipt)
				writeLiveReport(t, filepath.Join(path, "oracle.json"), map[string]any{"run": orun, "ledger": oracleLab.snapshot(), "business_pass": business})
				reflex := Reflex{When: mechanismManualClaim().When, Decide: "Create once, inspect fresh state, report actual completion", Observe: source, arguments: l.arguments()}
				validation := reflex.validate()
				var witnesses []map[string]any
				if validation == nil {
					validation = verifyObserve(t.Context(), &reflex, state, caps)
				}
				if validation == nil {
					witnesses, validation = observationWitnesses(t.Context(), &reflex, state, caps)
				}
				reviewErr := validation
				reviewed := false
				if validation == nil {
					reviewed = true
					re, _, _ := testInstallation(t, Config{Mode: "auto", Directory: filepath.Join(path, "review")}, s.liveClient(t, path, false))
					ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
					reviewErr = re.reviewReflex(ctx, &reflex, &compilation{capabilities: caps}, witnesses)
					cancel()
				}
				admitted := reviewErr == nil
				expected := name == "valid"
				s.add(t, mechanismRow{Experiment: "review", Condition: name, Seed: trial, Accepted: business == expected && admitted == expected, Expected: "correct program admitted; independently failing mutant rejected", Observed: map[string]any{"business_pass": business, "admitted": admitted, "semantic_review_executed": reviewed, "validation_error": errorText(validation)}, Source: mechanismHash(source), Evidence: path, Error: errorText(reviewErr)})
			})
		}
	}
	// Compile can be studied even if review is faulty: admission and business
	// success are recorded separately, including drafts rejected before publish.
	compilePassed := false
	for _, condition := range []string{"baseline", "guide"} {
		for trial := 0; trial < 3; trial++ {
			t.Run(fmt.Sprintf("compile/%s/%d", condition, trial), func(t *testing.T) {
				path := s.directory("compile", condition, trial)
				cl := s.liveClient(t, path, false)
				ce, ccfg, _ := testInstallation(t, Config{Mode: "auto", Directory: path}, cl, newMechanismLedger(0, "normal").command())
				llm, err := provider.NewProvider(&provider.ProviderConfig{Provider: "openai", APIKey: os.Getenv("CYBER_API_KEY"), BaseURL: os.Getenv("CYBER_BASE_URL"), Model: os.Getenv("CYBER_MODEL"), Timeout: 75})
				if err != nil {
					t.Fatal(err)
				}
				meter := &benchmarkProvider{Provider: llm, tracePath: filepath.Join(path, "llm.jsonl")}
				ccfg.Provider = meter
				ccfg.Model = os.Getenv("CYBER_MODEL")
				if condition == "guide" {
					meter.Provider = mechanismPromptProvider{Provider: llm, guide: string(guide), path: filepath.Join(path, "guided-requests.jsonl")}
				}
				claim := mechanismManualClaim()
				id := "c" + digest(claim)[:16]
				_, err = ce.updateLibrary(func(lib *library) (bool, error) {
					lib.Claims[id] = claimRecord{Claim: claim, Task: "golden"}
					return true, nil
				})
				if err != nil {
					t.Fatal(err)
				}
				job := declaration{cfg: ccfg, task: "golden", session: "compile-" + condition, turn: fmt.Sprint(trial), final: true, state: state, focus: []string{"Complete the asynchronous record"}}
				ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
				err = ce.compile(ctx, job, id)
				cancel()
				business := false
				results := []map[string]any{}
				drafts := mechanismDrafts(t, filepath.Join(path, "llm.jsonl"))
				draftResults := []map[string]any{}
				for i, draft := range drafts {
					draft.When = claim.When
					draft.Decide = "Create once, poll current status, report current receipt"
					if validation := draft.validate(); validation != nil {
						draftResults = append(draftResults, map[string]any{"draft": i, "source_sha256": mechanismHash(draft.Observe), "error": validation.Error()})
						continue
					}
					passed, qualification := s.qualifyNative(t, filepath.Join(path, "drafts", fmt.Sprint(i)), *draft)
					draftResults = append(draftResults, map[string]any{"draft": i, "source_sha256": mechanismHash(draft.Observe), "business_pass": passed, "qualification": qualification})
				}
				writeLiveReport(t, filepath.Join(path, "draft-qualification.json"), draftResults)
				for _, record := range ce.snapshot().Reflexes {
					business, results = s.qualifyNative(t, path, record.Reflex)
				}
				writeLiveReport(t, filepath.Join(path, "qualification.json"), results)
				if business {
					compilePassed = true
				}
				s.add(t, mechanismRow{Experiment: "compile", Condition: condition, Seed: trial, Accepted: err == nil && business, Expected: "publish reusable source that passes 20 changed-parameter/fault variants", Observed: map[string]any{"published": len(ce.snapshot().Reflexes), "business_pass": business, "drafts": len(drafts), "llm_usage": meter.snapshot().usage, "jev_usage": cl.Usage()}, Evidence: path, Error: errorText(err)})
			})
		}
	}
	if !compilePassed {
		s.Stages["claim"] = "blocked: no generated Reflex passed independent qualification"
		return
	}
	s.claimExperiments(t, state)
}

func mechanismDrafts(t *testing.T, path string) []*Reflex {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := []*Reflex{}
	type loggedMessage struct {
		Content []struct {
			Value struct {
				Text *struct {
					Text string `json:"text"`
				} `json:"Text"`
			} `json:"Value"`
		} `json:"content"`
	}
	messageText := func(m loggedMessage) string {
		var out strings.Builder
		for _, part := range m.Content {
			if part.Value.Text != nil {
				out.WriteString(part.Value.Text.Text)
			}
		}
		return out.String()
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		var row struct {
			Request  struct{ Messages []loggedMessage } `json:"request"`
			Response *struct {
				Choices []struct{ Message loggedMessage }
			} `json:"response"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		if len(row.Request.Messages) == 0 || !strings.HasPrefix(messageText(row.Request.Messages[0]), compilePrompt) || row.Response == nil || len(row.Response.Choices) != 1 {
			continue
		}
		var reflex *Reflex
		if err := decodeReflex(strings.TrimSpace(messageText(row.Response.Choices[0].Message)), &reflex); err == nil && reflex != nil {
			out = append(out, reflex)
		}
	}
	return out
}

func TestMechanismQualifyRecordedDrafts(t *testing.T) {
	input := os.Getenv("JEV_MECHANISM_DRAFT_INPUT")
	if input == "" {
		t.Skip("recorded experiment directory required")
	}
	output := os.Getenv("JEV_MECHANISM_REPORT_DIR")
	if output == "" {
		t.Fatal("fresh output directory required")
	}
	if _, err := os.Stat(filepath.Join(output, "draft-summary.json")); err == nil {
		t.Fatal("use fresh output directory")
	}
	s := &mechanismSuite{Root: output, Sources: mechanismSources(t), Harness: mechanismHarness(t), Stages: map[string]string{}}
	paths, err := filepath.Glob(filepath.Join(input, "compile", "*", "*", "llm.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	rows := []map[string]any{}
	for _, path := range paths {
		drafts := mechanismDrafts(t, path)
		for index, draft := range drafts {
			condition := filepath.Base(filepath.Dir(filepath.Dir(path)))
			trial := filepath.Base(filepath.Dir(path))
			t.Run(fmt.Sprintf("%s/%s/%d", condition, trial, index), func(t *testing.T) {
				draft.When = mechanismManualClaim().When
				draft.Decide = "Create once, fresh inspection, honest report"
				row := map[string]any{"input": path, "draft": index, "source_sha256": mechanismHash(draft.Observe)}
				if err := draft.validate(); err != nil {
					row["error"] = err.Error()
				} else {
					passed, qualification := s.qualifyNative(t, filepath.Join(output, condition, trial, fmt.Sprint(index)), *draft)
					row["business_pass"] = passed
					writeLiveReport(t, filepath.Join(output, condition, trial, fmt.Sprint(index), "qualification.json"), qualification)
				}
				rows = append(rows, row)
				writeLiveReport(t, filepath.Join(output, "draft-summary.json"), map[string]any{"input": input, "rows": rows, "production_sha256": s.Sources, "harness_sha256": s.Harness})
			})
		}
	}
	if len(rows) == 0 {
		t.Fatal("no recorded generated drafts")
	}
}

func (s *mechanismSuite) qualifyNative(t *testing.T, path string, reflex Reflex) (bool, []map[string]any) {
	t.Helper()
	all := true
	rows := []map[string]any{}
	for seed := 0; seed < 20; seed++ {
		t.Run(fmt.Sprintf("qualification/%d", seed), func(t *testing.T) {
			mode := []string{"normal", "business_503", "tool_error", "no_query", "handoff"}[seed%5]
			l := newMechanismLedger(seed+200, mode)
			e, cfg, _ := testInstallation(t, Config{Mode: "auto", Directory: filepath.Join(path, "qualification", fmt.Sprint(seed))}, mechanismFake(t, "run"), l.command())
			cfg.SessionID = fmt.Sprintf("qualification-%d", seed)
			r := mechanismRunReflex(t, e, cfg, reflex, l.arguments(), "Create record "+l.resource+" reference "+l.reference, t.Context(), mode == "handoff")
			ok := l.effects == 1 && l.wrong == 0 && r.SourceStable && r.Generations == 0
			if mode == "no_query" {
				ok = ok && r.Reports == 0
			} else {
				ok = ok && r.Reports == 1 && mechanismHasReceipt(r, l.receipt)
			}
			if !ok {
				all = false
			}
			rows = append(rows, map[string]any{"seed": seed, "mode": mode, "accepted": ok, "ledger": l.snapshot(), "run": r})
		})
	}
	return all, rows
}

func (s *mechanismSuite) claimExperiments(t *testing.T, state json.RawMessage) {
	for _, condition := range []string{"same_workflow", "empty_library", "new_workflow", "unrelated", "final_prose"} {
		for trial := 0; trial < 3; trial++ {
			t.Run(fmt.Sprintf("claim/%s/%d", condition, trial), func(t *testing.T) {
				path := s.directory("claim", condition, trial)
				cl := s.liveClient(t, path, false)
				ce, cfg, _ := testInstallation(t, Config{Mode: "auto", Directory: path}, cl, newMechanismLedger(0, "normal").command())
				llm, err := provider.NewProvider(&provider.ProviderConfig{Provider: "openai", APIKey: os.Getenv("CYBER_API_KEY"), BaseURL: os.Getenv("CYBER_BASE_URL"), Model: os.Getenv("CYBER_MODEL"), Timeout: 75})
				if err != nil {
					t.Fatal(err)
				}
				meter := &benchmarkProvider{Provider: llm, tracePath: filepath.Join(path, "llm.jsonl")}
				cfg.Provider = meter
				cfg.Model = os.Getenv("CYBER_MODEL")
				claim := mechanismManualClaim()
				id := "c" + digest(claim)[:16]
				if condition != "empty_library" {
					_, err = ce.updateLibrary(func(lib *library) (bool, error) {
						lib.Claims[id] = claimRecord{Claim: claim, Task: "golden"}
						return true, nil
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				focus := "Create the asynchronous record with new parameters and inspect current completion"
				input := state
				if condition == "same_workflow" {
					input = json.RawMessage(strings.ReplaceAll(strings.ReplaceAll(string(state), "resource-0", "resource-next"), "reference-0", "reference-next"))
				}
				if condition == "new_workflow" {
					focus = "Add exactly two units using mechanism add, report the observed quantity; do not submit a record"
					input = json.RawMessage(`{"messages":[{"role":"user","text":"Add two units to resource-new and report its observed quantity"}]}`)
				}
				if condition == "unrelated" {
					focus = "Summarize the deployment incident without performing any resource operation"
					input = json.RawMessage(`{"messages":[{"role":"user","text":"Summarize a deployment incident"},{"role":"assistant","text":"The incident summary is complete"}]}`)
				}
				if condition == "final_prose" {
					focus = "The final explanation has been delivered; no additional work is requested"
					input = json.RawMessage(`{"messages":[{"role":"user","text":"Thank you"},{"role":"assistant","text":"Done"}]}`)
				}
				// Nonfinal job isolates discovery/grouping; no automatic compilation.
				ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
				err = ce.declare(ctx, declaration{cfg: cfg, state: input, focus: []string{focus}, task: fmt.Sprintf("claim-%s-%d", condition, trial), final: false})
				cancel()
				choice := mechanismRecordedChoice(t, filepath.Join(path, "jev-wire.jsonl"), "claim0")
				ok := err == nil
				count := len(ce.snapshot().Claims)
				switch condition {
				case "same_workflow":
					ok = ok && choice == id && count == 1
				case "empty_library":
					ok = ok && choice == "new" && count > 0
				case "new_workflow":
					ok = ok && choice == "new" && count > 1
				default:
					ok = ok && choice == Defer && count == 1
				}
				business := false
				if condition == "empty_library" && ok {
					for claimID := range ce.snapshot().Claims {
						ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
						compileErr := ce.compile(ctx, declaration{cfg: cfg, state: state, focus: []string{focus}, task: "generated-claim", final: true}, claimID)
						cancel()
						if compileErr != nil {
							err = compileErr
						}
					}
					for _, record := range ce.snapshot().Reflexes {
						passed, qualification := s.qualifyNative(t, path, record.Reflex)
						business = business || passed
						writeLiveReport(t, filepath.Join(path, "qualification.json"), qualification)
					}
					ok = ok && business
				}
				s.add(t, mechanismRow{Experiment: "claim", Condition: condition, Seed: trial, Accepted: ok, Expected: "same-scene reuse, independent new scene, prose defer; empty-library generation reaches qualified execution", Observed: map[string]any{"choice": choice, "claims": ce.snapshot().Claims, "business_pass": business, "llm_usage": meter.snapshot().usage, "jev_usage": cl.Usage()}, Evidence: path, Error: errorText(err)})
			})
		}
	}
}

func mechanismRecordedChoice(t *testing.T, path, id string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		var row struct {
			Response *jevapi.Response `json:"response"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		if row.Response != nil {
			if a, ok := row.Response.Answers[id]; ok {
				return a.Choice
			}
		}
	}
	return ""
}
