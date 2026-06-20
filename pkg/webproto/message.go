package webproto

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/chainreactors/aiscan/pkg/agent/tmux"
	"github.com/chainreactors/aiscan/pkg/remotepty"
)

type Message struct {
	Type     string          `json:"type"`
	TaskID   string          `json:"task_id,omitempty"`
	StreamID string          `json:"stream_id,omitempty"`
	Data     string          `json:"data,omitempty"`
	DataB64  string          `json:"data_b64,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

type RegisterPayload struct {
	Name     string        `json:"name"`
	Commands []string      `json:"commands,omitempty"`
	Identity AgentIdentity `json:"identity,omitempty"`
	Stats    AgentStats    `json:"stats,omitempty"`
}

type AgentIdentity struct {
	NodeID       string         `json:"node_id,omitempty"`
	NodeName     string         `json:"node_name,omitempty"`
	Space        string         `json:"space,omitempty"`
	IOAURL       string         `json:"ioa_url,omitempty"`
	Hostname     string         `json:"hostname,omitempty"`
	Username     string         `json:"username,omitempty"`
	WorkingDir   string         `json:"working_dir,omitempty"`
	OS           string         `json:"os,omitempty"`
	Arch         string         `json:"arch,omitempty"`
	PID          int            `json:"pid,omitempty"`
	Provider     string         `json:"provider,omitempty"`
	Model        string         `json:"model,omitempty"`
	Capabilities []string       `json:"capabilities,omitempty"`
	Meta         map[string]any `json:"meta,omitempty"`
}

type AgentStats struct {
	Turns            int    `json:"turns,omitempty"`
	ToolCalls        int    `json:"tool_calls,omitempty"`
	RunningTools     int    `json:"running_tools,omitempty"`
	PromptTokens     int    `json:"prompt_tokens,omitempty"`
	CompletionTokens int    `json:"completion_tokens,omitempty"`
	TotalTokens      int    `json:"total_tokens,omitempty"`
	CacheReadTokens  int    `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int    `json:"cache_write_tokens,omitempty"`
	Assets           int    `json:"assets,omitempty"`
	Loots            int    `json:"loots,omitempty"`
	LastEvent        string `json:"last_event,omitempty"`
}

type PTYPayload struct {
	SessionID string      `json:"session_id,omitempty"`
	Data      string      `json:"data,omitempty"`
	DataB64   string      `json:"data_b64,omitempty"`
	Command   string      `json:"command,omitempty"`
	Kind      string      `json:"kind,omitempty"`
	Args      []string    `json:"args,omitempty"`
	Name      string      `json:"name,omitempty"`
	Rows      int         `json:"rows,omitempty"`
	Cols      int         `json:"cols,omitempty"`
	Bytes     int         `json:"bytes,omitempty"`
	Singleton bool        `json:"singleton,omitempty"`
	State     tmux.State  `json:"state,omitempty"`
	ExitCode  int         `json:"exit_code,omitempty"`
	Session   *tmux.Info  `json:"session,omitempty"`
	Sessions  []tmux.Info `json:"sessions,omitempty"`
}

func MessageToFrame(msg Message) (remotepty.Frame, error) {
	frameType, ok := frameTypeFromMessage(msg.Type)
	if !ok {
		return remotepty.Frame{}, fmt.Errorf("unsupported pty message: %s", msg.Type)
	}
	payload, err := DecodePTYPayload(msg.Payload)
	if err != nil {
		return remotepty.Frame{}, err
	}
	data, err := decodeData(payload.Data, payload.DataB64)
	if err != nil {
		return remotepty.Frame{}, err
	}
	if len(data) == 0 {
		data, err = decodeData(msg.Data, msg.DataB64)
		if err != nil {
			return remotepty.Frame{}, err
		}
	}
	frame := remotepty.Frame{
		Type:      frameType,
		StreamID:  msg.StreamID,
		SessionID: payload.SessionID,
		Kind:      payload.Kind,
		Name:      payload.Name,
		Command:   payload.Command,
		Args:      append([]string(nil), payload.Args...),
		Data:      data,
		Cols:      payload.Cols,
		Rows:      payload.Rows,
		Bytes:     payload.Bytes,
		Singleton: payload.Singleton,
		State:     payload.State,
		ExitCode:  payload.ExitCode,
		Session:   payload.Session,
		Sessions:  append([]tmux.Info(nil), payload.Sessions...),
	}
	if frame.SessionID == "" && payload.Session != nil {
		frame.SessionID = payload.Session.ID
	}
	if frame.Kind == "" && payload.Session != nil {
		frame.Kind = payload.Session.Kind
	}
	if frame.Name == "" && payload.Session != nil {
		frame.Name = payload.Session.Name
	}
	return frame, nil
}

func FrameToMessage(frame remotepty.Frame) Message {
	msg := Message{
		Type:     messageTypeFromFrame(frame.Type),
		StreamID: frame.StreamID,
	}
	switch frame.Type {
	case remotepty.FrameOpen, remotepty.FrameAttach, remotepty.FrameInput, remotepty.FrameResize,
		remotepty.FrameDetach, remotepty.FrameKill, remotepty.FrameList:
		payload := PTYPayload{
			SessionID: frame.SessionID,
			Command:   frame.Command,
			Kind:      frame.Kind,
			Args:      append([]string(nil), frame.Args...),
			Name:      frame.Name,
			Rows:      frame.Rows,
			Cols:      frame.Cols,
			Bytes:     frame.Bytes,
			Singleton: frame.Singleton,
		}
		encodePayloadData(&payload, frame.Data)
		msg.Payload = mustMarshal(payload)
	case remotepty.FrameOutput:
		encodeMessageData(&msg, frame.Data)
	case remotepty.FrameError:
		if frame.Error != "" {
			msg.Data = frame.Error
		} else {
			msg.Data = string(frame.Data)
		}
	case remotepty.FrameOpened:
		msg.Payload = mustMarshal(map[string]any{
			"session_id": frame.SessionID,
			"kind":       frame.Kind,
			"name":       frame.Name,
			"pid":        sessionPID(frame),
			"session":    frame.Session,
		})
	case remotepty.FrameAttached:
		msg.Payload = mustMarshal(map[string]any{
			"session_id": frame.SessionID,
			"session":    frame.Session,
		})
	case remotepty.FrameDetached:
		msg.Payload = mustMarshal(map[string]any{"session_id": frame.SessionID})
	case remotepty.FrameSessions:
		msg.Payload = mustMarshal(map[string]any{"sessions": frame.Sessions})
	case remotepty.FrameClosed:
		msg.Payload = mustMarshal(map[string]any{
			"session_id": frame.SessionID,
			"state":      frame.State,
			"exit_code":  frame.ExitCode,
			"session":    frame.Session,
		})
	}
	return msg
}

func DecodePTYPayload(raw json.RawMessage) (PTYPayload, error) {
	var payload PTYPayload
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return payload, fmt.Errorf("decode pty payload: %w", err)
		}
	}
	return payload, nil
}

func messageTypeFromFrame(frameType remotepty.FrameType) string {
	if frameType == "" {
		return ""
	}
	return "pty." + string(frameType)
}

func frameTypeFromMessage(msgType string) (remotepty.FrameType, bool) {
	if !strings.HasPrefix(msgType, "pty.") {
		return "", false
	}
	switch strings.TrimPrefix(msgType, "pty.") {
	case string(remotepty.FrameOpen):
		return remotepty.FrameOpen, true
	case string(remotepty.FrameOpened):
		return remotepty.FrameOpened, true
	case string(remotepty.FrameAttach):
		return remotepty.FrameAttach, true
	case string(remotepty.FrameAttached):
		return remotepty.FrameAttached, true
	case string(remotepty.FrameInput):
		return remotepty.FrameInput, true
	case string(remotepty.FrameOutput):
		return remotepty.FrameOutput, true
	case string(remotepty.FrameResize):
		return remotepty.FrameResize, true
	case string(remotepty.FrameDetach):
		return remotepty.FrameDetach, true
	case string(remotepty.FrameDetached):
		return remotepty.FrameDetached, true
	case string(remotepty.FrameKill):
		return remotepty.FrameKill, true
	case string(remotepty.FrameList):
		return remotepty.FrameList, true
	case string(remotepty.FrameSessions):
		return remotepty.FrameSessions, true
	case string(remotepty.FrameClosed):
		return remotepty.FrameClosed, true
	case string(remotepty.FrameError):
		return remotepty.FrameError, true
	default:
		return "", false
	}
}

func decodeData(text, encoded string) ([]byte, error) {
	if encoded != "" {
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("decode terminal data: %w", err)
		}
		return data, nil
	}
	if text == "" {
		return nil, nil
	}
	return []byte(text), nil
}

func encodeMessageData(msg *Message, data []byte) {
	if len(data) == 0 {
		return
	}
	if utf8.Valid(data) {
		msg.Data = string(data)
		return
	}
	msg.DataB64 = base64.StdEncoding.EncodeToString(data)
}

func encodePayloadData(payload *PTYPayload, data []byte) {
	if len(data) == 0 {
		return
	}
	if utf8.Valid(data) {
		payload.Data = string(data)
		return
	}
	payload.DataB64 = base64.StdEncoding.EncodeToString(data)
}

func mustMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

func sessionPID(frame remotepty.Frame) int {
	if frame.Session == nil {
		return 0
	}
	return frame.Session.PID
}
