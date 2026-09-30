// Package provider adapts the LLM provider State to Extension lifecycle. It is
// unrelated to declaration aggregation; CLI and Config register through typed
// resource Points directly.
package provider

import (
	"context"
	"fmt"
	"sync"

	"github.com/chainreactors/cyber/agent/provider"
	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/telemetry"
)

type Extension struct {
	mu      sync.Mutex
	release func()
	state   *provider.State
	config  provider.StartupConfig
	logger  telemetry.Logger
	closed  bool
}

func New(config provider.StartupConfig) *Extension {
	config.Fallbacks = append([]provider.ProviderConfig(nil), config.Fallbacks...)
	return &Extension{config: config}
}

// Load owns and publishes the provider state for this installation.
func (e *Extension) Load(scope *extension.Scope) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = false
	e.state = &provider.State{}
	logger, err := extension.Use[telemetry.Logger](scope)
	if err != nil {
		return err
	}
	e.logger = logger
	release, err := provider.Initialize(scope.Init(), e.state, e.config, logger)
	e.release = release
	if err != nil {
		return err
	}
	if err := extension.Provide[*provider.State](scope, e.state); err != nil {
		return err
	}
	return extension.Provide[provider.Controller](scope, e)
}

// Reload is the provider extension's live configuration boundary. Profiles
// may delegate to it, but session/runtime code must not initialize providers.
func (e *Extension) Reload(ctx context.Context, config provider.ProviderConfig) error {
	if e == nil {
		return fmt.Errorf("provider extension is unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state == nil || e.closed {
		return fmt.Errorf("provider extension is unavailable")
	}
	return e.state.Update(ctx, config, e.logger)
}
func (e *Extension) Close(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.release != nil {
		e.release()
		e.release = nil
	}
	e.closed = true
	return nil
}
