package service

import (
	"context"
	"fmt"
	"testing"

	aop "github.com/chainreactors/cyber/aop"
)

func terminalFixture(id, origin, turn string) *aop.Event {
	return &aop.Event{Id: id, SessionId: origin, TurnId: turn,
		Payload: &aop.Event_TurnEnded{TurnEnded: &aop.TurnEnded{StopReason: "completed"}}}
}

func TestTerminalDedupSurvivesServiceRestartAndDifferentEventIDs(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/events.db", ScanSchema)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	createStoredSession(t, store, "parent")
	first := NewService(ServiceConfig{Store: store})
	defer first.Close(context.Background())
	first.BroadcastAOPEvent("parent", terminalFixture("first", "parent", "same-turn"))
	first.BroadcastAOPEvent("parent", terminalFixture("child-first", "child", "same-turn"))
	restarted := NewService(ServiceConfig{Store: store})
	defer restarted.Close(context.Background())
	replay, stop := restarted.hub.SubscribeAOP("parent")
	defer stop()
	restarted.BroadcastAOPEvent("parent", terminalFixture("retry-new-id", "parent", "same-turn"))
	restarted.BroadcastAOPEvent("parent", terminalFixture("child-retry-new-id", "child", "same-turn"))
	select {
	case event := <-replay:
		t.Fatalf("restarted service republished a completed turn: %v", event)
	default:
	}
	restarted.BroadcastAOPEvent("parent", terminalFixture("next", "parent", "next-turn"))
	if got := (<-replay).Event.Seq; got != 3 {
		t.Fatalf("duplicate terminals consumed timeline sequence: %d", got)
	}
	stored, err := store.ListAOPEvents(t.Context(), "parent", 20)
	if err != nil || len(stored) != 3 {
		t.Fatalf("durable terminals=%v, error=%v", stored, err)
	}
}

func TestExplicitTurnIDReusePreservesBothExecutionGenerations(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(fmt.Sprintf("started=%v", started), func(t *testing.T) {
			store, err := NewSQLiteStore(t.TempDir()+"/events.db", ScanSchema)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			createStoredSession(t, store, "session")
			svc := NewService(ServiceConfig{Store: store})
			defer svc.Close(context.Background())
			svc.BroadcastAOPEvent("session", terminalFixture("first", "session", "reused"))
			if err := svc.resetTurnTerminal("session", "reused"); err != nil {
				t.Fatal(err)
			}
			if started {
				svc.BroadcastAOPEvent("session", &aop.Event{Id: "second-start", SessionId: "session", TurnId: "reused",
					Payload: &aop.Event_TurnStarted{TurnStarted: &aop.TurnStarted{}}})
				ended, err := store.HasTurnEnded(t.Context(), "session", "reused")
				if err != nil || ended {
					t.Fatalf("previous generation ended the new execution: %v, %v", ended, err)
				}
			}
			// Admission can fail before Node emits TurnStarted. That execution also
			// needs its own terminal, while further duplicates stay suppressed.
			svc.BroadcastAOPEvent("session", terminalFixture("second", "session", "reused"))
			svc.BroadcastAOPEvent("session", terminalFixture("second-retry", "session", "reused"))
			stored, err := store.ListAOPEvents(t.Context(), "session", 20)
			if err != nil {
				t.Fatal(err)
			}
			var terminals []string
			for _, event := range stored {
				if event.GetTurnEnded() != nil {
					terminals = append(terminals, event.Id)
				}
			}
			if fmt.Sprint(terminals) != "[first second]" {
				t.Fatalf("execution generations lost or duplicated: %v", terminals)
			}
		})
	}
}

func TestTransientDeltasDoNotReuseDurableSequenceOrDeliveryCursor(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/events.db", ScanSchema)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	createStoredSession(t, store, "session")
	svc := NewService(ServiceConfig{Store: store})
	defer svc.Close(context.Background())
	deliveries, stop := svc.hub.SubscribeAOP("session")
	defer stop()
	svc.BroadcastAOPEvent("session", &aop.Event{Id: "delta", SessionId: "session", TurnId: "turn",
		Payload: &aop.Event_MessageDelta{MessageDelta: &aop.MessageDelta{MessageId: "answer"}}})
	svc.BroadcastAOPEvent("session", terminalFixture("end", "session", "turn"))
	delta, end := <-deliveries, <-deliveries
	if delta.Event.Seq != 1 || end.Event.Seq != 2 || delta.Cursor != "" || end.Cursor != "1" {
		t.Fatalf("live sequence and durable cursor conflated: %v, %v", delta, end)
	}
	restarted := NewService(ServiceConfig{Store: store})
	defer restarted.Close(context.Background())
	restarted.BroadcastAOPEvent("session", terminalFixture("after-restart", "session", "next"))
	stored, err := store.ListAOPEvents(t.Context(), "session", 20)
	if err != nil || len(stored) != 2 || stored[1].Seq != 3 {
		t.Fatalf("durable sequence after live deltas/restart=%v, error=%v", stored, err)
	}
}

func TestChildSessionStartDoesNotReportRootContextLoss(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir()+"/events.db", ScanSchema)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	createStoredSession(t, store, "parent")
	svc := NewService(ServiceConfig{Store: store})
	defer svc.Close(context.Background())
	svc.BroadcastAOPEvent("parent", terminalFixture("root-history", "parent", "turn"))
	svc.BroadcastAOPEvent("parent", &aop.Event{Id: "child-start", SessionId: "child",
		Payload: &aop.Event_SessionStarted{SessionStarted: &aop.SessionStarted{ParentSessionId: "parent"}}})
	stored, err := store.ListAOPEvents(t.Context(), "parent", 20)
	if err != nil || len(stored) != 2 || stored[1].GetSessionStarted() == nil {
		t.Fatalf("child creation incorrectly announced root context loss: %v, %v", stored, err)
	}
}

func TestTerminalDedupWithoutStoreRetainsTurnReuse(t *testing.T) {
	svc := NewService(ServiceConfig{})
	defer svc.Close(context.Background())
	deliveries, stop := svc.hub.SubscribeAOP("session")
	defer stop()
	svc.BroadcastAOPEvent("session", terminalFixture("first", "session", "turn"))
	svc.BroadcastAOPEvent("session", terminalFixture("duplicate", "session", "turn"))
	if err := svc.resetTurnTerminal("session", "turn"); err != nil {
		t.Fatal(err)
	}
	svc.BroadcastAOPEvent("session", terminalFixture("second", "session", "turn"))
	if len(deliveries) != 2 {
		t.Fatalf("in-memory mode lost turn reuse or terminal dedup: %d deliveries", len(deliveries))
	}
}
