package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
	"sync"

	"github.com/chainreactors/cyber/core/resource"
)

// NativeCall is a normalized invocation. Argv is reconstructed by the host,
// never trusted from model-supplied metadata.
type NativeCall struct {
	ID         string          `json:"call_id,omitempty"`
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments"`
	Read       bool            `json:"read,omitempty"`
	Step       string          `json:"step,omitempty"`
	Occurrence int             `json:"occurrence,omitempty"`
	Argv       []string        `json:"argv,omitempty"`
}

type NativeAccess string

const (
	NativeRead        NativeAccess = "read"
	NativeEffect      NativeAccess = "effect"
	NativeUnsupported NativeAccess = "unsupported"
)

// NativeContract describes a tool's protocol, not a business workflow. Outcome
// concerns the native operation; it must not infer business success from exit 0.
// Resolve may acknowledge only the same native operation identity.
type NativeContract struct {
	ID          string
	Version     string
	Description string
	Classify    func(NativeCall) (NativeAccess, error)
	Outcome     func(NativeCall, map[string]any) string
	Resolve     func(NativeCall, NativeCall, map[string]any) bool
}

// NativeContracts is a detached snapshot of tool-owned protocol contracts.
// Consumers classify against one snapshot throughout validation or execution.
type NativeContracts map[string]NativeContract

// NativeContractRegistry is installed by the command registry. Tool owners add
// protocol contracts during extension loading; consumers borrow snapshots.
type NativeContractRegistry struct {
	mu     sync.RWMutex
	values map[string]NativeContract
	owners map[string]uint64
	next   uint64
}

func NewNativeContractRegistry() *NativeContractRegistry {
	return &NativeContractRegistry{values: map[string]NativeContract{}, owners: map[string]uint64{}}
}

func (r *NativeContractRegistry) Register(c NativeContract) error {
	_, err := r.Add(c)
	return err
}

// Add contributes contracts atomically. Extension scopes own the returned
// handle, so failed loading and unloading also remove native capabilities.
func (r *NativeContractRegistry) Add(contracts ...NativeContract) (resource.Handle, error) {
	if r == nil || len(contracts) == 0 {
		return nil, errors.New("native contract needs identity, version and classification")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := map[string]bool{}
	for _, c := range contracts {
		if c.ID == "" || c.Version == "" || c.Classify == nil {
			return nil, errors.New("native contract needs identity, version and classification")
		}
		if _, ok := r.values[c.ID]; ok || seen[c.ID] {
			return nil, fmt.Errorf("duplicate native contract %q", c.ID)
		}
		seen[c.ID] = true
	}
	r.next++
	owner := r.next
	for _, c := range contracts {
		r.values[c.ID], r.owners[c.ID] = c, owner
	}
	return resource.HandleFunc(func(context.Context) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		for _, c := range contracts {
			if r.owners[c.ID] == owner {
				delete(r.values, c.ID)
				delete(r.owners, c.ID)
			}
		}
		return nil
	}), nil
}

func (r *NativeContractRegistry) Snapshot() NativeContracts {
	if r == nil {
		return NativeContracts{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return maps.Clone(r.values)
}

func (r *NativeContractRegistry) Catalog() map[string]any {
	out := map[string]any{}
	for id, c := range r.Snapshot() {
		out[id] = map[string]string{"version": c.Version, "description": c.Description}
	}
	return out
}

func (r *NativeContractRegistry) Access(call NativeCall) (NativeAccess, error) {
	return r.Snapshot().Access(call)
}

func (values NativeContracts) Access(call NativeCall) (NativeAccess, error) {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	access := NativeUnsupported
	for _, id := range ids {
		current, err := values[id].Classify(call)
		if err != nil {
			return NativeUnsupported, err
		}
		if current == NativeUnsupported {
			continue
		}
		if current != NativeRead && current != NativeEffect {
			return NativeUnsupported, errors.New("invalid native access classification")
		}
		if access != NativeUnsupported && current != access {
			return NativeUnsupported, errors.New("conflicting native contracts")
		}
		access = current
	}
	if access == NativeUnsupported {
		return access, errors.New("unsupported native operation")
	}
	return access, nil
}
