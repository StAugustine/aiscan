package console

import (
	"io"
	"sync"
)

// errorWriter retains failures from renderers whose presentation methods cannot
// return errors. It is safe for the live status writer and the task goroutine.
type errorWriter struct {
	mu     sync.Mutex
	writer io.Writer
	err    error
}

func (w *errorWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.writer.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	w.err = err
	return n, err
}

func (w *errorWriter) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}
