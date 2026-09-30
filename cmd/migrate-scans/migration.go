package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"time"

	scanpb "github.com/chainreactors/cyber/pkg/web/scan"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
	_ "modernc.org/sqlite"
)

func migrateScans(ctx context.Context, path string) (int64, error) {
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
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(scans)`)
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
	legacy := []string{"id", "target", "mode", "verify", "sniper", "status", "progress", "error", "scan_json", "created_at", "updated_at"}
	if !slices.Equal(columns, legacy) {
		return 0, fmt.Errorf("expected the scan_json schema, found columns %v; database was not changed", columns)
	}
	var triggers int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE tbl_name = 'scans' AND type = 'trigger'`).Scan(&triggers); err != nil {
		return 0, err
	}
	if triggers != 0 {
		return 0, fmt.Errorf("custom scan-table triggers require an explicit conversion; database was not changed")
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE scans ADD COLUMN has_options BOOLEAN NOT NULL DEFAULT false`); err != nil {
		return 0, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT id, scan_json FROM scans ORDER BY id`)
	if err != nil {
		return 0, err
	}
	var count int64
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return 0, err
		}
		scan := new(scanpb.Scan)
		if err := protojson.Unmarshal([]byte(raw), scan); err != nil {
			rows.Close()
			return 0, fmt.Errorf("decode scan %q: %w; transaction rolled back", id, err)
		}
		if scan.Id != id {
			rows.Close()
			return 0, fmt.Errorf("scan %q has mismatched JSON identity %q; transaction rolled back", id, scan.Id)
		}
		var verify *bool
		if scan.Options != nil {
			verify = scan.Options.Verify
		}
		// JSON was the legacy read path. Rebuild all fields from that source,
		// including presence and timestamps, before removing it permanently.
		_, err = tx.ExecContext(ctx, `UPDATE scans SET target=?, mode=?, verify=?, sniper=?, status=?, progress=?, error=?, created_at=?, updated_at=?, has_options=? WHERE id=?`,
			scan.Target, scan.Mode, verify, scan.GetOptions().GetSniper(), int32(scan.Status), scan.Progress, scan.Error,
			storedTime(scan.CreatedAt), storedTime(scan.UpdatedAt), scan.Options != nil, id)
		if err != nil {
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
	// SQLite retains unrelated indexes and foreign keys and rejects any object
	// that still depends on this column. Such failures roll back the whole change.
	if _, err := tx.ExecContext(ctx, `ALTER TABLE scans DROP COLUMN scan_json`); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

func storedTime(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return ""
	}
	return ts.AsTime().UTC().Format(time.RFC3339Nano)
}
