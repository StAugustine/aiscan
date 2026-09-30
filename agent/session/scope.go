package session

import "sort"

// SessionIDs snapshots live physical session IDs in a logical session's tree.
// Control adapters use this to scope child operations to their owning session.
func (rt *Runtime) SessionIDs(root string) []string {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	_, state := rt.findSessionLocked(root)
	if state == nil {
		return nil
	}
	selected := map[string]bool{state.id: true}
	for changed := true; changed; {
		changed = false
		for _, child := range rt.sessions {
			if child != nil && selected[child.parentSessionID] && !selected[child.id] {
				selected[child.id] = true
				changed = true
			}
		}
	}
	out := make([]string, 0, len(selected))
	for id := range selected {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
