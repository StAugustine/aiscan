package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/chainreactors/aiscan/pkg/acp"
)

type Service struct {
	store Store
	hub   *Hub
}

func NewService(store Store) *Service {
	if store == nil {
		store = NewMemoryStore()
	}
	return &Service{store: store, hub: NewHub()}
}

func (s *Service) Store() Store {
	return s.store
}

func (s *Service) Hub() *Hub {
	return s.hub
}

func (s *Service) ListSpaces(ctx context.Context) ([]acp.SpaceInfo, error) {
	spaces, err := s.store.ListSpaces()
	if err != nil {
		return nil, err
	}
	result := make([]acp.SpaceInfo, 0, len(spaces))
	for _, space := range spaces {
		info, err := s.spaceInfo(space)
		if err != nil {
			return nil, err
		}
		result = append(result, info)
	}
	return result, nil
}

func (s *Service) ListNodes(ctx context.Context) ([]acp.Node, error) {
	return s.store.ListNodes()
}

func (s *Service) RegisterNode(ctx context.Context, body acp.NodeCreate) (acp.Node, error) {
	if strings.TrimSpace(body.Name) == "" {
		return acp.Node{}, acp.ProtocolError(http.StatusUnprocessableEntity, "name is required")
	}
	node := acp.Node{
		ID:   acp.NewID(),
		Name: body.Name,
		Meta: defaultMeta(body.Meta),
	}
	if err := s.store.PutNode(node); err != nil {
		return acp.Node{}, err
	}
	return node, nil
}

func (s *Service) GetNode(ctx context.Context, nodeID string) (acp.Node, error) {
	node, ok, err := s.store.GetNode(nodeID)
	if err != nil {
		return acp.Node{}, err
	}
	if !ok {
		return acp.Node{}, acp.ProtocolError(http.StatusNotFound, "Node '%s' not found", nodeID)
	}
	return node, nil
}

func (s *Service) CreateSpace(ctx context.Context, callerNodeID string, body acp.SpaceCreate) (acp.SpaceInfo, error) {
	nodeID, err := s.callerNodeID(callerNodeID)
	if err != nil {
		return acp.SpaceInfo{}, err
	}
	if strings.TrimSpace(body.Name) == "" {
		return acp.SpaceInfo{}, acp.ProtocolError(http.StatusUnprocessableEntity, "Space name is required")
	}
	if strings.TrimSpace(body.Description) == "" {
		return acp.SpaceInfo{}, acp.ProtocolError(http.StatusUnprocessableEntity, "Space description is required")
	}
	space, err := s.store.PutSpaceIfAbsent(acp.Space{ID: acp.NewID(), Name: body.Name})
	if err != nil {
		return acp.SpaceInfo{}, err
	}
	if err := s.store.JoinSpace(space.ID, nodeID, body.Description); err != nil {
		return acp.SpaceInfo{}, err
	}
	return s.spaceInfo(space)
}

func (s *Service) GetSpace(ctx context.Context, spaceID string) (acp.SpaceInfo, error) {
	space, err := s.requireSpace(spaceID)
	if err != nil {
		return acp.SpaceInfo{}, err
	}
	return s.spaceInfo(space)
}

func (s *Service) SendMessage(ctx context.Context, spaceID, callerNodeID string, body acp.SendMessage) (acp.Message, error) {
	if _, err := s.requireSpace(spaceID); err != nil {
		return acp.Message{}, err
	}
	sender, err := s.callerNodeID(callerNodeID)
	if err != nil {
		return acp.Message{}, err
	}
	if body.Content == nil {
		return acp.Message{}, acp.ProtocolError(http.StatusUnprocessableEntity, "content is required")
	}
	refs := emptyRef()
	if body.Refs != nil {
		refs = completeRef(*body.Refs)
	}
	if err := s.validateRefs(refs, spaceID); err != nil {
		return acp.Message{}, err
	}
	record := acp.MessageRecord{
		ID:      acp.NewID(),
		SpaceID: spaceID,
		Sender:  sender,
		Content: body.Content,
		Refs:    refs,
	}
	if err := s.store.AppendMessage(record); err != nil {
		return acp.Message{}, err
	}
	message := acp.ExposeMessage(record)
	s.hub.Broadcast(spaceID, message)
	return message, nil
}

func (s *Service) ReadMessages(ctx context.Context, spaceID, callerNodeID string, opts acp.ReadOptions) ([]acp.Message, error) {
	if _, err := s.requireSpace(spaceID); err != nil {
		return nil, err
	}
	if opts.Limit < 0 {
		return nil, acp.ProtocolError(http.StatusUnprocessableEntity, "limit must be greater than 0")
	}
	if opts.Limit == 0 {
		opts.Limit = 0
	}
	if opts.After != "" {
		if _, ok, err := s.store.GetMessage(spaceID, opts.After); err != nil {
			return nil, err
		} else if !ok {
			return nil, acp.ProtocolError(http.StatusUnprocessableEntity, "after: '%s' not found in space '%s'", opts.After, spaceID)
		}
	}

	var records []acp.MessageRecord
	var err error
	if opts.MessageID != "" {
		if _, ok, err := s.store.GetMessage(spaceID, opts.MessageID); err != nil {
			return nil, err
		} else if !ok {
			return nil, acp.ProtocolError(http.StatusNotFound, "Message '%s' not found in space '%s'", opts.MessageID, spaceID)
		}
		records, err = s.store.GetRelatedMessages(spaceID, opts.MessageID, opts.After, opts.Limit)
	} else if opts.All {
		records, err = s.store.GetMessages(spaceID, opts.After, opts.Limit)
	} else if callerNodeID != "" {
		if _, ok, err := s.store.GetNode(callerNodeID); err != nil {
			return nil, err
		} else if !ok {
			return nil, acp.ProtocolError(http.StatusUnprocessableEntity, "caller node '%s' not found", callerNodeID)
		}
		records, err = s.store.GetMessagesForNode(spaceID, callerNodeID, opts.After, opts.Limit)
	} else {
		records, err = s.store.GetStartMessages(spaceID, opts.After, opts.Limit)
	}
	if err != nil {
		return nil, err
	}
	return exposeMessages(records), nil
}

func (s *Service) IsRelated(ctx context.Context, spaceID, rootMessageID, messageID string) (bool, error) {
	records, err := s.store.GetRelatedMessages(spaceID, rootMessageID, "", 0)
	if err != nil {
		return false, err
	}
	for _, record := range records {
		if record.ID == messageID {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) callerNodeID(nodeID string) (string, error) {
	if nodeID == "" {
		return "", acp.ProtocolError(http.StatusUnprocessableEntity, "caller node identity is required")
	}
	if _, ok, err := s.store.GetNode(nodeID); err != nil {
		return "", err
	} else if !ok {
		return "", acp.ProtocolError(http.StatusUnprocessableEntity, "caller node '%s' not found", nodeID)
	}
	return nodeID, nil
}

func (s *Service) requireSpace(spaceID string) (acp.Space, error) {
	space, ok, err := s.store.GetSpace(spaceID)
	if err != nil {
		return acp.Space{}, err
	}
	if !ok {
		return acp.Space{}, acp.ProtocolError(http.StatusNotFound, "Space '%s' not found", spaceID)
	}
	return space, nil
}

func (s *Service) spaceInfo(space acp.Space) (acp.SpaceInfo, error) {
	nodes, err := s.store.GetSpaceNodes(space.ID)
	if err != nil {
		return acp.SpaceInfo{}, err
	}
	count, err := s.store.GetMessageCount(space.ID)
	if err != nil {
		return acp.SpaceInfo{}, err
	}
	info := acp.SpaceInfo{
		ID:           space.ID,
		Name:         space.Name,
		Nodes:        make([]acp.SpaceNode, 0, len(nodes)),
		MessageCount: count,
	}
	for _, node := range nodes {
		info.Nodes = append(info.Nodes, acp.SpaceNode{
			ID:          node.Node.ID,
			Name:        node.Node.Name,
			Description: node.Description,
		})
	}
	return info, nil
}

func (s *Service) validateRefs(refs acp.Ref, spaceID string) error {
	for _, mid := range refs.Messages {
		if _, ok, err := s.store.GetMessage(spaceID, mid); err != nil {
			return err
		} else if !ok {
			return acp.ProtocolError(http.StatusUnprocessableEntity, "refs.messages: '%s' not found in space '%s'", mid, spaceID)
		}
	}
	for _, nid := range refs.Nodes {
		if _, ok, err := s.store.GetNode(nid); err != nil {
			return err
		} else if !ok {
			return acp.ProtocolError(http.StatusUnprocessableEntity, "refs.nodes: '%s' not found", nid)
		}
	}
	return nil
}

func exposeMessages(records []acp.MessageRecord) []acp.Message {
	messages := make([]acp.Message, 0, len(records))
	for _, record := range records {
		messages = append(messages, acp.ExposeMessage(record))
	}
	return messages
}

func defaultMeta(meta map[string]any) map[string]any {
	if meta == nil {
		return map[string]any{}
	}
	return meta
}

func emptyRef() acp.Ref {
	return acp.Ref{
		Messages: []string{},
		Nodes:    []string{},
	}
}

func completeRef(ref acp.Ref) acp.Ref {
	if ref.Messages == nil {
		ref.Messages = []string{}
	}
	if ref.Nodes == nil {
		ref.Nodes = []string{}
	}
	return ref
}

func statusOf(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if protocol, ok := err.(*acp.Error); ok {
		return protocol.Status
	}
	return http.StatusInternalServerError
}

func detailOf(err error) string {
	if err == nil {
		return ""
	}
	if protocol, ok := err.(*acp.Error); ok {
		return protocol.Detail
	}
	return fmt.Sprintf("%v", err)
}
