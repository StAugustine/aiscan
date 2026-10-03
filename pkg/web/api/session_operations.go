package api

import "context"

type operationGate struct {
	users int
	token chan struct{}
}

// lockOperations keeps one request ID atomic across methods, while unrelated
// sessions can dispatch independently. Reset/Delete take their management gate
// before the request gate: Reset calls Open/Close with child request IDs, and a
// waiting Reset must not reserve one of those IDs while holding up its parent.
func (s *Sessions) lockOperations(ctx context.Context, keys ...string) (func(), error) {
	releases := make([]func(), 0, len(keys))
	release := func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}
	for _, key := range keys {
		unlock, err := s.lockOperation(ctx, key)
		if err != nil {
			release()
			return nil, err
		}
		releases = append(releases, unlock)
	}
	return release, nil
}

func (s *Sessions) lockOperation(ctx context.Context, key string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.operationsMu.Lock()
	if s.operations == nil {
		s.operations = make(map[string]*operationGate)
	}
	gate := s.operations[key]
	if gate == nil {
		gate = &operationGate{token: make(chan struct{}, 1)}
		s.operations[key] = gate
	}
	gate.users++
	s.operationsMu.Unlock()
	drop := func() {
		s.operationsMu.Lock()
		gate.users--
		if gate.users == 0 {
			delete(s.operations, key)
		}
		s.operationsMu.Unlock()
	}
	select {
	case gate.token <- struct{}{}:
		unlock := func() { <-gate.token; drop() }
		if err := ctx.Err(); err != nil {
			unlock()
			return nil, err
		}
		return unlock, nil
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	}
}
