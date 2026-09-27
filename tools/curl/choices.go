package curl

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"

	"github.com/chainreactors/cyber/agent/provider"
	aop "github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
	"github.com/chainreactors/cyber/tools/toolargs"
	"mvdan.cc/sh/v3/syntax"
)

// Choices exposes read candidates on URLs supplied by the task or already
// chosen by the ordinary model. Remote page text cannot add new targets here.
func (c *Command) Choices(_ context.Context, messages []*aop.Message) (json.RawMessage, map[string]*aop.Content, error) {
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
	var observations []string
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
		if result := provider.MessageToolResult(m); result != nil {
			text := coretool.ResultText(result)
			if len(text) > 2048 {
				text = text[:2048] + " [partial; consult original evidence]"
			}
			observations = append(observations, text)
		}
		if m.Name == "jev" || m.Name == "jev-step" {
			text := provider.MessageText(m)
			if len(text) > 4096 {
				text = text[:4096] + " [partial; consult evidence log]"
			}
			observations = append(observations, text)
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
	for _, target := range urls {
		for _, flag := range []string{"", "-I", "-i"} {
			quoted, err := syntax.Quote(target, syntax.LangBash)
			if err != nil {
				continue
			}
			command := "curl "
			if flag != "" {
				command += flag + " "
			}
			args, _ := json.Marshal(map[string]string{"command": command + quoted})
			id := "choice-curl-" + aop.EnvelopeID()
			choices[id] = &aop.Content{Value: &aop.Content_ToolCall{ToolCall: &aop.ToolCall{Id: id, Name: "bash", Arguments: &aop.EncodedValue{Data: args, MediaType: aop.JSONMediaType}}}}
		}
	}
	if len(observations) > 4 {
		observations = observations[len(observations)-4:]
	}
	state, err := json.Marshal(map[string]any{"known_urls": urls, "recent_observations": observations, "note": "HTTP success or a scanner match is not proof of a vulnerability. Missing independent evidence requires ordinary model review."})
	return state, choices, err
}
