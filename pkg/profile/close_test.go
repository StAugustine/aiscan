package profile

import (
	"context"
	"errors"
	"testing"
)

func TestCloseOncePreservesErrorsAndSurvivesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	runErr, closeErr := errors.New("run failed"), errors.New("close failed")
	calls := 0
	finish := CloseOnce(ctx, func(ctx context.Context) error {
		calls++
		if ctx.Err() != nil {
			t.Error("cleanup canceled with task")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("unbounded cleanup")
		}
		return closeErr
	})
	err := finish(runErr)
	if !errors.Is(err, runErr) || !errors.Is(err, closeErr) {
		t.Fatal(err)
	}
	if second := finish(err); calls != 1 || second != err {
		t.Fatalf("closed twice: %v %d", second, calls)
	}
}
