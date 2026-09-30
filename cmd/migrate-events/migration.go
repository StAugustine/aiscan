package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"

	"github.com/chainreactors/cyber/aop"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	_ "modernc.org/sqlite"
)

const eventTableDDL = `CREATE TABLE chat_aop_events (
	id VARCHAR NOT NULL PRIMARY KEY,
	session_id VARCHAR NOT NULL,
	event_id VARCHAR NOT NULL,
	cursor INTEGER NOT NULL,
	turn_id VARCHAR NOT NULL,
	emitter VARCHAR NOT NULL,
	sequence INTEGER NOT NULL,
	event_proto BLOB NOT NULL,
	created_at VARCHAR NOT NULL,
	UNIQUE(session_id, cursor),
	FOREIGN KEY(session_id) REFERENCES chat_sessions(id) ON DELETE CASCADE
)`

func migrateEvents(ctx context.Context, path string) (count int64, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return 0, err
	}
	dsn := (&url.URL{Scheme: "file", Opaque: filepath.ToSlash(abs), RawQuery: "mode=rw&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(chat_aop_events)`)
	if err != nil {
		return 0, err
	}
	var columns []string
	for rows.Next() {
		var cid, notNull, pk int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return 0, err
		}
		columns = append(columns, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	legacy := []string{"id", "session_id", "event_id", "cursor", "turn_id", "emitter", "sequence", "event_json", "created_at"}
	if !slices.Equal(columns, legacy) {
		return 0, fmt.Errorf("expected the event_json schema, found columns %v; database was not changed", columns)
	}

	// Retain the existing explicit indexes. The table's PK and cursor uniqueness
	// are recreated by the DDL; no index version or compatibility reader remains.
	rows, err = tx.QueryContext(ctx, `SELECT sql FROM sqlite_master WHERE tbl_name = 'chat_aop_events' AND type = 'index' AND sql IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	var indexes []string
	for rows.Next() {
		var statement string
		if err := rows.Scan(&statement); err != nil {
			rows.Close()
			return 0, err
		}
		indexes = append(indexes, statement)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	var triggers int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE tbl_name = 'chat_aop_events' AND type = 'trigger'`).Scan(&triggers); err != nil {
		return 0, err
	}
	if triggers != 0 {
		return 0, fmt.Errorf("custom event-table triggers require an explicit conversion; database was not changed")
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE chat_aop_events RENAME TO chat_aop_events_legacy_json`); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, eventTableDDL); err != nil {
		return 0, err
	}
	insert, err := tx.PrepareContext(ctx, `INSERT INTO chat_aop_events (id, session_id, event_id, cursor, turn_id, emitter, sequence, event_proto, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer insert.Close()
	rows, err = tx.QueryContext(ctx, `SELECT id, session_id, event_id, cursor, turn_id, emitter, sequence, event_json, created_at FROM chat_aop_events_legacy_json ORDER BY session_id, cursor`)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var id, sessionID, eventID, turnID, emitter, raw, createdAt string
		var cursor, sequence int64
		if err := rows.Scan(&id, &sessionID, &eventID, &cursor, &turnID, &emitter, &sequence, &raw, &createdAt); err != nil {
			rows.Close()
			return 0, err
		}
		event := new(aop.Event)
		if err := protojson.Unmarshal([]byte(raw), event); err != nil {
			rows.Close()
			return 0, fmt.Errorf("decode event %q: %w; transaction rolled back", eventID, err)
		}
		binary, err := proto.Marshal(event)
		if err != nil {
			rows.Close()
			return 0, fmt.Errorf("encode event %q: %w", eventID, err)
		}
		if _, err := insert.ExecContext(ctx, id, sessionID, eventID, cursor, turnID, emitter, sequence, binary, createdAt); err != nil {
			rows.Close()
			return 0, err
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE chat_aop_events_legacy_json`); err != nil {
		return 0, err
	}
	for _, statement := range indexes {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}
