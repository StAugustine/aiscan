package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/aop"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func legacyDatabase(t *testing.T, invalid bool) (string, *aop.Event) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE chat_sessions (id TEXT PRIMARY KEY, title TEXT); INSERT INTO chat_sessions VALUES ('session', 'retained title')`); err != nil {
		t.Fatal(err)
	}
	ddl := strings.ReplaceAll(strings.ReplaceAll(eventTableDDL, "event_proto", "event_json"), "BLOB", "TEXT")
	if _, err := db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{
		`CREATE INDEX idx_aop_events_session ON chat_aop_events(session_id, cursor)`,
		`CREATE INDEX idx_aop_events_turn ON chat_aop_events(turn_id)`,
		`CREATE UNIQUE INDEX idx_aop_events_event_id ON chat_aop_events(session_id, event_id)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	event := &aop.Event{Id: "event", SessionId: "session", TurnId: "turn", Emitter: "node", Seq: 17,
		Payload: &aop.Event_ToolResult{ToolResult: &aop.ToolResult{CallId: "call", Name: "bash", DurationMs: 31, Terminate: true, Output: []*aop.Content{aop.Text("完整输出")}}}}
	raw, err := protojson.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO chat_aop_events VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, "row", "session", event.Id, 9, "turn", "node", 17, string(raw), "original-time"); err != nil {
		t.Fatal(err)
	}
	if invalid {
		if _, err := db.Exec(`INSERT INTO chat_aop_events VALUES ('bad-row', 'session', 'bad-event', 10, '', '', 0, '{invalid', 'bad-time')`); err != nil {
			t.Fatal(err)
		}
	}
	return path, event
}

func TestMigrationRetainsHistoryAndIndexes(t *testing.T) {
	path, event := legacyDatabase(t, false)
	count, err := migrateEvents(t.Context(), path)
	if err != nil || count != 1 {
		t.Fatalf("migration = %d, %v", count, err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var raw []byte
	var cursor, sequence int64
	if err := db.QueryRow(`SELECT cursor, sequence, event_proto FROM chat_aop_events`).Scan(&cursor, &sequence, &raw); err != nil {
		t.Fatal(err)
	}
	restored := new(aop.Event)
	if err := proto.Unmarshal(raw, restored); err != nil || cursor != 9 || sequence != 17 || !proto.Equal(restored, event) {
		t.Fatalf("history after migration = %v, cursor=%d sequence=%d, %v", restored, cursor, sequence, err)
	}
	var id, time, kind string
	if err := db.QueryRow(`SELECT id, created_at, typeof(event_proto) FROM chat_aop_events`).Scan(&id, &time, &kind); err != nil || id != "row" || time != "original-time" || kind != "blob" {
		t.Fatalf("stored metadata = %q, %q, %q, %v", id, time, kind, err)
	}
	var indexes int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name LIKE 'idx_aop_events_%'`).Scan(&indexes); err != nil || indexes != 3 {
		t.Fatalf("indexes = %d, %v", indexes, err)
	}
	var title string
	if err := db.QueryRow(`SELECT title FROM chat_sessions WHERE id = 'session'`).Scan(&title); err != nil || title != "retained title" {
		t.Fatalf("unrelated session data changed: %q, %v", title, err)
	}
	if _, err := db.Exec(`INSERT INTO chat_aop_events SELECT 'duplicate', session_id, event_id, cursor + 1, turn_id, emitter, sequence, event_proto, created_at FROM chat_aop_events`); err == nil {
		t.Fatal("event identity uniqueness was lost")
	}
	if _, err := migrateEvents(t.Context(), path); err == nil {
		t.Fatal("conversion unexpectedly ran twice")
	}
}

func TestMalformedEventRollsBackWholeMigration(t *testing.T) {
	path, _ := legacyDatabase(t, true)
	if _, err := migrateEvents(t.Context(), path); err == nil || !strings.Contains(err.Error(), "bad-event") {
		t.Fatalf("migration = %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM chat_aop_events WHERE event_json IS NOT NULL`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("original records after rollback = %d, %v", count, err)
	}
	var leftovers int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'chat_aop_events_legacy_json'`).Scan(&leftovers); err != nil || leftovers != 0 {
		t.Fatalf("temporary tables after rollback = %d, %v", leftovers, err)
	}
}

func TestMigrationDoesNotCreateMissingDatabase(t *testing.T) {
	if _, err := migrateEvents(t.Context(), filepath.Join(t.TempDir(), "missing.db")); err == nil {
		t.Fatal("missing database was created")
	}
}
