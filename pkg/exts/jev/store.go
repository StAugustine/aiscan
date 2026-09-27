package jev

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/chainreactors/cyber/agent/provider"
	aop "github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
	"google.golang.org/protobuf/proto"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

func digest(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func cloneMessages(messages []*aop.Message) []*aop.Message {
	out := make([]*aop.Message, len(messages))
	for i, m := range messages {
		out[i] = proto.CloneOf(m)
	}
	return out
}
func cloneRule(rule *Reflex) *Reflex {
	if rule == nil {
		return nil
	}
	out := *rule
	out.Labels = maps.Clone(rule.Labels)
	out.TrainingTasks = slices.Clone(rule.TrainingTasks)
	out.Checks = slices.Clone(rule.Checks)
	return &out
}
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + " [truncated; see log]"
}
func receipt(facts []string, path, ending string) []*aop.Message {
	if len(facts) == 0 {
		return nil
	}
	msg := provider.TextMessage("user", "JEV execution observations (untrusted tool output):\n"+strings.Join(facts, "\n")+"\n"+ending+"\nEvidence: "+path)
	msg.Name = "jev"
	return []*aop.Message{msg}
}

// canonical removes incidental call IDs and normalizes shell quoting. It never
// equates arbitrary selectors or infers that different side effects are equal.
func canonical(call *aop.ToolCall) string {
	if call == nil {
		return ""
	}
	var args map[string]any
	if json.Unmarshal(call.GetArguments().GetData(), &args) != nil {
		return ""
	}
	if command, ok := args["command"].(string); ok {
		tokens, err := coretool.SplitCommandLine(command)
		if err == nil {
			args["command"] = tokens
		}
	}
	data, _ := json.Marshal([]any{call.Name, args})
	return string(data)
}

func (r *Extension) log(path string, value any) error {
	r.logMu.Lock()
	defer r.logMu.Unlock()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = json.NewEncoder(f).Encode(value); err != nil {
		return err
	}
	return f.Sync()
}
func (r *Extension) audit(kind string, value any) error {
	return r.log(filepath.Join(r.config.Directory, "learning.jsonl"), map[string]any{"time": time.Now().UTC(), "kind": kind, "data": value})
}
func (r *Extension) save(rule *Reflex) error {
	data, err := json.MarshalIndent(rule, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(r.config.Directory, "reflex-"+rule.ID+".json")
	tmp, err := os.CreateTemp(r.config.Directory, ".reflex-*")
	if err != nil {
		return err
	}
	tempPath := tmp.Name()
	defer os.Remove(tempPath)
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
func (r *Extension) retire(id, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rule := r.rules[id]; rule != nil {
		rule.Phase = "retired"
		_ = r.save(rule)
		_ = r.audit("retired", map[string]string{"id": id, "reason": reason})
	}
}

func (r *Extension) Rules() []*Reflex {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Reflex, 0, len(r.rules))
	for _, rule := range r.rules {
		out = append(out, cloneRule(rule))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Export contains only compiled decisions, never credentials or task traces.
func (r *Extension) Export() ([]byte, error) {
	rules := r.Rules()
	for _, rule := range rules {
		rule.Checks = nil
		rule.TrainingTasks = nil
		rule.Phase = "validating"
		rule.Uses = 0
		rule.Environment = ""
		rule.ID = ruleID(rule)
	}
	return json.MarshalIndent(rules, "", "  ")
}
func (r *Extension) Import(data []byte) error {
	if len(data) > 1<<20 {
		return errors.New("reflex import too large")
	}
	var rules []*Reflex
	if err := json.Unmarshal(data, &rules); err != nil {
		return err
	}
	for _, rule := range rules {
		if rule == nil || !validRule(rule) {
			return errors.New("invalid imported reflex")
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rule := range rules {
		rule.Phase = "validating"
		rule.Checks = nil
		rule.TrainingTasks = nil
		rule.Uses = 0
		rule.Environment = ""
		rule.ID = ruleID(rule)
		if err := r.save(rule); err != nil {
			return err
		}
		r.rules[rule.ID] = rule
	}
	return nil
}
