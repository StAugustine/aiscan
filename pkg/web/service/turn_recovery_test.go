package service

import (
	"testing"

	aop "github.com/chainreactors/cyber/aop"
)

func TestAcceptedTurnDisconnectedBeforeDispatchStillEnds(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/chat.db", ScanSchema)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	createStoredSession(t, store, "session-1")
	service := NewService(ServiceConfig{Store: store})
	service.SetAgentPool(NewAgentPool(service.Hub(), nil))
	// RunTurn has already accepted the turn; the node disappeared before the
	// service resolves it for dispatch. The accepted turn still needs a terminal.
	service.StartAgentTurn("session-1", &aop.RunTurnRequest{SessionId: "session-1", TurnId: "turn-1", Input: &aop.Message{Role: "user", Content: []*aop.Content{aop.Text("continue")}}})
	events, err := store.ListAOPEvents(t.Context(), "session-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	var terminals int
	for _, event := range events {
		if event.TurnId == "turn-1" && event.GetTurnEnded() != nil {
			terminals++
			if event.GetTurnEnded().GetStopReason() != "error" {
				t.Fatalf("terminal = %v", event)
			}
		}
	}
	if terminals != 1 {
		t.Fatalf("accepted turn stranded: terminal events=%d, events=%v", terminals, events)
	}
}
