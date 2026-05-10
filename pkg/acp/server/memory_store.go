package server

import (
	"sync"

	"github.com/chainreactors/aiscan/pkg/acp"
)

type MemoryStore struct {
	mu         sync.RWMutex
	nodes      map[string]acp.Node
	spaces     map[string]acp.Space
	spaceNames map[string]string
	messages   map[string][]acp.MessageRecord
	spaceNodes map[string]map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		nodes:      make(map[string]acp.Node),
		spaces:     make(map[string]acp.Space),
		spaceNames: make(map[string]string),
		messages:   make(map[string][]acp.MessageRecord),
		spaceNodes: make(map[string]map[string]string),
	}
}

func (s *MemoryStore) PutNode(node acp.Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes[node.ID] = node
	return nil
}

func (s *MemoryStore) GetNode(nodeID string) (acp.Node, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	node, ok := s.nodes[nodeID]
	return node, ok, nil
}

func (s *MemoryStore) ListNodes() ([]acp.Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]acp.Node, 0, len(s.nodes))
	for _, node := range s.nodes {
		result = append(result, node)
	}
	return result, nil
}

func (s *MemoryStore) ListSpaces() ([]acp.Space, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]acp.Space, 0, len(s.spaces))
	for _, space := range s.spaces {
		result = append(result, space)
	}
	return result, nil
}

func (s *MemoryStore) PutSpaceIfAbsent(space acp.Space) (acp.Space, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existingID, ok := s.spaceNames[space.Name]; ok {
		if existing, ok := s.spaces[existingID]; ok {
			return existing, nil
		}
	}
	s.spaces[space.ID] = space
	s.spaceNames[space.Name] = space.ID
	if _, ok := s.messages[space.ID]; !ok {
		s.messages[space.ID] = []acp.MessageRecord{}
	}
	if _, ok := s.spaceNodes[space.ID]; !ok {
		s.spaceNodes[space.ID] = make(map[string]string)
	}
	return space, nil
}

func (s *MemoryStore) GetSpace(spaceID string) (acp.Space, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	space, ok := s.spaces[spaceID]
	return space, ok, nil
}

func (s *MemoryStore) JoinSpace(spaceID, nodeID, description string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.spaceNodes[spaceID]; !ok {
		s.spaceNodes[spaceID] = make(map[string]string)
	}
	s.spaceNodes[spaceID][nodeID] = description
	return nil
}

func (s *MemoryStore) GetSpaceNodes(spaceID string) ([]acp.SpaceNodeRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	members := s.spaceNodes[spaceID]
	result := make([]acp.SpaceNodeRecord, 0, len(members))
	for nodeID, description := range members {
		node, ok := s.nodes[nodeID]
		if !ok {
			continue
		}
		result = append(result, acp.SpaceNodeRecord{Node: node, Description: description})
	}
	return result, nil
}

func (s *MemoryStore) AppendMessage(message acp.MessageRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[message.SpaceID] = append(s.messages[message.SpaceID], message)
	return nil
}

func (s *MemoryStore) GetMessage(spaceID, messageID string) (acp.MessageRecord, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, message := range s.messages[spaceID] {
		if message.ID == messageID {
			return message, true, nil
		}
	}
	return acp.MessageRecord{}, false, nil
}

func (s *MemoryStore) GetMessagesForNode(spaceID, nodeID, after string, limit int) ([]acp.MessageRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := cloneMessages(s.messages[spaceID])
	messages := make([]acp.MessageRecord, 0, len(all))
	for _, message := range all {
		if containsString(message.Refs.Nodes, nodeID) {
			messages = append(messages, message)
		}
	}
	return windowMessages(messages, all, after, limit), nil
}

func (s *MemoryStore) GetMessageCount(spaceID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.messages[spaceID]), nil
}

func (s *MemoryStore) GetMessages(spaceID, after string, limit int) ([]acp.MessageRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := cloneMessages(s.messages[spaceID])
	return windowMessages(all, all, after, limit), nil
}

func (s *MemoryStore) GetStartMessages(spaceID, after string, limit int) ([]acp.MessageRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := cloneMessages(s.messages[spaceID])
	messages := make([]acp.MessageRecord, 0, len(all))
	for _, message := range all {
		if len(message.Refs.Messages) == 0 && len(message.Refs.Nodes) == 0 {
			messages = append(messages, message)
		}
	}
	return windowMessages(messages, all, after, limit), nil
}

func (s *MemoryStore) GetRelatedMessages(spaceID, messageID, after string, limit int) ([]acp.MessageRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := cloneMessages(s.messages[spaceID])
	return relatedMessages(all, messageID, after, limit), nil
}

func cloneMessages(messages []acp.MessageRecord) []acp.MessageRecord {
	cloned := make([]acp.MessageRecord, len(messages))
	for i, message := range messages {
		cloned[i] = message
	}
	return cloned
}

func windowMessages(messages, allMessages []acp.MessageRecord, after string, limit int) []acp.MessageRecord {
	if after != "" {
		order := make(map[string]int, len(allMessages))
		for i, message := range allMessages {
			order[message.ID] = i
		}
		afterPosition, ok := order[after]
		if !ok {
			messages = nil
		} else {
			filtered := make([]acp.MessageRecord, 0, len(messages))
			for _, message := range messages {
				if order[message.ID] > afterPosition {
					filtered = append(filtered, message)
				}
			}
			messages = filtered
		}
	}
	if limit > 0 {
		if after == "" {
			if len(messages) > limit {
				messages = messages[len(messages)-limit:]
			}
		} else if len(messages) > limit {
			messages = messages[:limit]
		}
	}
	return messages
}

func relatedMessages(allMessages []acp.MessageRecord, messageID, after string, limit int) []acp.MessageRecord {
	index := make(map[string]acp.MessageRecord, len(allMessages))
	children := make(map[string][]string)
	for _, message := range allMessages {
		index[message.ID] = message
		for _, parentID := range message.Refs.Messages {
			children[parentID] = append(children[parentID], message.ID)
		}
	}

	related := make(map[string]struct{})
	stack := []string{messageID}
	for len(stack) > 0 {
		mid := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, seen := related[mid]; seen {
			continue
		}
		message, ok := index[mid]
		if !ok {
			continue
		}
		related[mid] = struct{}{}
		stack = append(stack, message.Refs.Messages...)
	}

	stack = []string{messageID}
	for len(stack) > 0 {
		mid := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, childID := range children[mid] {
			if _, seen := related[childID]; seen {
				continue
			}
			related[childID] = struct{}{}
			stack = append(stack, childID)
		}
	}

	messages := make([]acp.MessageRecord, 0, len(related))
	for _, message := range allMessages {
		if _, ok := related[message.ID]; ok {
			messages = append(messages, message)
		}
	}
	return windowMessages(messages, allMessages, after, limit)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
