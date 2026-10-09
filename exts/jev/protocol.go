package jev

import (
	"context"
	"fmt"
	"time"

	agentsession "github.com/chainreactors/cyber/agent/session"
	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/operation"
	"google.golang.org/protobuf/proto"
)

type ProtocolExtension struct{}

func NewProtocol() *ProtocolExtension { return &ProtocolExtension{} }
func (*ProtocolExtension) Load(scope *extension.Scope) error {
	runtime, err := extension.Use[*Extension](scope)
	if err != nil {
		return err
	}
	sessions, err := extension.Use[*agentsession.Runtime](scope)
	if err != nil {
		return err
	}
	return extension.Add(scope, aop.Binding{Prototype: &ProtocolMessage{}, Open: func() aop.NamespaceHandler {
		return protocolHandler(runtime, sessions)
	}})
}

func protocolHandler(runtime *Extension, sessions *agentsession.Runtime) aop.NamespaceHandler {
	return func(ctx context.Context, envelope *aop.Envelope, message proto.Message, send aop.SendFunc) error {
		value, ok := message.(*ProtocolMessage)
		if !ok {
			return fmt.Errorf("unexpected JEV namespace message")
		}
		reply := func(payload proto.Message) error { return send(aop.Reply(envelope.GetId(), payload)) }
		request := value.GetRequest()
		sessionID := request.GetSessionId()
		if wait := value.GetWaitIdle(); wait != nil {
			sessionID = wait.SessionId
		}
		if sessionID == "" {
			return reply(aop.NewProtocolError("INVALID_ARGUMENT", "session_id is required"))
		}
		if bound := operation.InvocationFromContext(ctx).SessionID; bound != "" && bound != sessionID {
			return reply(aop.NewProtocolError("JEV_DENIED", "session scope mismatch"))
		}
		if sessions == nil || len(sessions.SessionIDs(sessionID)) == 0 {
			return reply(aop.NewProtocolError("JEV_DENIED", "session is not open on this node"))
		}
		if wait := value.GetWaitIdle(); wait != nil {
			timeout := 6 * time.Minute
			if wait.TimeoutMs > 0 && int64(wait.TimeoutMs) < timeout.Milliseconds() {
				timeout = time.Duration(wait.TimeoutMs) * time.Millisecond
			}
			waitCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			err := runtime.WaitIdle(waitCtx)
			return reply(&ProtocolMessage{Message: &ProtocolMessage_Idle{Idle: &WaitIdleResponse{Settled: err == nil, Error: errorText(err)}}})
		}
		return reply(&ProtocolMessage{Message: &ProtocolMessage_Library{Library: runtime.libraryView()}})
	}
}
