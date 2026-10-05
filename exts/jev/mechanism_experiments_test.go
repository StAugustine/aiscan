//go:build full

package jev

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

// These experiments intentionally preserve production defects. A diagnostic
// run records counterexamples; STRICT=1 additionally fails their acceptance.
// Model stubs replace inference only, never the Agent/Executor/journal path.
type mechanismRow struct {
	Experiment string         `json:"experiment"`
	Condition  string         `json:"condition"`
	Seed       int            `json:"seed"`
	Accepted   bool           `json:"accepted"`
	Expected   string         `json:"expected"`
	Observed   map[string]any `json:"observed"`
	Source     string         `json:"source_sha256,omitempty"`
	Error      string         `json:"error,omitempty"`
	Evidence   string         `json:"evidence,omitempty"`
}

type mechanismSuite struct {
	Root      string            `json:"-"`
	Started   string            `json:"started"`
	Sources   map[string]string `json:"production_sha256"`
	Harness   map[string]string `json:"harness_sha256"`
	Unchanged bool              `json:"production_unchanged"`
	Rows      []mechanismRow    `json:"rows"`
	Stages    map[string]string `json:"stages"`
}

func mechanismHarness(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, path := range []string{"mechanism_experiments_test.go", "mechanism_browser_test.go", "mechanism_learning_test.go", "testdata/mechanism-compile-guide.md", "testdata/mechanism_runner.mjs", "testdata/playwright_takeover_lab.py"} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		out[path] = mechanismHash(string(b))
	}
	if path, err := os.Executable(); err == nil {
		if b, err := os.ReadFile(path); err == nil {
			out["executed_test_binary"] = mechanismHash(string(b))
		}
	}
	return out
}

func mechanismHash(value string) string {
	s := sha256.Sum256([]byte(value))
	return hex.EncodeToString(s[:])
}

func mechanismSources(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, dir := range []string{".", "../../tools/playwright"} {
		paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			out[filepath.ToSlash(path)] = mechanismHash(string(b))
		}
	}
	return out
}

func (s *mechanismSuite) checkpoint(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(s.Root, 0700); err != nil {
		t.Fatal(err)
	}
	s.Unchanged = digest(s.Sources) == digest(mechanismSources(t))
	writeLiveReport(t, filepath.Join(s.Root, "summary.json"), s)
}

func (s *mechanismSuite) add(t *testing.T, row mechanismRow) {
	t.Helper()
	s.Rows = append(s.Rows, row)
	s.checkpoint(t)
	if row.Seed == 0 || !row.Accepted {
		t.Logf("%s/%s seed=%d accepted=%t evidence=%s error=%s", row.Experiment, row.Condition, row.Seed, row.Accepted, row.Evidence, row.Error)
	}
}

func mechanismSeeds() int { return 20 }

func (s *mechanismSuite) directory(experiment, condition string, seed int) string {
	return filepath.Join(s.Root, experiment, condition, fmt.Sprint(seed))
}

type mechanismLedger struct {
	mu                                 sync.Mutex
	resource, reference, receipt, mode string
	effects, reads, wrong              int
	supplemented                       bool
	cancel                             context.CancelFunc
	calls                              []string
}

func newMechanismLedger(seed int, mode string) *mechanismLedger {
	return &mechanismLedger{resource: fmt.Sprintf("resource-%d", seed), reference: fmt.Sprintf("参考 '%d' \\ value \"current\"", seed), receipt: "receipt-" + aop.EnvelopeID(), mode: mode}
}

func (l *mechanismLedger) arguments() map[string]any {
	return map[string]any{"resource": l.resource, "reference": l.reference, "query": l.mode != "no_query", "mode": l.mode}
}

func (l *mechanismLedger) command() coretool.Command {
	return coretool.Command{Name: "mechanism", Usage: "mechanism add <resource> | submit <resource> <reference> | status <resource> | supplement | pending | cancel. add increments one unit. submit commits once, may return HTTP 503 JSON or a tool error after commit; neither authorizes resubmission. status is effect-free, returns HTTP 503, pending, then current receipt. No status is available in no_query mode. Output is JSON with resource, reference, state, receipt, quantity or http_status.", Run: func(ctx context.Context, ex *coretool.Execution) (any, error) {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.calls = append(l.calls, strings.Join(ex.Args, " "))
		if len(ex.Args) == 0 {
			l.wrong++
			return nil, fmt.Errorf("missing operation")
		}
		var data any
		switch ex.Args[0] {
		case "add":
			if len(ex.Args) != 2 || ex.Args[1] != l.resource {
				l.wrong++
				return nil, fmt.Errorf("wrong add target")
			}
			l.effects++
			data = map[string]any{"quantity": l.effects, "resource": l.resource}
		case "submit":
			if len(ex.Args) != 3 || ex.Args[1] != l.resource || ex.Args[2] != l.reference || l.effects != 0 {
				l.wrong++
				return nil, fmt.Errorf("wrong/repeated submit")
			}
			l.effects++
			data = map[string]any{"resource": l.resource, "http_status": 202, "state": "pending"}
			if l.mode == "business_503" {
				data = map[string]any{"resource": l.resource, "http_status": 503, "state": "unknown"}
			}
			if l.mode == "tool_error" || l.mode == "no_query" {
				return nil, fmt.Errorf("response lost after commit")
			}
		case "status":
			if len(ex.Args) != 2 || ex.Args[1] != l.resource || l.effects != 1 || l.mode == "no_query" {
				l.wrong++
				return nil, fmt.Errorf("status unavailable/wrong target")
			}
			l.reads++
			data = map[string]any{"resource": l.resource, "http_status": 503, "state": "temporary outage"}
			if l.reads == 2 {
				data = map[string]any{"resource": l.resource, "state": "pending"}
			}
			if l.reads >= 3 {
				data = map[string]any{"resource": l.resource, "reference": l.reference, "state": "complete", "receipt": l.receipt}
			}
		case "supplement":
			l.supplemented = true
			data = map[string]any{"supplemented": true}
		case "pending":
			l.reads++
			data = map[string]any{"state": "pending"}
		case "cancel":
			if l.cancel != nil {
				l.cancel()
			}
			return nil, ctx.Err()
		default:
			l.wrong++
			return nil, fmt.Errorf("unsupported operation")
		}
		_, err := fmt.Fprint(ex.Stdout, jsonText(data))
		return nil, err
	}}
}

func (l *mechanismLedger) snapshot() map[string]any {
	l.mu.Lock()
	defer l.mu.Unlock()
	return map[string]any{"effects": l.effects, "reads": l.reads, "wrong": l.wrong, "calls": append([]string(nil), l.calls...), "supplemented": l.supplemented}
}

const mechanismAsyncSource = `js:function(context,args){
 if(!args||!args.resource||!args.reference||typeof args.query!=='boolean')return {defer:'missing current arguments',parameters:'resource, reference, query, mode'};
 function call(command,read){return execute({name:'bash',arguments:{command:command},read:read});}
 const submitted=call('mechanism submit '+quote(args.resource)+' '+quote(args.reference),false);
 if(args.mode==='handoff'&&!context.history.some(function(r){return r.arguments.command==='mechanism supplement'&&!r.is_error;}))return {defer:'missing supplementary read'};
 if(!args.query)return {defer:'committed outcome unknown; status unavailable; preserve prior effect'};
 for(let i=0;i<8;i++){
  const r=call('mechanism status '+quote(args.resource),true);
  if(r.is_error)return {defer:'status unavailable'};
  if(r.data&&r.data.state==='complete'&&r.data.resource===args.resource&&r.data.reference===args.reference&&r.data.receipt)return {report:r.data};
 }
 return {defer:'still pending'};
}`

const mechanismRepeatSource = `js:function(context,args){
 if(!args||!args.resource||!args.variant)return {defer:'missing current arguments',parameters:'resource, variant'};
 const command='mechanism add '+quote(args.resource);
 execute({name:'bash',arguments:{command:command},read:false});
 const r=execute({name:'bash',arguments:{command:command+(args.variant==='cosmetic'?' ':'')},read:false});
 return r.data&&r.data.quantity===2?{report:r.data}:{defer:'two intended units were not added'};
}`

const mechanismSemanticSource = `js:function(context,args){
 if(!args||!args.resource||!args.reference||typeof args.query!=='boolean')return {defer:'missing current arguments',parameters:'resource, reference, query, mode'};
 const route=jev({state:{user:context.user},questions:{route:{type:'choice',instructions:'Select the current user request: create an asynchronous record, inspect the existing record, or cancel without effects. Unrelated requests defer.',criteria:{create:'Create the requested record',inspect:'Inspect an existing record without creation',cancel:'Cancel; perform no operation',defer:'Unsupported task'}}}}).answers.route.choice;
 if(route==='defer')return {defer:'unsupported task'};
 if(route==='cancel')return {report:{cancelled:true}};
 if(route==='create')execute({name:'bash',arguments:{command:'mechanism submit '+quote(args.resource)+' '+quote(args.reference)},read:false});
 for(let i=0;i<8;i++){
  const r=execute({name:'bash',arguments:{command:'mechanism status '+quote(args.resource)},read:true});
  if(r.is_error)return {defer:'status failed'};
  if(r.data&&r.data.state==='complete'&&r.data.resource===args.resource&&r.data.reference===args.reference&&r.data.receipt)return {report:r.data};
 }
 return {defer:'pending'};
}`

type mechanismRun struct {
	Output                                                                         string `json:"output"`
	Error                                                                          string `json:"error,omitempty"`
	Reports, Takeovers, Dispatches, Parameters, Generations, MainCalls, Background int
	Handoffs                                                                       []string        `json:"handoffs"`
	Events                                                                         []*RuntimeEvent `json:"events"`
	SourceStable                                                                   bool            `json:"source_stable"`
	Usage                                                                          *aop.TokenUsage `json:"jev_usage,omitempty"`
	ReportValues                                                                   []any           `json:"report_values,omitempty"`
	PrivateHandoffReasons                                                          []string        `json:"private_handoff_reasons,omitempty"`
}

func (r mechanismRun) metrics() map[string]any {
	return map[string]any{"reports": r.Reports, "takeovers": r.Takeovers, "dispatches": r.Dispatches, "parameters": r.Parameters, "generations": r.Generations, "ordinary_calls": r.MainCalls, "handoffs": r.Handoffs, "private_handoff_reasons": r.PrivateHandoffReasons, "source_stable": r.SourceStable, "jev_usage": r.Usage, "error": r.Error}
}

func mechanismHasReceipt(r mechanismRun, receipt string) bool {
	for _, value := range r.ReportValues {
		if data, ok := value.(map[string]any); ok && data["receipt"] == receipt {
			return true
		}
	}
	return false
}

// Record background requests, but reject them before inference. This lets us
// exercise production hooks without altering the foreground runtime.
func mechanismFake(t *testing.T, branch string) *jevapi.Client {
	return fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		if _, ok := req.Questions["entry"]; ok {
			return runtimeAnswers(req, branch)
		}
		out := map[string]jevapi.Answer{}
		for id := range req.Questions {
			choice := Defer
			if id == "route" {
				choice = branch
				if choice == "run" {
					choice = "create"
				}
			}
			out[id] = answer(choice)
		}
		return out
	})
}

func mechanismRunAgent(t *testing.T, e *Extension, cfg agent.Config, source string, args map[string]any, user string, ctx context.Context, supplement bool) mechanismRun {
	return mechanismRunReflex(t, e, cfg, Reflex{When: "The user requests creating, inspecting or cancelling a native record, or a supported browser business workflow", Decide: "Select current requested work; unrelated requests defer", Observe: source}, args, user, ctx, supplement)
}

func (s *mechanismSuite) verifier(t *testing.T) {
	for seed := 0; seed < 20; seed++ {
		for _, condition := range []string{"plain", "quoted"} {
			t.Run(fmt.Sprintf("%s/%d", condition, seed), func(t *testing.T) {
				l := newMechanismLedger(seed, "normal")
				if condition == "plain" {
					l.reference = fmt.Sprintf("reference-%d", seed)
				}
				dir := s.directory("E7", condition, seed)
				e, cfg, _ := testInstallation(t, Config{Mode: "auto", Directory: dir}, mechanismFake(t, "run"), l.command())
				cfg.SessionID = fmt.Sprintf("verify-%s-%d", condition, seed)
				user := "Create record " + l.resource + " reference " + l.reference
				run := mechanismRunAgent(t, e, cfg, mechanismAsyncSource, l.arguments(), user, t.Context(), false)
				state := mechanismState(user, run)
				caps, err := e.capabilities(cfg, state)
				if err != nil {
					t.Fatal(err)
				}
				r := Reflex{When: mechanismManualClaim().When, Decide: "Create once and poll", Observe: mechanismAsyncSource, arguments: l.arguments()}
				if err := r.validate(); err != nil {
					t.Fatal(err)
				}
				validation := verifyObserve(t.Context(), &r, state, caps)
				business := l.effects == 1 && l.reads == 3 && l.wrong == 0 && mechanismHasReceipt(run, l.receipt)
				// Compare actual shell argument values, not string encodings.
				old := l.reference
				newValue := old + "__probe_" + digest(old)[:12]
				originalCommand := "mechanism submit " + mechanismQuote(t, l.resource) + " " + mechanismQuote(t, old)
				regenerated := "mechanism submit " + mechanismQuote(t, l.resource) + " " + mechanismQuote(t, newValue)
				textual := strings.ReplaceAll(originalCommand, old, newValue)
				writeLiveReport(t, filepath.Join(dir, "trace.json"), map[string]any{"run": run, "state": state, "capabilities": caps, "arguments": l.arguments()})
				s.add(t, mechanismRow{Experiment: "E7", Condition: condition, Seed: seed, Accepted: business && validation == nil, Expected: "parameterized correct program passes runtime and replay validation", Observed: map[string]any{"business_pass": business, "replay_pass": validation == nil, "original_command": originalCommand, "regenerated_command": regenerated, "textually_replaced_command": textual, "same_command_after_replacement": textual == regenerated}, Source: mechanismHash(mechanismAsyncSource), Evidence: dir, Error: errorText(validation)})
			})
		}
	}
}

func mechanismRunReflex(t *testing.T, e *Extension, cfg agent.Config, reflex Reflex, args map[string]any, user string, ctx context.Context, supplement bool) mechanismRun {
	t.Helper()
	if err := reflex.validate(); err != nil {
		t.Fatal(err)
	}
	_, err := e.updateLibrary(func(lib *library) (bool, error) {
		lib.Reflexes["r"+digest(reflex)[:16]] = reflexRecord{Reflex: reflex}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before := digest(e.snapshot())
	var result mechanismRun
	var mu sync.Mutex
	sub := e.stream.Observe(func(event *aop.Event) {
		v := new(RuntimeEvent)
		if event.SessionId == cfg.SessionID && event.GetExtension() != nil && event.GetExtension().UnmarshalTo(v) == nil {
			mu.Lock()
			result.Events = append(result.Events, v)
			mu.Unlock()
		}
	})
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		if provider.MessageText(req.Messages[0]) == claimPrompt || provider.MessageText(req.Messages[0]) == compilePrompt {
			return nil, fmt.Errorf("unexpected background generation")
		}
		if strings.HasPrefix(provider.MessageText(req.Messages[0]), "Supply only the CURRENT") {
			mu.Lock()
			result.Parameters++
			mu.Unlock()
			if len(req.Tools) > 0 {
				return nil, fmt.Errorf("parameter extraction exposed tools")
			}
			return reply(provider.TextMessage("assistant", jsonText(args))), nil
		}
		last := provider.MessageText(req.Messages[len(req.Messages)-1])
		e.mu.Lock()
		for _, record := range e.tasks {
			if len(record.Handoff) > 0 {
				var h map[string]any
				if json.Unmarshal(record.Handoff, &h) == nil {
					if reason, ok := h["reason"].(string); ok {
						result.PrivateHandoffReasons = append(result.PrivateHandoffReasons, reason)
					}
				}
			}
		}
		e.mu.Unlock()
		if supplement && strings.Contains(last, "missing supplementary read") {
			supplement = false
			return reply(&aop.Message{Role: "assistant", Content: []*aop.Content{action("mechanism supplement")}}), nil
		}
		return reply(provider.TextMessage("assistant", last)), nil
	})
	beforeUsage := e.client.Usage()
	r, err := agent.NewAgent(cfg).Run(ctx, agent.TextInput(user))
	result.Error = errorText(err)
	if r != nil {
		result.Output = r.Output
		for _, m := range r.Messages {
			result.MainCalls += len(provider.MessageToolCalls(m))
		}
	}
	settle(t, e)
	_ = sub.Close(context.Background())
	result.SourceStable = before == digest(e.snapshot())
	result.Usage = subtractUsage(e.client.Usage(), beforeUsage)
	for _, event := range result.Events {
		if event.Background {
			result.Background++
			if g := event.GetGeneration(); g != nil && g.State == "started" {
				result.Generations++
			}
			continue
		}
		if event.GetTakeover() != nil {
			result.Takeovers++
		}
		if event.GetDispatch() != nil {
			result.Dispatches++
		}
		if o := event.GetObservation(); o != nil {
			var value map[string]any
			if json.Unmarshal([]byte(o.StateJson), &value) == nil {
				if output, ok := value["result"].(map[string]any); ok {
					if actual, ok := output[report]; ok {
						result.ReportValues = append(result.ReportValues, actual)
					}
				}
			}
		}
		if h := event.GetHandoff(); h != nil {
			result.Handoffs = append(result.Handoffs, h.Reason)
			if h.Reason == report {
				result.Reports++
			}
		}
	}
	return result
}

func (s *mechanismSuite) native(t *testing.T) {
	for seed := 0; seed < mechanismSeeds(); seed++ {
		for _, condition := range []string{"direct", "identical", "cosmetic"} {
			t.Run(fmt.Sprintf("E1/%s/%d", condition, seed), func(t *testing.T) {
				l := newMechanismLedger(seed, "normal")
				dir := s.directory("E1", condition, seed)
				e, cfg, _ := testInstallation(t, Config{Mode: "auto", Directory: dir}, mechanismFake(t, "run"), l.command())
				cfg.SessionID = fmt.Sprintf("E1-%s-%d", condition, seed)
				row := mechanismRow{Experiment: "E1", Condition: condition, Seed: seed, Expected: "two effects; no wrong target", Evidence: dir, Source: mechanismHash(mechanismRepeatSource)}
				if condition == "direct" {
					for i := 0; i < 2; i++ {
						_, err := cfg.Tools.ExecuteTool(t.Context(), "bash", jsonText(map[string]any{"command": "mechanism add " + l.resource}))
						if err != nil {
							t.Fatal(err)
						}
					}
				} else {
					args := l.arguments()
					args["variant"] = condition
					r := mechanismRunAgent(t, e, cfg, mechanismRepeatSource, args, "Add exactly two units of "+l.resource, t.Context(), false)
					writeLiveReport(t, filepath.Join(dir, "run.json"), r)
					row.Error = r.Error
				}
				row.Observed = l.snapshot()
				row.Accepted = l.effects == 2 && l.wrong == 0
				s.add(t, row)
			})
		}
		for _, condition := range []string{"normal", "business_503", "tool_error", "no_query", "handoff", "cosmetic_resume"} {
			t.Run(fmt.Sprintf("E2-E4/%s/%d", condition, seed), func(t *testing.T) {
				l := newMechanismLedger(seed, condition)
				if condition == "cosmetic_resume" {
					l.mode = "handoff"
				}
				dir := s.directory("E2-E4", condition, seed)
				e, cfg, _ := testInstallation(t, Config{Mode: "auto", Directory: dir}, mechanismFake(t, "run"), l.command())
				cfg.SessionID = fmt.Sprintf("async-%s-%d", condition, seed)
				source := mechanismAsyncSource
				if condition == "cosmetic_resume" {
					source = strings.Replace(source, "quote(args.reference),false)", "quote(args.reference)+(context.history.some(function(r){return r.arguments.command==='mechanism supplement';})?' ':''),false)", 1)
				}
				r := mechanismRunAgent(t, e, cfg, source, l.arguments(), "Create the asynchronous record "+l.resource+" with reference "+l.reference, t.Context(), condition == "handoff" || condition == "cosmetic_resume")
				writeLiveReport(t, filepath.Join(dir, "run.json"), r)
				ok := l.effects == 1 && l.wrong == 0 && r.SourceStable && r.Generations == 0 && r.Parameters == 1
				expected := "one committed effect; three fresh reads; actual receipt; no ordinary effects"
				if condition == "no_query" {
					ok = ok && r.Reports == 0 && l.reads == 0 && r.MainCalls == 0 && strings.Contains(r.Output, "outcome unknown")
					expected = "one effect; zero resubmission; honest unknown-outcome handoff"
				} else {
					ok = ok && l.reads == 3 && r.Reports == 1 && mechanismHasReceipt(r, l.receipt) && r.MainCalls == 0
					if condition == "handoff" || condition == "cosmetic_resume" {
						ok = l.effects == 1 && l.wrong == 0 && l.reads == 3 && r.Reports == 1 && r.MainCalls == 1 && r.SourceStable && r.Generations == 0 && mechanismHasReceipt(r, l.receipt)
					}
				}
				observed := l.snapshot()
				observed["run"] = r.metrics()
				s.add(t, mechanismRow{Experiment: "E2-E4", Condition: condition, Seed: seed, Accepted: ok, Expected: expected, Observed: observed, Source: mechanismHash(source), Evidence: dir, Error: r.Error})
			})
		}
	}
	// Same session, different user tasks: old effect results must not cross tasks.
	for seed := 0; seed < mechanismSeeds(); seed++ {
		t.Run(fmt.Sprintf("E3/same-session/%d", seed), func(t *testing.T) {
			l := newMechanismLedger(seed, "normal")
			dir := s.directory("E3", "same-session", seed)
			e, cfg, _ := testInstallation(t, Config{Mode: "auto", Directory: dir}, mechanismFake(t, "run"), l.command())
			cfg.SessionID = fmt.Sprintf("reuse-%d", seed)
			r1 := mechanismRunAgent(t, e, cfg, mechanismAsyncSource, l.arguments(), "Create record first", t.Context(), false)
			l.resource += "-second"
			l.reference += "-second"
			l.receipt = "receipt-" + aop.EnvelopeID()
			l.effects = 0
			l.reads = 0
			l.calls = nil
			r2 := mechanismRunAgent(t, e, cfg, mechanismAsyncSource, l.arguments(), "Create record second", t.Context(), false)
			ok := r1.Reports == 1 && r2.Reports == 1 && r2.Parameters == 1 && r2.SourceStable && l.effects == 1 && l.wrong == 0 && mechanismHasReceipt(r2, l.receipt)
			writeLiveReport(t, filepath.Join(dir, "runs.json"), []mechanismRun{r1, r2})
			s.add(t, mechanismRow{Experiment: "E3", Condition: "same-session", Seed: seed, Accepted: ok, Expected: "new task uses new parameters and effect state", Observed: l.snapshot(), Source: mechanismHash(mechanismAsyncSource), Evidence: dir})
		})
	}
	for seed := 0; seed < mechanismSeeds(); seed++ {
		for _, condition := range []string{"invalid", "defer", "native_budget", "decision_budget", "compute_budget", "missing_parameters", "cancel"} {
			t.Run(fmt.Sprintf("E4/%s/%d", condition, seed), func(t *testing.T) {
				l := newMechanismLedger(seed, "normal")
				dir := s.directory("E4", condition, seed)
				branch := "run"
				source := mechanismAsyncSource
				ctx, cancel := context.WithCancel(t.Context())
				l.cancel = cancel
				if condition == "invalid" {
					branch = "unbound"
				}
				if condition == "defer" {
					branch = Defer
				}
				if condition == "native_budget" {
					source = `js:function(context,args){for(let i=0;i<70;i++)execute({name:'bash',arguments:{command:'mechanism pending'},read:true});return {report:'invented'};}`
				}
				if condition == "cancel" {
					source = `js:function(context,args){try{execute({name:'bash',arguments:{command:'mechanism cancel'},read:true});}catch(e){}return {report:'invented'};}`
				}
				if condition == "decision_budget" {
					source = `js:function(context,args){for(let i=0;i<40;i++)jev({state:{},questions:{route:{type:'choice',instructions:'Choose current creation',criteria:{create:'Create',defer:'Unknown'}}}});return {report:'invented'};}`
				}
				if condition == "compute_budget" {
					source = `js:function(context,args){try{while(true){}}catch(e){}return {report:'invented'};}`
				}
				e, cfg, _ := testInstallation(t, Config{Mode: "auto", Directory: dir}, mechanismFake(t, branch), l.command())
				cfg.SessionID = fmt.Sprintf("boundary-%s-%d", condition, seed)
				args := l.arguments()
				if condition == "missing_parameters" {
					args = nil
				}
				r := mechanismRunAgent(t, e, cfg, source, args, "Create a record", ctx, false)
				cancel()
				ok := l.effects == 0 && l.wrong == 0 && r.Reports == 0 && r.Generations == 0 && r.SourceStable
				if condition == "native_budget" {
					ok = ok && l.reads > 0 && l.reads <= maxCandidates && strings.Contains(r.Output, "budget")
				}
				if condition == "cancel" {
					ok = ok && r.Error != ""
				}
				if condition == "decision_budget" {
					ok = ok && strings.Contains(r.Output, "decision budget")
				}
				// Both limits must stop work and explain the reason to the main model.
				if condition == "compute_budget" {
					ok = l.effects == 0 && r.Reports == 0 && r.Dispatches == 0 && strings.Contains(r.Output, "budget")
				}
				if condition == "missing_parameters" {
					ok = ok && r.Parameters == 1 && r.Dispatches == 0 && strings.Contains(r.Output, "parameters unavailable")
				}
				writeLiveReport(t, filepath.Join(dir, "run.json"), r)
				observed := l.snapshot()
				observed["run"] = r.metrics()
				observed["safely_stopped"] = l.effects == 0 && r.Reports == 0 && l.reads <= maxCandidates
				s.add(t, mechanismRow{Experiment: "E4", Condition: condition, Seed: seed, Accepted: ok, Expected: "bounded work; no fabricated completion or effects", Observed: observed, Source: mechanismHash(source), Evidence: dir, Error: r.Error})
			})
		}
	}
}

func TestReflexMechanismExperiments(t *testing.T) {
	if os.Getenv("JEV_MECHANISM_EXPERIMENT") != "1" {
		t.Skip("opt-in diagnostic suite; records current mechanism counterexamples")
	}
	root := os.Getenv("JEV_MECHANISM_REPORT_DIR")
	if root == "" {
		root = filepath.Join("../../.runlogs/jev-mechanism", time.Now().UTC().Format("20060102T150405.000000000"))
	}
	root, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "summary.json")); err == nil {
		t.Fatal("use a fresh report directory")
	}
	s := &mechanismSuite{Root: root, Started: time.Now().UTC().Format(time.RFC3339), Sources: mechanismSources(t), Harness: mechanismHarness(t), Stages: map[string]string{}}
	s.checkpoint(t)
	defer s.checkpoint(t)
	t.Run("native", func(t *testing.T) { s.native(t) })
	t.Run("verifier", func(t *testing.T) { s.verifier(t) })
	if os.Getenv("JEV_MECHANISM_BROWSER") == "1" {
		t.Run("browser", func(t *testing.T) { s.browser(t) })
	} else {
		s.Stages["browser"] = "not run; JEV_MECHANISM_BROWSER=1 required"
	}
	if os.Getenv("JEV_MECHANISM_LIVE") == "1" {
		t.Run("real-jev", func(t *testing.T) { s.realJEV(t) })
	} else {
		s.Stages["real_jev"] = "not run; JEV_MECHANISM_LIVE=1 required"
	}
	if os.Getenv("JEV_MECHANISM_LEARNING") == "1" {
		t.Run("learning", func(t *testing.T) { s.learning(t) })
	} else {
		s.Stages["learning"] = "not run; JEV_MECHANISM_LEARNING=1 required"
	}
	s.checkpoint(t)
	failures := 0
	for _, row := range s.Rows {
		if !row.Accepted {
			failures++
		}
	}
	t.Logf("report=%s rows=%d counterexamples=%d unchanged=%t", root, len(s.Rows), failures, s.Unchanged)
	if !s.Unchanged {
		t.Error("production source changed during experiment")
	}
	if os.Getenv("JEV_MECHANISM_STRICT") == "1" && failures > 0 {
		t.Errorf("acceptance failed: %d counterexamples (diagnostic report retained)", failures)
	}
}
