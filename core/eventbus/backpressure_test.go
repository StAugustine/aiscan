package eventbus

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBackpressureBoundsPendingAndPreservesOrder(t *testing.T) {
	for _, byBytes := range []bool{false, true} {
		t.Run(map[bool]string{false: "count", true: "bytes"}[byBytes], func(t *testing.T) {
			b := New[string]()
			gate, entered, emitted := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var got []string
			opts := SubscribeOptions[string]{Buffer: 1, BlockOnOverflow: true}
			if byBytes {
				opts.Buffer = 8
				opts.MaxBytes = 2
				opts.Size = func(v string) int64 { return int64(len(v)) }
			}
			s, err := b.SubscribeAsync(opts, func(v string) error {
				if v == "aa" {
					close(entered)
					<-gate
				}
				got = append(got, v)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			b.Emit("aa")
			waitSubscription(t, entered)
			go func() { b.Emit("bb"); close(emitted) }()
			select {
			case <-emitted:
				t.Error("overflow did not block")
			case <-time.After(30 * time.Millisecond):
			}
			s.mu.Lock()
			if s.pending != 1 || s.bytes > 2 {
				t.Errorf("pending=%d bytes=%d", s.pending, s.bytes)
			}
			s.mu.Unlock()
			close(gate)
			waitSubscription(t, emitted)
			if err := s.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, ",") != "aa,bb" || s.Err() != nil || s.Dropped() != 0 {
				t.Fatalf("got=%v err=%v dropped=%d", got, s.Err(), s.Dropped())
			}
		})
	}
}

func TestBackpressureStopUnblocksPublisher(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "close", true: "cancel"}[cancel], func(t *testing.T) {
			b := New[int]()
			gate, entered, emitted := make(chan struct{}), make(chan struct{}), make(chan struct{})
			s, err := b.SubscribeAsync(SubscribeOptions[int]{Buffer: 1, BlockOnOverflow: true}, func(int) error { close(entered); <-gate; return nil })
			if err != nil {
				t.Fatal(err)
			}
			b.Emit(1)
			waitSubscription(t, entered)
			go func() { b.Emit(2); close(emitted) }()
			select {
			case <-emitted:
				t.Error("publisher was not blocked")
			case <-time.After(30 * time.Millisecond):
			}
			if cancel {
				s.Cancel()
			} else {
				ctx, stop := context.WithCancel(t.Context())
				stop()
				if err := s.Close(ctx); !errors.Is(err, context.Canceled) {
					t.Errorf("close=%v", err)
				}
			}
			waitSubscription(t, emitted)
			close(gate)
			waitSubscription(t, s.Done())
		})
	}
}

func TestBackpressureRejectsOversizedEventWithoutWaiting(t *testing.T) {
	b := New[string]()
	s, err := b.SubscribeAsync(SubscribeOptions[string]{Buffer: 1, MaxBytes: 1, Size: func(v string) int64 { return int64(len(v)) }, BlockOnOverflow: true}, func(string) error { t.Error("oversized event admitted"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	emitted := make(chan struct{})
	go func() { b.Emit("too large"); close(emitted) }()
	waitSubscription(t, emitted)
	waitSubscription(t, s.Done())
	if !errors.Is(s.Err(), ErrOverflow) || s.Dropped() != 1 {
		t.Fatalf("err=%v dropped=%d", s.Err(), s.Dropped())
	}
}
