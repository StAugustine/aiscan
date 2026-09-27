package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	aop "github.com/chainreactors/cyber/aop"

	coreregistry "github.com/chainreactors/cyber/core/registry"
)

var (
	ErrInvalidCommand   = errors.New("invalid command registration")
	ErrDuplicateCommand = coreregistry.ErrDuplicate
	ErrUnavailable      = coreregistry.ErrUnavailable
	ErrStaleChoice      = errors.New("observed choice is stale")
)

// Command is an immutable native command declaration. Its dependencies are
// captured by Run when the owning extension is constructed.
type Command struct {
	Name  string
	Usage string
	// QuickReference is the command's inline reference in the system prompt. A
	// command that declares none is listed by its Usage summary line, so a Usage
	// that opens with the generated "Usage:" header has to declare one.
	QuickReference  string
	DescriptionPath string
	Run             func(context.Context, *Execution) (any, error)
	// Choices observes current tool-owned state. It must not execute actions.
	// Calls are native Executor calls, bounded, session-scoped and checked again
	// by the tool at execution. Empty choices require ordinary model reasoning.
	Choices func(context.Context, []*aop.Message) (json.RawMessage, map[string]*aop.Content, error)
	// Contract invalidates learned decisions when candidate semantics change.
	Contract string
}

// StripShellSyntax rejects shell constructs a pseudo-command cannot honor.
func StripShellSyntax(tokens []string) ([]string, error) {
	clean := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token == "|" || token == "||" {
			return nil, fmt.Errorf("pseudo-commands run in-process and do not support shell pipes (got %q). To limit output, use the command's own flags or call a separate filter step", token)
		}
		if token == "&&" || token == ";" {
			return nil, fmt.Errorf("pseudo-commands do not support shell command chaining (got %q). Issue each command separately", token)
		}
		if isStderrDup(token) {
			continue
		}
		if isFileRedirection(token) {
			return nil, fmt.Errorf("pseudo-commands do not support file redirection (got %q); use the returned tool result", token)
		}
		clean = append(clean, token)
	}
	return clean, nil
}

func isStderrDup(token string) bool {
	switch token {
	case "2>&1", "1>&2", ">&2", ">&1":
		return true
	default:
		return false
	}
}

func isFileRedirection(token string) bool {
	switch token {
	case ">", ">>", "<", "<<", "2>", "1>", "0<", "&>", "&>>":
		return true
	}
	for _, prefix := range []string{"&>", "2>", "1>", "0<", ">>", ">", "<<", "<"} {
		if strings.HasPrefix(token, prefix) {
			return true
		}
	}
	return false
}
