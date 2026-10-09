//go:build full

package browser

import (
	"context"
	"testing"

	"github.com/chainreactors/cyber/core/extension"
	coretool "github.com/chainreactors/cyber/core/tool"
	"github.com/chainreactors/cyber/pkg/testutil/hosttest"
)

func TestModuleOwnsBrowserRegistration(t *testing.T) {
	registry := coretool.NewCommandRegistry()
	instance, err := New(t.TempDir(), "default")
	if err != nil {
		t.Fatal(err)
	}
	var contracts *coretool.NativeContractRegistry
	probe := extension.Func{LoadFunc: func(scope *extension.Scope) error {
		var err error
		contracts, err = extension.Use[*coretool.NativeContractRegistry](scope)
		return err
	}}
	set := hosttest.Set(t,
		hosttest.Capabilities(),
		registry,
		instance,
		probe,
	)
	if err := set.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !registry.Has("playwright") {
		t.Fatal("browser command was not published")
	}
	if len(contracts.Snapshot()) != 1 {
		t.Fatal("browser native contract missing")
	}
	if err := set.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if registry.Has("playwright") {
		t.Fatal("browser command remained published")
	}
	if len(contracts.Snapshot()) != 0 {
		t.Fatal("browser native contract remained published")
	}
}

func TestFailedBrowserLoadRetractsNativeContract(t *testing.T) {
	registry := coretool.NewCommandRegistry()
	instance, err := New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	var contracts *coretool.NativeContractRegistry
	prior := extension.Func{LoadFunc: func(scope *extension.Scope) error {
		var err error
		contracts, err = extension.Use[*coretool.NativeContractRegistry](scope)
		if err != nil {
			return err
		}
		return extension.Add(scope, coretool.Command{Name: "playwright", Run: func(context.Context, *coretool.Execution) (any, error) { return nil, nil }})
	}}
	set := hosttest.Set(t, hosttest.Capabilities(), registry, prior, instance)
	if err := set.Load(t.Context()); err == nil {
		t.Fatal("duplicate browser command loaded")
	}
	if len(contracts.Snapshot()) != 0 {
		t.Fatal("failed browser load leaked a trusted capability")
	}
	if err := set.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
