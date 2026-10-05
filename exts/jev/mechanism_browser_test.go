//go:build full

package jev

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent"
	coretool "github.com/chainreactors/cyber/core/tool"
	browserext "github.com/chainreactors/cyber/exts/browser"
	"mvdan.cc/sh/v3/syntax"
)

func coreResultText(r *coretool.Result) string { return coretool.ResultText(r) }

// This one source is reused without editing across all URLs, DOM IDs, form
// values and operation counts. Native browser operations remain trusted input.
const mechanismBrowserSource = `js:function(context,args){
 if(!args||!args.url||!args.session||!args.kind||!args.reference)return {defer:'missing current arguments',parameters:'url, session, kind, employee, amount, area, reference'};
 function run(op,values,read){let command='playwright '+op+' '+quote(args.session);for(const value of values)command+=' '+quote(String(value));const r=execute({name:'bash',arguments:{command:command},read:read});if(r.is_error)throw Error(r.text||'browser operation failed');return r;}
 const opened=execute({name:'bash',arguments:{command:'playwright open '+quote(args.url)+' --session '+quote(args.session)+' --no-speed-up --op-timeout 3'},read:false});
 if(opened.is_error)return {defer:'browser open failed'};
 try{
  if(args.kind==='expense'){
   run('fill',['label=Employee',args.employee],false);run('fill',['label=Amount',args.amount],false);run('fill',['label=Reference',args.reference],false);
   run('select-option',['label=Area',args.area],false);run('wait-for',['role=button[name="Submit expense"]'],true);run('click',['role=button[name="Submit expense"]'],false);
  }else if(args.kind==='shadow'){
   run('fill',['label=Customer reference',args.reference],false);run('click',['role=button[name="Save customer"]'],false);
  }else if(args.kind==='repeat'){
   run('click',['role=button[name="Add one"]'],false);run('click',['role=button[name="Add one"]'],false);
   const quantity=run('inner-text',['#quantity'],true).text;if(String(quantity).trim()!=='2')return {defer:'actual quantity is not two',quantity:quantity};
   run('click',['role=button[name="Checkout"]'],false);
  }else return {defer:'unsupported browser case'};
  run('wait-for',['--idle'],true);
  for(let i=0;i<12;i++){const r=run('inner-text',['output'],true);const m=/receipt-[a-zA-Z0-9-]+/.exec(r.text||'');if(m)return {report:{receipt:m[0],url:args.url,reference:args.reference}};}
  return {defer:'no final receipt observed'};
 }catch(e){return {defer:'browser binding failed: '+String(e)};}
}`

func mechanismQuote(t *testing.T, value string) string {
	t.Helper()
	q, err := syntax.Quote(value, syntax.LangBash)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func mechanismDirectBrowser(t *testing.T, cfg agent.Config, args map[string]any) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	run := func(op string, values ...string) (string, error) {
		command := "playwright " + op
		for _, v := range values {
			command += " " + mechanismQuote(t, v)
		}
		r, err := cfg.Tools.ExecuteTool(ctx, "bash", jsonText(map[string]any{"command": command}))
		if err != nil {
			return "", err
		}
		if r.IsError {
			return "", fmt.Errorf("%s", coreResultText(r))
		}
		return coreResultText(r), nil
	}
	session := args["session"].(string)
	if _, err := run("open", args["url"].(string), "--session", session, "--no-speed-up", "--op-timeout", "3"); err != nil {
		return "", err
	}
	steps := [][]string{}
	switch args["kind"] {
	case "expense":
		steps = [][]string{{"fill", "label=Employee", args["employee"].(string)}, {"fill", "label=Amount", args["amount"].(string)}, {"fill", "label=Reference", args["reference"].(string)}, {"select-option", "label=Area", args["area"].(string)}, {"wait-for", `role=button[name="Submit expense"]`}, {"click", `role=button[name="Submit expense"]`}}
	case "shadow":
		steps = [][]string{{"fill", "label=Customer reference", args["reference"].(string)}, {"click", `role=button[name="Save customer"]`}}
	case "repeat":
		steps = [][]string{{"click", `role=button[name="Add one"]`}, {"click", `role=button[name="Add one"]`}, {"click", `role=button[name="Checkout"]`}}
	}
	for _, step := range steps {
		if _, err := run(step[0], append([]string{session}, step[1:]...)...); err != nil {
			return "", err
		}
	}
	if _, err := run("wait-for", session, "--idle"); err != nil {
		return "", err
	}
	for i := 0; i < 12; i++ {
		text, err := run("inner-text", session, "output")
		if err != nil {
			return "", err
		}
		if strings.Contains(text, "receipt-") {
			return text, nil
		}
	}
	return "", fmt.Errorf("no actual receipt")
}

func (s *mechanismSuite) browser(t *testing.T) {
	lab := startBrowserTakeoverLab(t)
	for _, kind := range []string{"expense", "shadow", "repeat"} {
		livePassed := true
		handwrittenPassed := true
		conditions := []string{"direct", "handwritten"}
		if os.Getenv("JEV_MECHANISM_LIVE") == "1" {
			conditions = append(conditions, "real")
		}
		for seed := 0; seed < mechanismSeeds(); seed++ {
			for _, condition := range conditions {
				if condition == "real" && (!handwrittenPassed || seed >= 5 && !livePassed) {
					s.Stages["E5-"+kind+"-real"] = "remaining trials skipped after a handwritten control failure or a real trial failure at/after the five-trial gate"
					continue
				}
				t.Run(fmt.Sprintf("%s/%s/%d", kind, condition, seed), func(t *testing.T) {
					dir := s.directory("E5-"+kind, condition, seed)
					browser, err := browserext.New(dir, "")
					if err != nil {
						t.Fatal(err)
					}
					client := mechanismFake(t, "run")
					if condition == "real" {
						client = s.liveClient(t, dir, true)
					}
					e, cfg, _ := testInstallationWithExtensions(t, Config{Mode: "auto", Directory: dir}, client, browser)
					var args map[string]any
					lab.control(t, "new", map[string]any{"kind": kind, "index": seed, "artifact_dir": filepath.Join(dir, "artifacts")}, &args)
					args["session"] = fmt.Sprintf("mechanism-%s-%d", kind, seed)
					cfg.SessionID = fmt.Sprintf("E5-%s-%s-%d", kind, condition, seed)
					output := ""
					var runErr error
					var r mechanismRun
					if condition == "direct" {
						output, runErr = mechanismDirectBrowser(t, cfg, args)
					} else {
						ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
						r = mechanismRunAgent(t, e, cfg, mechanismBrowserSource, args, "Use browser UI at "+args["url"].(string)+" . "+args["prompt"].(string), ctx, false)
						cancel()
						output = jsonText(r.ReportValues)
						writeLiveReport(t, filepath.Join(dir, "run.json"), r)
					}
					var oracle map[string]any
					lab.control(t, "check", map[string]any{"id": args["id"], "output": output}, &oracle)
					ok := oracle["correct"] == true && runErr == nil
					if condition != "direct" {
						ok = ok && r.Error == "" && r.Reports == 1 && r.Takeovers > 0 && r.MainCalls == 0 && r.SourceStable && r.Generations == 0
					}
					oracle["run"] = r.metrics()
					s.add(t, mechanismRow{Experiment: "E5-" + kind, Condition: condition, Seed: seed, Accepted: ok, Expected: "trusted current business effects, no duplicates/wrong targets, actual receipt, same source", Observed: oracle, Source: mechanismHash(mechanismBrowserSource), Evidence: dir, Error: errorText(runErr)})
					if condition == "handwritten" && !ok {
						handwrittenPassed = false
					}
					if condition == "real" && !ok {
						livePassed = false
					}
				})
			}
		}
	}
	s.Stages["browser_capability_gaps"] = "Previous independent official/native comparison retained in .runlogs/jev-playwright-20261004/comparison-final.json and template-drag.json; popup/frame/download are separate backend gaps, template drag succeeds. Not rerun as a model test."
}

func (s *mechanismSuite) realJEV(t *testing.T) {
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		t.Fatal("TYPESAFE_API_KEY required for explicit live run")
	}
	for _, condition := range []string{"create", "inspect", "cancel", "unrelated"} {
		passed := true
		for seed := 0; seed < mechanismSeeds(); seed++ {
			if seed == 5 && !passed {
				s.Stages["E6-"+condition] = "stopped after initial five: deterministic control or real selection failed"
				break
			}
			for _, mode := range []string{"fixed", "real"} {
				t.Run(fmt.Sprintf("%s/%s/%d", condition, mode, seed), func(t *testing.T) {
					l := newMechanismLedger(seed, "business_503")
					user := []string{"Create an asynchronous record", "请创建一条记录并等待实际完成", "Create the record; report only its final receipt"}[seed%3]
					if condition == "inspect" {
						user = "Inspect the existing record without creating another"
						l.effects = 1
					}
					if condition == "cancel" {
						user = "Cancel this operation; do not create any record"
					}
					if condition == "unrelated" {
						user = "Write a poem about the moon; do not operate resources"
					}
					dir := s.directory("E6-"+condition, mode, seed)
					branch := condition
					if condition == "unrelated" {
						branch = Defer
					}
					client := mechanismFake(t, branch)
					if mode == "real" {
						client = s.liveClient(t, dir, true)
					}
					e, cfg, _ := testInstallation(t, Config{Mode: "auto", Directory: dir}, client, l.command())
					cfg.SessionID = fmt.Sprintf("E6-%s-%s-%d", condition, mode, seed)
					r := mechanismRunAgent(t, e, cfg, mechanismSemanticSource, l.arguments(), user+" Resource "+l.resource+" reference "+l.reference, t.Context(), false)
					ok := r.SourceStable && r.Generations == 0 && r.MainCalls == 0 && l.wrong == 0 && r.Error == ""
					if condition == "cancel" {
						ok = ok && l.effects == 0 && r.Reports == 1 && strings.Contains(r.Output, `"cancelled":true`)
					} else if condition == "unrelated" {
						ok = ok && l.effects == 0 && r.Reports == 0
					} else {
						ok = ok && l.effects == 1 && l.reads == 3 && r.Reports == 1 && mechanismHasReceipt(r, l.receipt)
					}
					observed := l.snapshot()
					observed["run"] = r.metrics()
					writeLiveReport(t, filepath.Join(dir, "run.json"), r)
					s.add(t, mechanismRow{Experiment: "E6-" + condition, Condition: mode, Seed: seed, Accepted: ok, Expected: "same source; correct entry/branch; actual effects or no-op/defer", Observed: observed, Source: mechanismHash(mechanismSemanticSource), Evidence: dir, Error: r.Error})
					if !ok {
						passed = false
					}
				})
			}
		}
	}
}
