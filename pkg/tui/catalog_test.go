package tui

import (
	"reflect"
	"testing"

	"github.com/chainreactors/aiscan/pkg/slashcmd"
)

// TestREPLCommandsMatchCatalog guards the single-source-of-truth contract: the
// REPL's static command set (metadata) must stay in parity with the slashcmd
// catalog that the web hub + frontend menu read, so /help and the "/" menu never
// describe the same command differently. Building the command lists only creates
// closures (never invokes them), so a zero-value console is sufficient.
func TestREPLCommandsMatchCatalog(t *testing.T) {
	r := &AgentConsole{}
	var repl []Command
	repl = append(repl, r.builtinCommands()...)
	repl = append(repl, r.providerCommands()...)
	repl = append(repl, r.ioaCommands()...)

	catalog := make(map[string]slashcmd.Spec)
	for _, s := range slashcmd.Core() {
		catalog[s.Name] = s
	}

	replNames := make(map[string]bool, len(repl))
	for _, c := range repl {
		replNames[c.Name] = true
		s, ok := catalog[c.Name]
		if !ok {
			t.Errorf("REPL command %q has no slashcmd.Core() entry — add it to the catalog", c.Name)
			continue
		}
		if s.Description != c.Description {
			t.Errorf("%q description drift:\n  REPL:    %q\n  catalog: %q", c.Name, c.Description, s.Description)
		}
		if !reflect.DeepEqual(s.Aliases, c.Aliases) {
			t.Errorf("%q alias drift:\n  REPL:    %v\n  catalog: %v", c.Name, c.Aliases, s.Aliases)
		}
	}

	// Every agent-scope, runnable catalog command must exist in the REPL (hub-scope
	// entries like /scan, /agents run only on the web hub and have no REPL handler).
	for _, s := range slashcmd.Core() {
		if s.Scope != slashcmd.ScopeAgent {
			continue
		}
		if !replNames[s.Name] {
			t.Errorf("agent-scope catalog command %q has no REPL command", s.Name)
		}
	}
}
