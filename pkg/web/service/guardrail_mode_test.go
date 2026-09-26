package service

import (
	"context"
	"errors"
	"github.com/chainreactors/cyber/core/types"
	"github.com/chainreactors/cyber/pkg/profile"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"testing"
)

type modeProfile struct {
	profile.Profile
	mode string
}

func (p *modeProfile) SetGuardrailMode(mode string) error { p.mode = mode; return nil }

func TestGuardrailModeSavePreservesProfileAndWork(t *testing.T) {
	for _, fail := range []bool{false, true} {
		current, _, closed := newRecordingProfile(t)
		target := &modeProfile{Profile: current, mode: "safe"}
		values, _ := structpb.NewStruct(map[string]any{"mode": "safe"})
		config := &types.DistributeConfig{Extensions: map[string]*structpb.Struct{"guardrail": values}}
		store := &transactionalConfigStore{cfg: config}
		if fail {
			store.commitErr = errors.New("disk full")
		}
		svc := NewService(ServiceConfig{Profile: target, ConfigStore: store,
			BuildProfile: func(context.Context, *PreparedConfig) (profile.Profile, error) {
				t.Fatal("mode edit rebuilt profile")
				return nil, nil
			}})
		work, admitted := svc.beginWork()
		if !admitted {
			t.Fatal("work rejected")
		}
		next := proto.CloneOf(config)
		next.Extensions["guardrail"].Fields["mode"] = structpb.NewStringValue("auto")
		_, err := svc.SaveConfig(t.Context(), next)
		if (err != nil) != fail {
			t.Fatalf("save error = %v", err)
		}
		want := "auto"
		if fail {
			want = "safe"
		}
		if target.mode != want || closed() || work.Err() != nil {
			t.Fatal("mode save disrupted work or changed mode after failed commit")
		}
		svc.work.Done()
		if err := svc.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}
