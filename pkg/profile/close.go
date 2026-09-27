package profile

import (
	"context"
	"errors"
	"time"
)

// CloseOnce gives the synchronous host lifecycle a finalizer usable both as a
// deferred fallback and before rendering a task result. Cleanup survives task
// cancellation, has a bounded deadline, and contributes its error only once.
func CloseOnce(ctx context.Context, close func(context.Context) error) func(error) error {
	closed := false
	return func(err error) error {
		if closed {
			return err
		}
		closed = true
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return errors.Join(err, close(ctx))
	}
}
