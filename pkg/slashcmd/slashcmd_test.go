package slashcmd

import "testing"

func TestLookupScope(t *testing.T) {
	cases := []struct {
		verb      string
		wantOK    bool
		wantScope Scope
	}{
		{"scan", true, ScopeHub},
		{"agents", true, ScopeHub},
		{"help", true, ScopeHub},
		{"status", true, ScopeAgent},
		{"provider", true, ScopeAgent},
		{"spaces", true, ScopeAgent},
		{"stop", true, ScopeAgent},
		{"eval", true, ScopeAgent},
		{"goal", true, ScopeAgent}, // alias of /eval
		{"quit", true, ScopeAgent}, // alias of /exit
		{"/scan", true, ScopeHub},  // leading slash tolerated
		{"SCAN", true, ScopeHub},   // case-insensitive
		{"unknownxyz", false, 0},   // unknown → falls through to chat
		{"etc/passwd", false, 0},   // path-like → not a command
		{"", false, 0},
	}
	for _, tc := range cases {
		spec, ok := Lookup(tc.verb)
		if ok != tc.wantOK {
			t.Errorf("Lookup(%q) ok = %v, want %v", tc.verb, ok, tc.wantOK)
			continue
		}
		if ok && spec.Scope != tc.wantScope {
			t.Errorf("Lookup(%q) scope = %v, want %v", tc.verb, spec.Scope, tc.wantScope)
		}
	}
}

func TestRunControlNotInWebMenu(t *testing.T) {
	// Run-control commands are REPL-only: the web expresses them via the Pause
	// button and the Goal toggle, so they must never appear in a web menu.
	runControl := map[string]bool{"/stop": true, "/followup": true, "/eval": true, "/loop": true, "/exit": true}
	for _, s := range append(HubWebMenu(), AgentWebMenu()...) {
		if runControl[s.Name] {
			t.Errorf("run-control command %q leaked into the web menu", s.Name)
		}
		if !s.WebMenu {
			t.Errorf("menu builder returned non-WebMenu command %q", s.Name)
		}
	}
}

func TestWebMenuScopes(t *testing.T) {
	for _, s := range HubWebMenu() {
		if s.Scope != ScopeHub {
			t.Errorf("HubWebMenu returned non-hub command %q", s.Name)
		}
	}
	for _, s := range AgentWebMenu() {
		if s.Scope != ScopeAgent {
			t.Errorf("AgentWebMenu returned non-agent command %q", s.Name)
		}
	}
	if got := len(HubWebMenu()); got != 3 { // scan, agents, help
		t.Errorf("HubWebMenu size = %d, want 3", got)
	}
}
