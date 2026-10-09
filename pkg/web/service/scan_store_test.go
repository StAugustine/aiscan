package service

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	scanpb "github.com/chainreactors/cyber/pkg/web/scan"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRelationalScansPreservePresenceEnumsAndTimestamps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scans.db")
	store, err := NewSQLiteStore(path, ScanSchema)
	if err != nil {
		t.Fatal(err)
	}
	var scans []*scanpb.Scan
	for i, options := range []*scanpb.ScanOptions{nil, {}, {Verify: proto.Bool(false)}, {Verify: proto.Bool(true)}, {Sniper: true}} {
		scan := &scanpb.Scan{Id: fmt.Sprintf("scan-%d", i), Target: "中文.example", Mode: "full", Options: options, Status: scanpb.ScanStatus(73), Progress: "保留进度", Error: "原错误"}
		if i != 0 {
			scan.CreatedAt = timestamppb.New(time.Date(2026, 9, 30, 8, 0, i, 987654321, time.UTC))
			scan.UpdatedAt = timestamppb.New(time.Date(2026, 9, 30, 8, 1, i, 123456789, time.UTC))
		}
		if err := store.Create(t.Context(), scan); err != nil {
			t.Fatal(err)
		}
		scans = append(scans, scan)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = NewSQLiteStore(path, ScanSchema)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, scan := range scans {
		got, err := store.Get(t.Context(), scan.Id)
		if err != nil || !proto.Equal(scan, got) {
			t.Fatalf("read %s: %v, %v", scan.Id, got, err)
		}
		scan.Options = nil
		scan.Status = scanpb.ScanStatus_SCAN_STATUS_COMPLETED
		changed, err := store.TransitionScan(t.Context(), scan, scanpb.ScanStatus(73))
		if err != nil || !changed {
			t.Fatalf("transition: %v, %v", changed, err)
		}
		got, err = store.Get(t.Context(), scan.Id)
		if err != nil || !proto.Equal(scan, got) {
			t.Fatalf("update %s: %v, %v", scan.Id, got, err)
		}
		changed, err = store.TransitionScan(t.Context(), scan, scanpb.ScanStatus(73))
		if err != nil || changed {
			t.Fatalf("stale transition: %v, %v", changed, err)
		}
	}
	listed, err := store.List(t.Context(), 10)
	if err != nil || len(listed) != len(scans) {
		t.Fatalf("list: %v, %v", listed, err)
	}
	for i, got := range listed {
		if !proto.Equal(got, scans[len(scans)-1-i]) {
			t.Fatalf("list order/payload: %v", listed)
		}
	}
}

func TestRelationalScanRejectsInvalidValuesBeforeWriting(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "scans.db"), ScanSchema)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, scan := range []*scanpb.Scan{
		{Id: "invalid-utf8", Target: string([]byte{0xff})},
		{Id: "invalid-time", CreatedAt: &timestamppb.Timestamp{Nanos: -1}},
	} {
		if err := store.Create(t.Context(), scan); err == nil {
			t.Fatalf("accepted invalid scan %s", scan.Id)
		}
	}
	listed, err := store.List(t.Context(), 10)
	if err != nil || len(listed) != 0 {
		t.Fatalf("partial writes: %v, %v", listed, err)
	}
}
