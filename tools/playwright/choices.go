//go:build full

package playwright

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/chainreactors/cyber/agent/provider"
	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
	"github.com/chainreactors/cyber/tools/toolargs"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"mvdan.cc/sh/v3/syntax"
)

const ChoiceContract = "playwright-live-v2"

func (c *Command) execScroll(ctx context.Context, args []string) (string, error) {
	if len(args) != 2 || (args[1] != "up" && args[1] != "down") {
		return "", fmt.Errorf("usage: playwright scroll <session> <up|down>")
	}
	sess, err := c.getSession(args[0])
	if err != nil {
		return "", err
	}
	return sess.withPage(ctx, func(page *rod.Page) (string, error) {
		direction := 1
		if args[1] == "up" {
			direction = -1
		}
		_, err := page.Eval(`direction=>window.scrollBy(0,direction*innerHeight*0.75)`, direction)
		return "Scrolled " + args[1], err
	})
}

//go:embed choices.js
var choiceSnapshot string

type observedChoice struct {
	args     []string
	node     int
	state    [32]byte
	document string
}
type observedPage struct {
	Document string `json:"document"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Text     string `json:"text"`
	Elements []struct {
		Node      int     `json:"node"`
		Selector  string  `json:"selector"`
		Label     string  `json:"label"`
		Tag       string  `json:"tag"`
		Type      string  `json:"type"`
		Role      string  `json:"role"`
		Value     string  `json:"value"`
		Sensitive bool    `json:"sensitive"`
		Checked   bool    `json:"checked"`
		Expanded  *string `json:"expanded"`
		Readonly  bool    `json:"readonly"`
		Options   []struct {
			Value    string `json:"value"`
			Label    string `json:"label"`
			Selected bool   `json:"selected"`
		} `json:"options"`
	} `json:"elements"`
	Scroll  struct{ X, Y, Height, Viewport, Width float64 } `json:"scroll"`
	Loading bool                                            `json:"loading"`
}

// Choices covers browser entry and current page operations. Observation never
// opens a browser or performs input; selected calls use the ordinary Executor.
func (c *Command) Choices(ctx context.Context, messages []*aop.Message) (json.RawMessage, map[string]*aop.Content, error) {
	owner := operation.InvocationFromContext(ctx).SessionID
	if owner == "" {
		return nil, nil, nil
	}
	c.sessionsMu.Lock()
	var sessions []*Session
	for _, s := range c.sessions {
		if s.owner == owner {
			sessions = append(sessions, s)
		}
	}
	c.sessionsMu.Unlock()
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Name < sessions[j].Name })
	choices := map[string]*aop.Content{}
	states := map[string]json.RawMessage{}
	if len(sessions) == 0 {
		urls := toolargs.TaskURLs(messages)
		for i, target := range urls {
			quoted, err := syntax.Quote(target, syntax.LangBash)
			if err != nil {
				continue
			}
			args, _ := json.Marshal(map[string]string{"command": "playwright open " + quoted})
			// Entry has no DOM handle to validate. Its immutable URL comes from
			// the user; normal tool admission still decides whether it may open.
			call := &aop.ToolCall{Id: aop.EnvelopeID(), Name: "bash", Arguments: &aop.EncodedValue{Data: args, MediaType: aop.JSONMediaType}, WorkingDirectory: c.workDir}
			choices[fmt.Sprintf("open%d", i)] = &aop.Content{Value: &aop.Content_ToolCall{ToolCall: call}}
		}
		if len(urls) > 0 {
			states["available_urls"], _ = json.Marshal(urls)
		}
	}
	for _, sess := range sessions {
		_, err := sess.withPage(ctx, func(page *rod.Page) (string, error) {
			value, err := page.Eval(choiceSnapshot)
			if err != nil {
				return "", err
			}
			var state observedPage
			if err = value.Value.Unmarshal(&state); err != nil {
				return "", err
			}
			raw, err := json.Marshal(state)
			if err != nil {
				return "", err
			}
			if len(raw) > 28<<10 || len(state.Elements) > 64 {
				return "", nil
			}
			states[sess.Name] = raw
			sess.choices = map[string]observedChoice{}
			add := func(node int, args ...string) {
				if len(choices) >= 64 {
					return
				}
				id := "choice-pw-" + aop.EnvelopeID()
				parts := []string{"playwright"}
				for _, arg := range args {
					quoted, err := syntax.Quote(arg, syntax.LangBash)
					if err != nil {
						return
					}
					parts = append(parts, quoted)
				}
				arguments, _ := json.Marshal(map[string]string{"command": strings.Join(parts, " ")})
				call := &aop.ToolCall{Id: id, Name: "bash", Arguments: &aop.EncodedValue{Data: arguments, MediaType: aop.JSONMediaType}, WorkingDirectory: c.workDir}
				choices[fmt.Sprintf("c%d", len(choices))] = &aop.Content{Value: &aop.Content_ToolCall{ToolCall: call}}
				sess.choices[id] = observedChoice{args: args, node: node, state: sha256.Sum256(raw), document: state.Document}
			}
			known := knownFieldValues(messages, sess.Name)
			for _, el := range state.Elements {
				switch {
				case el.Tag == "select":
					for _, option := range el.Options {
						if !option.Selected {
							add(el.Node, "select-option", sess.Name, el.Selector, option.Value)
						}
					}
				case (el.Tag == "input" && !slices.Contains([]string{"checkbox", "radio", "button", "submit", "reset", "file", "hidden"}, el.Type)) || el.Tag == "textarea":
					if !el.Sensitive && !el.Readonly {
						for _, value := range known[el.Selector] {
							if value != el.Value {
								add(el.Node, "fill", sess.Name, el.Selector, value)
							}
						}
					}
				case el.Tag == "input" && el.Type == "file":
				default:
					add(el.Node, "click", sess.Name, el.Selector)
				}
			}
			if state.Scroll.Y > 0 {
				add(0, "scroll", sess.Name, "up")
			}
			if state.Scroll.Y+state.Scroll.Viewport < state.Scroll.Height {
				add(0, "scroll", sess.Name, "down")
			}
			if state.Loading {
				add(0, "wait-for", sess.Name, "--stable")
			}
			return "", nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	// An owned session set can be empty before open or after close. Preserve
	// that observation so ordinary completion decisions remain defer evidence.
	data, err := json.Marshal(states)
	return data, choices, err
}

func knownFieldValues(messages []*aop.Message, session string) map[string][]string {
	out := map[string][]string{}
	success := map[string]bool{}
	for _, m := range messages {
		if r := provider.MessageToolResult(m); r != nil && !r.IsError {
			success[r.CallId] = true
		}
	}
	for _, m := range messages {
		for _, call := range provider.MessageToolCalls(m) {
			if !success[call.Id] || call.Name != "bash" {
				continue
			}
			var args struct {
				Command string `json:"command"`
			}
			if json.Unmarshal(call.GetArguments().GetData(), &args) != nil {
				continue
			}
			tokens, err := coretool.SplitCommandLine(args.Command)
			if err == nil && len(tokens) == 5 && tokens[0] == "playwright" && tokens[1] == "fill" && tokens[2] == session && len(tokens[4]) <= 2000 {
				out[tokens[3]] = append(out[tokens[3]], tokens[4])
			}
		}
	}
	return out
}

func (c *Command) runChoice(ctx context.Context, sub string, args []string) (string, error) {
	if len(args) < 1 {
		return "", coretool.ErrStaleChoice
	}
	sess, err := c.getSession(args[0])
	if err != nil {
		return "", coretool.ErrStaleChoice
	}
	inv := operation.InvocationFromContext(ctx)
	if sess.owner == "" || sess.owner != inv.SessionID {
		return "", coretool.ErrStaleChoice
	}
	return sess.withPage(ctx, func(page *rod.Page) (string, error) {
		choice, ok := sess.choices[inv.CallID]
		delete(sess.choices, inv.CallID)
		if !ok || !slices.Equal(choice.args, append([]string{sub}, args...)) {
			return "", coretool.ErrStaleChoice
		}
		value, err := page.Eval(choiceSnapshot)
		if err != nil {
			return "", coretool.ErrStaleChoice
		}
		var current observedPage
		if value.Value.Unmarshal(&current) != nil {
			return "", coretool.ErrStaleChoice
		}
		raw, _ := json.Marshal(current)
		if current.Document != choice.document || sha256.Sum256(raw) != choice.state {
			return "", coretool.ErrStaleChoice
		}
		if sub == "scroll" {
			delta := current.Scroll.Viewport * 0.75
			if args[1] == "up" {
				delta = -delta
			}
			_, err = page.Eval(`dy=>window.scrollBy(0,dy)`, delta)
		} else if sub == "wait-for" {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		} else {
			el, err := page.ElementByJS(rod.Eval(`id=>window.__cyberChoices?.nodes.get(id)`, choice.node))
			if err != nil {
				return "", coretool.ErrStaleChoice
			}
			guard, err := el.Eval(`() => {if(!this.isConnected||this.matches(':disabled')||this.closest('[inert],[aria-disabled=true]'))return false;const r=this.getBoundingClientRect();return this.contains(document.elementFromPoint(r.x+r.width/2,r.y+r.height/2));}`)
			if err != nil || !guard.Value.Bool() {
				return "", coretool.ErrStaleChoice
			}
			switch sub {
			case "click":
				err = el.Click(proto.InputMouseButtonLeft, 1)
			case "fill":
				if err = el.SelectAllText(); err == nil {
					err = el.Input(args[2])
				}
			case "select-option":
				err = el.Select([]string{fmt.Sprintf("option[value=%q]", args[2])}, true, rod.SelectorTypeCSSSector)
			default:
				return "", coretool.ErrStaleChoice
			}
			if err != nil {
				return "", err
			}
		}
		if err != nil {
			return "", err
		}
		// Capture effect before the next observation, even if navigation follows.
		label := ""
		for _, el := range current.Elements {
			if el.Node == choice.node {
				label = el.Label
				break
			}
		}
		return fmt.Sprintf("Executed %s in session %s against observed document %s\nObserved URL: %s\nTarget label: %q", sub, sess.Name, choice.document, current.URL, label), nil
	})
}
