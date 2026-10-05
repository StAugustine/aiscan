package jev

import (
	"strings"
	"time"
)

func compilationErrorStage(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	for _, stage := range []string{"reader", "observe syntax", "arguments", "native schema", "format", "review", "coverage"} {
		if strings.Contains(message, stage) {
			return stage
		}
	}
	return "provider_or_output"
}

type compileAttempt struct {
	active   bool
	failures int
	retryAt  time.Time
}

func (e *Extension) beginCompilation(key string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.compiling == nil {
		e.compiling = map[string]compileAttempt{}
	}
	previous := e.compiling[key]
	if previous.active || time.Now().Before(previous.retryAt) {
		return false
	}
	previous.active = true
	e.compiling[key] = previous
	return true
}

func (e *Extension) endCompilation(key string, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err == nil {
		delete(e.compiling, key)
		return
	}
	previous := e.compiling[key]
	previous.active = false
	previous.failures++
	delay := 30 * time.Second
	if previous.failures == 2 {
		delay = 2 * time.Minute
	} else if previous.failures > 2 {
		delay = 10 * time.Minute
	}
	previous.retryAt = time.Now().Add(delay)
	e.compiling[key] = previous
}
