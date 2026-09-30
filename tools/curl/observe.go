package curl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/chainreactors/cyber/agent/provider"
	aop "github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
	"github.com/chainreactors/cyber/tools/toolargs"
	"mvdan.cc/sh/v3/syntax"
)

// Observe exposes read candidates on URLs supplied by the task or already
// chosen by the ordinary model. Remote page text cannot add new targets here.
// Results remain in native history; copying them into physical state duplicates
// context and makes unchanged endpoints appear to change after every decision.
func (c *Command) Observe(_ context.Context, messages []*aop.Message) (json.RawMessage, map[string]*aop.Content, error) {
	targets := map[string]bool{}
	for _, raw := range toolargs.TaskURLs(messages) {
		targets[raw] = true
	}
	add := func(raw string) {
		u, err := url.Parse(raw)
		if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && len(raw) <= 2048 && len(targets) < 8 {
			targets[raw] = true
		}
	}
	start := 0
	for i, m := range messages {
		if m.Role == "user" && m.Name == "" {
			start = i
		}
	}
	for _, m := range messages[start:] {
		for _, call := range provider.MessageToolCalls(m) {
			if call.Name != "bash" {
				continue
			}
			var args struct {
				Command string `json:"command"`
			}
			if json.Unmarshal(call.GetArguments().GetData(), &args) != nil {
				continue
			}
			tokens, err := coretool.SplitCommandLine(args.Command)
			if err != nil || len(tokens) < 2 || tokens[0] != "curl" {
				continue
			}
			r, err := Parse(tokens[1:])
			if err == nil {
				add(r.URL)
			}
		}

	}
	if len(targets) == 0 {
		return nil, nil, nil
	}
	urls := make([]string, 0, len(targets))
	for target := range targets {
		urls = append(urls, target)
	}
	sort.Strings(urls)
	choices := map[string]*aop.Content{}
	addChoice := func(command string) {
		args, _ := json.Marshal(map[string]string{"command": command})
		call := &aop.ToolCall{Id: aop.EnvelopeID(), Name: "bash", Arguments: &aop.EncodedValue{Data: args, MediaType: aop.JSONMediaType}}
		choices[fmt.Sprintf("c%d", len(choices))] = &aop.Content{Value: &aop.Content_ToolCall{ToolCall: call}}
	}
	var batches [3][]string
	for _, target := range urls {
		for i, flag := range []string{"", "-I", "-i"} {
			quoted, err := syntax.Quote(target, syntax.LangBash)
			if err != nil {
				continue
			}
			command := "curl "
			if flag != "" {
				command += flag + " "
			}
			addChoice(command + quoted)
			batches[i] = append(batches[i], command+quoted)
		}
	}
	// A batch is another possible native shell call, not a prescribed plan.
	// The consumer decides whether these known reads are independent and needed.
	// Normal command admission and per-command results remain in effect.
	for _, batch := range batches {
		if len(batch) > 1 {
			addChoice(strings.Join(batch, "; "))
		}
	}
	state, err := json.Marshal(map[string]any{"known_urls": urls})
	return state, choices, err
}
