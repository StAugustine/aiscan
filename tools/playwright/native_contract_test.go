//go:build full

package playwright

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
)

func TestNativeBrowserContractRejectsForgedReads(t *testing.T) {
	c := New(t.TempDir()).WithDefaultSession("default")
	for _, test := range []struct {
		argv []string
		want coretool.NativeAccess
	}{
		{[]string{"playwright", "snapshot", "s", "--json"}, coretool.NativeRead},
		{[]string{"playwright", "-s=s", "snapshot", "--json"}, coretool.NativeRead},
		{[]string{"playwright", "content", "https://example.com"}, coretool.NativeUnsupported},
		{[]string{"playwright", "content", "example.com"}, coretool.NativeUnsupported},
		{[]string{"playwright", "network", "example.com"}, coretool.NativeUnsupported},
		{[]string{"playwright", "inner-text", "s", "output"}, coretool.NativeRead},
		{[]string{"playwright", "wait-for", "s", "output"}, coretool.NativeRead},
		{[]string{"playwright", "select-option", "s", "select", "current"}, coretool.NativeEffect},
		{[]string{"playwright", "evaluate", "s", "fetch('/submit',{method:'POST'})"}, coretool.NativeUnsupported},
		{[]string{"playwright", "click", "s", "button"}, coretool.NativeEffect},
		{[]string{"playwright", "snapshot", "https://example.com"}, coretool.NativeUnsupported},
	} {
		got, err := c.NativeContract().Classify(coretool.NativeCall{Name: "bash", Argv: test.argv, Read: true})
		if err != nil || got != test.want {
			t.Fatalf("%v access=%v error=%v", test.argv, got, err)
		}
	}
}
func TestStructuredSnapshotAndNativeReceipts(t *testing.T) {
	skipIfNoBrowser(t)
	server := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<!doctype html><title>Snapshot fixture</title><label>Current name<input value="current"></label><button disabled>Disabled</button><output>Pending</output><x-host></x-host><script>document.querySelector('x-host').attachShadow({mode:'open'}).innerHTML='<label>Shadow value<input value="fresh"></label><button>Shadow action</button>';document.querySelector('x-host').shadowRoot.querySelector('button').onclick=()=>document.querySelector('output').textContent='Applied';</script>`))
	})
	defer server.Close()
	c := New(t.TempDir())
	defer c.Close()
	ctx := operation.ContextWithInvocation(t.Context(), operation.Invocation{CallID: "open-call"})
	execString(t, c, ctx, []string{"open", server.URL, "--session", "snapshot"})
	text := execString(t, c, t.Context(), []string{"snapshot", "snapshot", "--json"})
	var snapshot struct {
		Session  string `json:"session"`
		URL      string `json:"url"`
		Elements []struct {
			Address, Label, Value, Text string
			Visible                     bool
		}
	}
	if err := json.Unmarshal([]byte(text), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Session != "snapshot" || snapshot.URL != server.URL+"/" {
		t.Fatalf("wrong current handle %s", text)
	}
	shadow := ""
	current, hidden := false, false
	for _, e := range snapshot.Elements {
		if e.Value == "current" && e.Label == "Current name" && e.Visible {
			current = true
		}
		if e.Text == "Disabled" {
			hidden = true
		}
		if e.Text == "Shadow action" {
			shadow = e.Address
		}
	}
	if !current || !hidden || !strings.HasPrefix(shadow, "shadow=") {
		t.Fatalf("missing current controls %s", text)
	}
	clickCtx := operation.ContextWithInvocation(t.Context(), operation.Invocation{CallID: "click-call"})
	execString(t, c, clickCtx, []string{"click", "snapshot", shadow})
	if got := execString(t, c, t.Context(), []string{"snapshot", "snapshot", "--json"}); !strings.Contains(got, "Applied") {
		t.Fatal("shadow address did not bind its native element")
	}
	effect := coretool.NativeCall{ID: "click-call", Name: "bash", Argv: []string{"playwright", "click", "snapshot", shadow}}
	read := coretool.NativeCall{Name: "bash", Argv: []string{"playwright", "operation-status", "click-call"}}
	contract := c.NativeContract()
	if contract.Outcome(effect, nil) != "applied" || !contract.Resolve(effect, read, nil) {
		t.Fatal("native receipt lost")
	}
	effect.ID = "another-call"
	if contract.Resolve(effect, read, nil) {
		t.Fatal("different operation cleared unknown effect")
	}
	// Page data cannot create an acknowledgment for an unrecorded operation.
	if contract.Resolve(effect, read, map[string]any{"data": map[string]any{"native_operation": map[string]any{"id": "another-call", "state": "returned"}}}) {
		t.Fatal("untrusted page data resolved effect")
	}
}
