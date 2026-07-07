// Package slashcmd is the single source of truth for aiscan's user-facing slash
// commands. It is a leaf package — types plus a static catalog, with no imports
// of pkg/tui, pkg/web, pkg/webagent, or pkg/agent — so every surface describes
// the same command set without drifting:
//
//   - the interactive REPL (pkg/tui) keeps its command set in parity with Core()
//     (guarded by a test), so /help and completion never diverge from the web;
//   - the web hub (pkg/web) routes a typed slash command by its Scope instead of
//     a hardcoded switch, and renders /help + the "/" menu from the catalog;
//   - the web frontend populates its "/" menu from the catalog the agent + hub
//     report, so it always reflects what actually works.
//
// This is deliberately distinct from pkg/commands, which is the LLM *tool*
// registry (the structured tools/pseudo-commands the model calls). slashcmd is
// only about the "/verb" commands a human types.
package slashcmd

import "strings"

// Scope declares where a command executes when typed in the web chat. It is only
// meaningful for web dispatch; the standalone REPL runs every command it has a
// handler for, regardless of Scope.
type Scope uint8

const (
	// ScopeAgent runs against the live agent — model, tools, skills, IOA, and
	// the conversation. In the web this is forwarded to the agent node over the
	// existing AgentConsole bridge (pkg/webagent.runChatREPLLine).
	ScopeAgent Scope = iota
	// ScopeHub runs on the web hub — the multi-agent roster, the scan pipeline,
	// and the session store. It has no counterpart in the standalone REPL.
	ScopeHub
)

// Spec is the surface-neutral description of one slash command.
type Spec struct {
	Name        string   `json:"name"`              // "/scan"
	Aliases     []string `json:"aliases,omitempty"` // ["/goal"]
	Usage       string   `json:"usage,omitempty"`   // "/scan <target> [--mode full]"
	Description string   `json:"description,omitempty"`
	Scope       Scope    `json:"scope"`
	// WebMenu advertises the command in the web "/" menu. Run-control commands
	// (/stop, /followup, /eval, /loop, /exit) are false: the web expresses those
	// through first-class UI (the Pause button, the Goal toggle), not slash text,
	// and the per-line web bridge cannot honor "act on the running task" anyway.
	WebMenu bool `json:"web_menu,omitempty"`
}

// core is the canonical catalog. Agent-scope entries come first, in REPL /help
// order; hub-scope entries follow. Descriptions are kept verbatim from the
// former pkg/tui definitions so REPL output is unchanged.
var core = []Spec{
	{Name: "/help", Scope: ScopeHub, WebMenu: true, Description: "查看命令面板"},
	{Name: "/status", Scope: ScopeAgent, WebMenu: true, Description: "查看模型、渲染模式、IOA 和 skills"},
	{Name: "/clear", Scope: ScopeAgent, WebMenu: true, Description: "清空当前会话上下文"},
	{Name: "/stop", Scope: ScopeAgent, WebMenu: false, Description: "停止当前正在运行的任务"},
	{Name: "/followup", Scope: ScopeAgent, WebMenu: false, Usage: "/followup <text>", Description: "排队到当前任务结束后再发送"},
	{Name: "/eval", Aliases: []string{"/goal"}, Scope: ScopeAgent, WebMenu: false, Usage: "/eval <criteria> | /eval off", Description: "设置/查看/关闭 goal evaluation (/eval off 关闭)"},
	{Name: "/loop", Scope: ScopeAgent, WebMenu: false, Usage: "/loop 30s <prompt> | /loop list | /loop stop <name>", Description: "定时循环任务 (/loop 30s <prompt> | /loop list | /loop stop <name>)"},
	{Name: "/exit", Aliases: []string{"/quit"}, Scope: ScopeAgent, WebMenu: false, Description: "退出交互模式"},
	{Name: "/provider", Scope: ScopeAgent, WebMenu: true, Usage: "/provider [list | set <name>]", Description: "查看/管理 LLM provider 链"},
	{Name: "/spaces", Scope: ScopeAgent, WebMenu: true, Description: "List all IOA spaces"},
	{Name: "/messages", Scope: ScopeAgent, WebMenu: true, Usage: "/messages <space>", Description: "List start messages in a space"},
	{Name: "/context", Scope: ScopeAgent, WebMenu: true, Usage: "/context <space> <message-id>", Description: "View message thread/context"},
	{Name: "/nodes", Scope: ScopeAgent, WebMenu: true, Usage: "/nodes [space]", Description: "List nodes (optionally scoped to a space)"},

	{Name: "/scan", Scope: ScopeHub, WebMenu: true, Usage: "/scan <target> [--mode full] [--verify] [--sniper] [--deep]", Description: "在本会话运行扫描"},
	{Name: "/agents", Scope: ScopeHub, WebMenu: true, Description: "列出已连接的 agent"},
}

// Core returns a copy of the full static catalog in canonical order.
func Core() []Spec { return append([]Spec(nil), core...) }

// Lookup resolves a bare verb (leading "/" optional) to its Spec, matching Name
// or any alias case-insensitively. ok is false for an unknown verb — which the
// web dispatch treats identically to an agent-scope command (both fall through
// to the agent), so skill commands need not be classified here.
func Lookup(verb string) (Spec, bool) {
	verb = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(verb), "/"))
	if verb == "" {
		return Spec{}, false
	}
	for _, s := range core {
		if specMatches(s, verb) {
			return s, true
		}
	}
	return Spec{}, false
}

func specMatches(s Spec, verb string) bool {
	if strings.ToLower(strings.TrimPrefix(s.Name, "/")) == verb {
		return true
	}
	for _, a := range s.Aliases {
		if strings.ToLower(strings.TrimPrefix(a, "/")) == verb {
			return true
		}
	}
	return false
}

// HubWebMenu returns the hub-scope, menu-visible static specs (/scan, /agents,
// /help). The hub prepends these to a session agent's reported commands.
func HubWebMenu() []Spec { return webMenuByScope(ScopeHub) }

// AgentWebMenu returns the agent-scope, menu-visible static specs (/status,
// /provider, the IOA commands, ...). An agent reports these — plus one SkillSpec
// per loaded skill — to the hub so the web menu shows what that agent offers.
func AgentWebMenu() []Spec { return webMenuByScope(ScopeAgent) }

func webMenuByScope(scope Scope) []Spec {
	var out []Spec
	for _, s := range core {
		if s.WebMenu && s.Scope == scope {
			out = append(out, s)
		}
	}
	return out
}

// SkillSpec builds a runtime menu spec for a loaded skill command.
func SkillSpec(name, description string) Spec {
	return Spec{
		Name:        "/" + strings.TrimPrefix(strings.TrimSpace(name), "/"),
		Description: description,
		Scope:       ScopeAgent,
		WebMenu:     true,
	}
}
