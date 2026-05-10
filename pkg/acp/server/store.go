package server

import "github.com/chainreactors/aiscan/pkg/acp"

type Store interface {
	PutNode(node acp.Node) error
	GetNode(nodeID string) (acp.Node, bool, error)
	ListNodes() ([]acp.Node, error)

	PutSpaceIfAbsent(space acp.Space) (acp.Space, error)
	GetSpace(spaceID string) (acp.Space, bool, error)
	ListSpaces() ([]acp.Space, error)
	JoinSpace(spaceID, nodeID, description string) error
	GetSpaceNodes(spaceID string) ([]acp.SpaceNodeRecord, error)

	AppendMessage(message acp.MessageRecord) error
	GetMessage(spaceID, messageID string) (acp.MessageRecord, bool, error)
	GetMessagesForNode(spaceID, nodeID, after string, limit int) ([]acp.MessageRecord, error)
	GetMessageCount(spaceID string) (int, error)
	GetMessages(spaceID, after string, limit int) ([]acp.MessageRecord, error)
	GetStartMessages(spaceID, after string, limit int) ([]acp.MessageRecord, error)
	GetRelatedMessages(spaceID, messageID, after string, limit int) ([]acp.MessageRecord, error)
}
