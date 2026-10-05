//go:build full

package playwright

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
)

type nativeOperation struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Command string `json:"command"`
}

// nativeArgs shares the executable command's global-session normalization.
func (c *Command) nativeArgs(argv []string) (string, []string) {
	session := c.defaultSession
	clean := []string{}
	for i := 0; i < len(argv); i++ {
		if argv[i] == "-s" && i+1 < len(argv) {
			i++
			session = argv[i]
		} else if strings.HasPrefix(argv[i], "-s=") {
			session = argv[i][3:]
		} else {
			clean = append(clean, argv[i])
		}
	}
	if len(clean) == 0 {
		return "", nil
	}
	sub, args := clean[0], clean[1:]
	if session != "" {
		args = c.injectGlobalSession(sub, args, session)
	}
	return sub, args
}
func (c *Command) nativeAccess(call coretool.NativeCall) (coretool.NativeAccess, error) {
	if call.Name != "bash" || len(call.Argv) < 2 || call.Argv[0] != "playwright" {
		return coretool.NativeUnsupported, nil
	}
	sub, args := c.nativeArgs(call.Argv[1:])
	if sub == "snapshot" {
		for _, arg := range args {
			if strings.Contains(arg, "://") {
				return coretool.NativeUnsupported, nil
			}
		}
	}
	switch sub {
	case "operation-status":
		if len(args) == 1 {
			return coretool.NativeRead, nil
		}
	case "content", "network":
		// These commands fall back to URL navigation for any unknown first
		// argument, including bare hostnames. Only an actual session is a read.
		if c.firstArgIsSession(args) {
			return coretool.NativeRead, nil
		}
	case "goto":
		// The native goto session variant extracts current text; the URL
		// variant navigates. Mirror the executable command's dispatch.
		if c.firstArgIsSession(args) {
			return coretool.NativeRead, nil
		}
		if len(args) > 0 {
			return coretool.NativeEffect, nil
		}
	case "snapshot", "get-attribute", "input-value", "inner-text", "is-visible", "is-hidden", "is-enabled", "is-disabled", "is-checked", "title", "url", "requests", "request", "route-list", "tab-list", "wait-for", "wait-for-url", "wait-for-request", "wait-for-response":
		// URL variants navigate. Only existing-session reads are supported.
		if len(args) > 0 && !strings.Contains(args[0], "://") && !strings.HasPrefix(args[0], "-") {
			return coretool.NativeRead, nil
		}
	case "sessions", "list":
		return coretool.NativeRead, nil
	case "open", "click", "fill", "type", "press", "select-option", "check", "uncheck", "hover", "dblclick", "tap", "focus", "blur", "close", "reload", "go-back", "back", "go-forward", "forward", "scroll":
		if len(args) > 0 {
			return coretool.NativeEffect, nil
		}
	}
	// Arbitrary evaluate, interception, file writes and unqualified capabilities
	// remain unsupported; model-supplied read:true cannot override this.
	return coretool.NativeUnsupported, nil
}
func (c *Command) NativeContract() coretool.NativeContract {
	return coretool.NativeContract{ID: "playwright", Version: "1", Description: "Persistent browser native operations. snapshot --json is a structured read. evaluate is unsupported. Successful actions acknowledge native dispatch only; page/job completion requires fresh evidence. operation-status <host call ID> inspects the native journal.", Classify: c.nativeAccess,
		Outcome: func(call coretool.NativeCall, _ map[string]any) string {
			c.nativeMu.Lock()
			defer c.nativeMu.Unlock()
			r, ok := c.nativeOps[call.ID]
			if ok && r.State == "returned" {
				return "applied"
			}
			return "unknown"
		},
		Resolve: func(effect, read coretool.NativeCall, _ map[string]any) bool {
			if len(read.Argv) < 2 {
				return false
			}
			sub, args := c.nativeArgs(read.Argv[1:])
			if sub != "operation-status" || len(args) != 1 || effect.ID == "" || args[0] != effect.ID {
				return false
			}
			c.nativeMu.Lock()
			defer c.nativeMu.Unlock()
			r, ok := c.nativeOps[effect.ID]
			return ok && r.State == "returned"
		},
	}
}
func (c *Command) beginNative(ctx context.Context, sub string, args []string) func(error) {
	id := operation.InvocationFromContext(ctx).CallID
	if id == "" {
		return func(error) {}
	}
	c.nativeMu.Lock()
	if c.nativeOps == nil {
		c.nativeOps = map[string]nativeOperation{}
	}
	// Retain unresolved records. Losing an old returned receipt fails closed.
	if len(c.nativeOps) >= 512 {
		for key, r := range c.nativeOps {
			if r.State == "returned" {
				delete(c.nativeOps, key)
				break
			}
		}
	}
	c.nativeOps[id] = nativeOperation{ID: id, State: "executing", Command: sub}
	c.nativeMu.Unlock()
	return func(err error) {
		c.nativeMu.Lock()
		defer c.nativeMu.Unlock()
		r := c.nativeOps[id]
		r.State = "unknown"
		if err == nil {
			r.State = "returned"
		}
		c.nativeOps[id] = r
	}
}
func (c *Command) execOperationStatus(args []string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("usage: playwright operation-status <host-call-id>")
	}
	c.nativeMu.Lock()
	r, ok := c.nativeOps[args[0]]
	c.nativeMu.Unlock()
	if !ok {
		return "", fmt.Errorf("native operation receipt unavailable")
	}
	data, err := json.Marshal(map[string]any{"native_operation": r})
	return string(data), err
}
