//go:build full

package playwright

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
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

//go:embed observe.js
var pageObservation string

type observedAction struct {
	args     []string
	node     int
	state    [32]byte
	document string
}
type observedPage struct {
	Document string `json:"document"`
	Revision uint64 `json:"revision"`
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
		Disabled  bool    `json:"disabled"`
		Required  bool    `json:"required"`
		Invalid   bool    `json:"invalid"`
		Options   []struct {
			Value    string `json:"value"`
			Label    string `json:"label"`
			Selected bool   `json:"selected"`
			Disabled bool   `json:"disabled"`
		} `json:"options"`
	} `json:"elements"`
	Scroll  struct{ X, Y, Height, Viewport, Width float64 } `json:"scroll"`
	Loading bool                                            `json:"loading"`
}

// Observe covers browser entry and current page operations. Observation never
// opens a browser or performs input; selected calls use the ordinary Executor.
func (c *Command) Observe(ctx context.Context, messages []*aop.Message) (json.RawMessage, map[string]*aop.Content, error) {
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
			sess.pending = nil // Any new observation invalidates the prior candidate set.
			value, err := page.Eval(pageObservation)
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
				return "", fmt.Errorf("browser observation exceeds state budget")
			}
			states[sess.Name] = raw
			sess.pending = map[string]observedAction{}
			overflow := false
			add := func(node int, args ...string) {
				if len(choices) >= 64 {
					overflow = true
					return
				}
				id := aop.EnvelopeID()
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
				sess.pending[id] = observedAction{args: args, node: node, state: sha256.Sum256(raw), document: state.Document}
			}
			known := knownFieldValues(messages, sess.Name)
			for _, el := range state.Elements {
				if el.Disabled {
					continue
				}
				switch {
				case el.Tag == "select":
					for _, option := range el.Options {
						if !option.Selected && !option.Disabled {
							add(el.Node, "select-option", sess.Name, el.Selector, option.Value)
						}
					}
				case (el.Tag == "input" && !slices.Contains([]string{"checkbox", "radio", "button", "submit", "reset", "file", "hidden"}, el.Type)) || el.Tag == "textarea":
					if !el.Sensitive && !el.Readonly {
						for _, value := range append(known[el.Selector], explicitFieldValues(messages)...) {
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
			// Async application state can change after readyState is complete.
			// Only the consumer decides whether to execute an explicit wait.
			add(0, "wait-for", sess.Name, "--stable")
			add(0, "close", sess.Name)
			if overflow {
				return "", fmt.Errorf("browser observation exceeds candidate budget")
			}
			c.sessionsMu.Lock()
			defer c.sessionsMu.Unlock()
			// Never evict issued IDs: an evicted replay would look like a normal
			// call. At the lifetime bound, observation fails back to the caller.
			if len(c.observed)+len(sess.pending) > 65536 {
				return "", fmt.Errorf("browser observation call capacity reached")
			}
			if c.observed == nil {
				c.observed = make(map[string]struct{})
			}
			for id := range sess.pending {
				c.observed[id] = struct{}{}
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

func (c *Command) runObserved(ctx context.Context, sub string, args []string) (string, error) {
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
	result, err := sess.withPage(ctx, func(page *rod.Page) (string, error) {
		choice, ok := sess.pending[inv.CallID]
		delete(sess.pending, inv.CallID)
		if !ok || !slices.Equal(choice.args, append([]string{sub}, args...)) {
			return "", coretool.ErrStaleChoice
		}
		value, err := page.Eval(pageObservation)
		if err != nil {
			return "", coretool.ErrStaleChoice
		}
		var current observedPage
		if value.Value.Unmarshal(&current) != nil {
			return "", coretool.ErrStaleChoice
		}
		raw, _ := json.Marshal(current)
		// Waiting reads the same document while it changes. Requiring the old
		// DOM revision would invalidate precisely the transition being awaited.
		// Actions and close still require the exact observed state.
		if current.Document != choice.document || (sub != "wait-for" && sha256.Sum256(raw) != choice.state) {
			return "", coretool.ErrStaleChoice
		}
		if sub == "close" {
			// Remove this exact session while its observed page is locked. A
			// concurrent close/reopen must never redirect cleanup to a new page.
			c.sessionsMu.Lock()
			defer c.sessionsMu.Unlock()
			if c.sessions[sess.Name] != sess {
				return "", coretool.ErrStaleChoice
			}
			delete(c.sessions, sess.Name)
			return fmt.Sprintf("Last observed page before closing:\nURL: %s\nTitle: %s\n%s", current.URL, current.Title, current.Text), nil
		}
		if sub == "scroll" {
			delta := current.Scroll.Viewport * 0.75
			if args[1] == "up" {
				delta = -delta
			}
			_, err = page.Eval(`dy=>window.scrollBy(0,dy)`, delta)
		} else if sub == "wait-for" {
			err = page.WaitStable(500 * time.Millisecond)
		} else {
			el, err := page.ElementByJS(rod.Eval(`id=>window.__cyberObservation?.nodes.get(id)`, choice.node))
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
	if err == nil && sub == "close" {
		closed, closeErr := c.closeSession(sess, "", "")
		return result + "\n" + closed, closeErr
	}
	return result, err
}

// Only literal values explicitly quoted by the current user become additional
// fill candidates. Binding to a field is a consumer decision; remote text cannot
// supply values and unknown/unquoted prose remains ordinary model work.
var quotedFieldValue = regexp.MustCompile("\"([^\"\\r\\n]{1,1000})\"|`([^`\\r\\n]{1,1000})`|“([^”\\r\\n]{1,1000})”")

func explicitFieldValues(messages []*aop.Message) []string {
	var values []string
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m == nil || m.Role != "user" || m.Name != "" {
			continue
		}
		for _, match := range quotedFieldValue.FindAllStringSubmatch(provider.MessageText(m), 8) {
			for _, value := range match[1:] {
				if value != "" && !slices.Contains(values, value) {
					values = append(values, value)
				}
			}
		}
		break
	}
	return values
}
