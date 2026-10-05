package tool

import (
	"context"
	"testing"
)

func TestNativeContractContributionLifetime(t *testing.T) {
	r := NewNativeContractRegistry()
	c := NativeContract{ID: "native", Version: "1", Classify: func(NativeCall) (NativeAccess, error) { return NativeRead, nil }}
	h, err := r.Add(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Add(NativeContract{ID: "other", Version: "1", Classify: c.Classify}, c); err == nil {
		t.Fatal("duplicate batch accepted")
	}
	if len(r.Snapshot()) != 1 {
		t.Fatal("failed batch leaked a contract")
	}
	snapshot := r.Snapshot()
	delete(snapshot, c.ID)
	if len(r.Snapshot()) != 1 {
		t.Fatal("snapshot mutated registry")
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	next, err := r.Add(c)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close(context.Background())
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.Snapshot()) != 1 {
		t.Fatal("old handle removed the new owner")
	}
}

func TestNativeContractConflictingAccess(t *testing.T) {
	r := NewNativeContractRegistry()
	for _, access := range []NativeAccess{NativeRead, NativeEffect} {
		c := NativeContract{ID: string(access), Version: "1", Classify: func(NativeCall) (NativeAccess, error) { return access, nil }}
		if err := r.Register(c); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Access(NativeCall{}); err == nil {
		t.Fatal("conflicting read/effect contracts accepted")
	}
}
