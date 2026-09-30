package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/types"
	scanpb "github.com/chainreactors/cyber/pkg/web/scan"
	"github.com/chainreactors/cyber/pkg/web/service"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func legacyScans(t *testing.T) (string, []*scanpb.Scan) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scans.db")
	store, err := service.NewSQLiteStore(path, service.ScanSchema)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(t.Context(), &types.SessionRecord{Session: &aop.Session{Id: "session"}}); err != nil {
		t.Fatal(err)
	}
	store.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP TABLE scans; CREATE TABLE scans (
		id VARCHAR PRIMARY KEY, target VARCHAR NOT NULL, mode VARCHAR NOT NULL,
		verify BOOLEAN, sniper BOOLEAN NOT NULL, status VARCHAR NOT NULL,
		progress VARCHAR NOT NULL, error VARCHAR NOT NULL, scan_json TEXT NOT NULL,
		created_at VARCHAR NOT NULL, updated_at VARCHAR NOT NULL);
		CREATE INDEX idx_scans_created ON scans(created_at DESC)`); err != nil {
		t.Fatal(err)
	}
	var scans []*scanpb.Scan
	for i, options := range []*scanpb.ScanOptions{nil, {}, {Verify: proto.Bool(false)}, {Verify: proto.Bool(true), Sniper: true}} {
		scan := &scanpb.Scan{Id: string(rune('a' + i)), Target: "中文.example", Options: options, Status: scanpb.ScanStatus(73), Progress: "canonical"}
		if i != 0 {
			scan.CreatedAt = timestamppb.New(time.Date(2026, 9, 30, 7, 1, i, 123456789, time.UTC))
		}
		raw, err := protojson.Marshal(scan)
		if err != nil {
			t.Fatal(err)
		}
		// Stale relational projections must be replaced from the old read source.
		if _, err := db.Exec(`INSERT INTO scans VALUES (?, 'stale', 'stale', true, false, 'queued', 'stale', '', ?, 'stale', 'stale')`, scan.Id, string(raw)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO session_scans VALUES ('session', ?)`, scan.Id); err != nil {
			t.Fatal(err)
		}
		scans = append(scans, scan)
	}
	return path, scans
}

func TestMigrationPreservesCanonicalScansLinksAndIndexes(t *testing.T) {
	path, scans := legacyScans(t)
	if store, err := service.NewSQLiteStore(path, service.ScanSchema); err == nil {
		store.Close()
		t.Fatal("server accepted legacy storage")
	} else if !strings.Contains(err.Error(), "migrate-scans") {
		t.Fatal(err)
	}
	count, err := migrateScans(t.Context(), path)
	if err != nil || count != int64(len(scans)) {
		t.Fatalf("migration: %d, %v", count, err)
	}
	store, err := service.NewSQLiteStore(path, service.ScanSchema)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, want := range scans {
		got, err := store.Get(t.Context(), want.Id)
		if err != nil || !proto.Equal(got, want) {
			t.Fatalf("scan %s: %v, %v", want.Id, got, err)
		}
		linked, err := store.ScanSessionIDs(t.Context(), want.Id)
		if err != nil || len(linked) != 1 || linked[0] != "session" {
			t.Fatalf("links: %v, %v", linked, err)
		}
	}
	if err := store.Delete(t.Context(), scans[0].Id); err != nil {
		t.Fatal(err)
	}
	linked, err := store.ScanSessionIDs(t.Context(), scans[0].Id)
	if err != nil || len(linked) != 0 {
		t.Fatalf("foreign-key cascade lost: %v, %v", linked, err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var indexes int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='idx_scans_created'`).Scan(&indexes); err != nil || indexes != 1 {
		t.Fatalf("indexes: %d, %v", indexes, err)
	}
	if _, err := migrateScans(t.Context(), path); err == nil {
		t.Fatal("converted twice")
	}
}

func TestMigrationFailureRollsBackSchemaAndRows(t *testing.T) {
	for _, statement := range []string{
		`UPDATE scans SET scan_json='{invalid' WHERE id='d'`,
		`UPDATE scans SET scan_json='{"id":"different"}' WHERE id='d'`,
		`CREATE INDEX custom_json ON scans(scan_json)`,
		`CREATE TRIGGER custom_scan AFTER UPDATE ON scans BEGIN SELECT 1; END`,
	} {
		t.Run(statement, func(t *testing.T) {
			path, _ := legacyScans(t)
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(statement); err != nil {
				t.Fatal(err)
			}
			if _, err := migrateScans(t.Context(), path); err == nil {
				t.Fatal("accepted unsupported database")
			}
			var extra int
			if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('scans') WHERE name='has_options'`).Scan(&extra); err != nil || extra != 0 {
				t.Fatalf("schema partially converted: %d, %v", extra, err)
			}
			var target, raw string
			if err := db.QueryRow(`SELECT target, scan_json FROM scans WHERE id='a'`).Scan(&target, &raw); err != nil || target != "stale" {
				t.Fatalf("rows partially converted: %q, %v", target, err)
			}
		})
	}
}
