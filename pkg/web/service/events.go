package service

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strconv"

	aop "github.com/chainreactors/cyber/aop"
	types "github.com/chainreactors/cyber/core/types"
	scanpb "github.com/chainreactors/cyber/pkg/web/scan"
	proto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Sequence includes live deltas that are deliberately not persisted. Keep
// only that counter in memory; durable turn terminals belong to the store.
// Boundaries are needed only for explicit turn-ID reuse before TurnStarted,
// or for terminal deduplication when running without a durable store.
type sessionEventState struct {
	sequence uint64
	turns    map[string]turnEventBoundary
}

type turnEventBoundary struct {
	cursor int64
	ended  bool
}

// BroadcastAOPEvent accepts a copy into the Web-owned timeline. Node sequence
// numbers belong to a different publication stream and never order this one.
func (s *Service) BroadcastAOPEvent(sessionID string, event *aop.Event) {
	if s == nil || s.hub == nil || sessionID == "" || event == nil || event.Payload == nil {
		return
	}
	recreated, err := s.acceptAOPEvent(sessionID, proto.CloneOf(event))
	if err != nil {
		slog.Error("persist AOP event", "session", sessionID, "error", err)
		return
	}
	if recreated {
		s.broadcastSystemMessage(sessionID, SysSessionContextReset, "The node recreated this session; the agent no longer has the earlier conversation in context.", nil)
	}
}

func (s *Service) acceptAOPEvent(sessionID string, event *aop.Event) (bool, error) {
	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	if event.SessionId == "" {
		event.SessionId = sessionID
	}
	if event.Id == "" {
		event.Id = generateID()
	}
	if event.EmittedAt == nil {
		event.EmittedAt = timestamppb.Now()
	}
	if s.eventState == nil {
		s.eventState = make(map[string]*sessionEventState)
	}
	state := s.eventState[sessionID]
	if state == nil {
		state = &sessionEventState{}
		if s.store != nil {
			var err error
			state.sequence, err = s.store.MaxAOPEventSeq(context.Background(), sessionID)
			if err != nil {
				return false, err
			}
		}
		s.eventState[sessionID] = state
	}
	turn := event.SessionId + "\x00" + event.TurnId
	boundary := state.turns[turn]
	if s.store == nil && event.GetTurnEnded() != nil && event.TurnId != "" && boundary.ended {
		return false, nil
	}
	recreated := s.sessionWasRecreated(sessionID, event)
	event.Seq = state.sequence + 1
	var cursor int64
	if s.store != nil {
		var persisted bool
		var err error
		cursor, persisted, err = s.store.appendAOPEvent(context.Background(), sessionID, event, boundary.cursor)
		if err != nil {
			// Deleting a session also deletes its history. Its remote close and
			// optional recap may still be in flight; they must not recreate it
			// or report a persistence failure for an intentionally removed row.
			if _, sessionErr := s.store.GetSession(context.Background(), sessionID); errors.Is(sessionErr, sql.ErrNoRows) {
				delete(s.eventState, sessionID)
				return false, nil
			}
			return false, err
		}
		if cursor > 0 && !persisted {
			return false, nil
		}
	}
	// Commit in-memory state only after persistence succeeded, so a retry after
	// storage failure can still publish the terminal event.
	state.sequence = event.Seq
	if event.TurnId != "" {
		if s.store != nil && (event.GetTurnStarted() != nil || event.GetTurnEnded() != nil) {
			delete(state.turns, turn)
		} else if s.store == nil && event.GetTurnEnded() != nil {
			if state.turns == nil {
				state.turns = make(map[string]turnEventBoundary)
			}
			state.turns[turn] = turnEventBoundary{ended: true}
		}
	}
	s.broadcastAOPEvent(sessionID, event, cursor)
	return recreated, nil
}

// sessionWasRecreated reports that a SessionStarted event announces a session
// the hub already holds durable history for. A node only recreates a session it
// has lost from memory (restart or eviction); the transcript survives here, so
// the agent silently resumes with an empty context unless the operator is told.
func (s *Service) sessionWasRecreated(sessionID string, event *aop.Event) bool {
	if s.store == nil || event.GetSessionStarted() == nil || event.SessionId != sessionID {
		return false
	}
	maximum, err := s.store.MaxAOPEventSeq(context.Background(), sessionID)
	return err == nil && maximum > 0
}

// PublishUserMessage records the operator input in the durable AOP timeline.
// Node delivery remains the caller's RunTurn/Command request; this function
// does not create a second transport path.
func (s *Service) PublishUserMessage(sessionID, turnID string, message *aop.Message) {
	if message == nil || len(message.Content) == 0 {
		return
	}
	userMessage := proto.CloneOf(message)
	if userMessage.Id == "" {
		userMessage.Id = generateID()
	}
	userMessage.Role = "user"
	s.BroadcastAOPEvent(sessionID, &aop.Event{
		SessionId: sessionID,
		TurnId:    turnID,
		Emitter:   "cyber.web",
		Payload:   &aop.Event_Message{Message: userMessage},
	})
}

func (s *Service) resetTurnTerminal(sessionID, turnID string) error {
	if sessionID == "" || turnID == "" {
		return nil
	}
	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	state := s.eventState[sessionID]
	if s.store == nil {
		if state != nil {
			delete(state.turns, sessionID+"\x00"+turnID)
		}
		return nil
	}
	ended, err := s.store.HasTurnEnded(context.Background(), sessionID, turnID)
	if err != nil || !ended {
		return err
	}
	var cursor int64
	if err := s.store.orm.NewSelect().Model((*aopEventModel)(nil)).
		ColumnExpr("COALESCE(MAX(cursor), 0)").Where("session_id = ?", sessionID).Scan(context.Background(), &cursor); err != nil {
		return err
	}
	if state == nil {
		sequence, err := s.store.MaxAOPEventSeq(context.Background(), sessionID)
		if err != nil {
			return err
		}
		state = &sessionEventState{sequence: sequence}
		if s.eventState == nil {
			s.eventState = make(map[string]*sessionEventState)
		}
		s.eventState[sessionID] = state
	}
	if state.turns == nil {
		state.turns = make(map[string]turnEventBoundary)
	}
	state.turns[sessionID+"\x00"+turnID] = turnEventBoundary{cursor: cursor}
	return nil
}

func (s *Service) broadcastAOPEvent(sessionID string, event *aop.Event, cursor int64) {
	deliveryCursor := ""
	if cursor > 0 {
		deliveryCursor = strconv.FormatInt(cursor, 10)
	}
	s.hub.BroadcastAOP(sessionID, &aop.EventDelivery{Cursor: deliveryCursor, Event: event}, isReliableAOPEvent(event))
}

// broadcastHubError emits a hub-originated failure as an AOP error event: the
// code names a translatable template (mirrored under `sys.*` in the frontend
// locales), message is the English fallback, and params feed i18n
// interpolation via the cyber.web extension.
func (s *Service) broadcastHubError(sessionID, code, message string, params map[string]any) {
	event := &aop.Event{
		Id: generateID(), EmittedAt: timestamppb.Now(), SessionId: sessionID, Emitter: "cyber.web",
		Payload: &aop.Event_Error{Error: &aop.ProtocolError{Code: code, Message: message}},
	}
	if len(params) > 0 {
		if values, err := structpb.NewStruct(params); err == nil {
			_ = types.SetWebMessage(event, &types.WebMessageMetadata{Params: values})
		}
	}
	s.BroadcastAOPEvent(sessionID, event)
}

func (s *Service) broadcastHubTurnEnded(sessionID, turnID, code, message string) {
	ended := &aop.TurnEnded{StopReason: "error", Error: &aop.ProtocolError{Code: code, Message: message}}
	s.BroadcastAOPEvent(sessionID, &aop.Event{
		SessionId: sessionID, TurnId: turnID, Emitter: "cyber.web",
		Payload: &aop.Event_TurnEnded{TurnEnded: ended},
	})
}

func isReliableAOPEvent(event *aop.Event) bool {
	switch payload := event.Payload.(type) {
	case *aop.Event_SessionEnded, *aop.Event_Error, *aop.Event_ToolResult, *aop.Event_TurnEnded, *aop.Event_Message:
		return true
	case *aop.Event_Status:
		// Status entries that drive durable UI state (eval/compact banners,
		// budget warnings) must survive reconnect; the rest are evictable.
		switch payload.Status.State {
		case types.EvalStateEnd, types.CompactStateEnd, "token_budget_warning":
			return true
		}
	}
	return false
}

// runHubCommand executes an application-level slash command that needs hub state.
// name is the canonical catalog name without its leading slash. Agent-scope
// commands never reach here; they fall through to the agent bridge.
func (s *Service) broadcastSystemMessage(sessionID, code, fallback string, params map[string]any) {
	metadata := &types.WebMessageMetadata{Code: code}
	if code != "" {
		metadata.Params, _ = structpb.NewStruct(params)
	}
	s.broadcastSystemMessageMetadata(sessionID, fallback, metadata)
}

func (s *Service) broadcastSystemMessageMetadata(sessionID, fallback string, metadata *types.WebMessageMetadata) {
	event := &aop.Event{
		Id: generateID(), EmittedAt: timestamppb.Now(), SessionId: sessionID, Emitter: "cyber.web",
		Payload: &aop.Event_Message{Message: &aop.Message{
			Id: generateID(), Role: "system", Content: []*aop.Content{aop.Text(fallback)},
		}},
	}
	if metadata != nil && (metadata.GetCode() != "" || metadata.GetNodeId() != "" || metadata.GetParams() != nil || metadata.GetAgentList() != nil || metadata.GetCommands() != nil) {
		_ = types.SetWebMessage(event, metadata)
	}
	s.BroadcastAOPEvent(sessionID, event)
}

// broadcastScanComplete mirrors a finished scan into the AOP timeline of every
// session bound to it, so a scan submitted over the scan RPC surfaces as a
// result card in the chat that commissioned it. The binding is the durable
// session_scans relation a session writes at open time (SessionBinding), not
// the in-flight task map: a scan id is never a registered session task.
// A session that binds after the scan already finished gets no live event and
// rebuilds the card from its own scan ids on load instead.
func (s *Service) broadcastScanComplete(scanID string) {
	s.broadcastScanStatus(scanID, scanpb.ScanStatus_SCAN_STATUS_COMPLETED)
}

func (s *Service) broadcastScanStatus(scanID string, status scanpb.ScanStatus) {
	if s.store == nil {
		return
	}
	sessionIDs, err := s.store.ScanSessionIDs(context.Background(), scanID)
	if err != nil || len(sessionIDs) == 0 {
		return
	}
	value, err := anypb.New(&scanpb.SessionScanEvent{ScanId: scanID, Status: status})
	if err != nil {
		return
	}
	for _, sid := range sessionIDs {
		s.BroadcastAOPEvent(sid, &aop.Event{
			SessionId: sid,
			Emitter:   "cyber.web",
			Payload:   &aop.Event_Extension{Extension: value},
		})
	}
}
