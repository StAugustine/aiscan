package jev

import (
	"encoding/json"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"github.com/chainreactors/cyber/core/resource"
	"github.com/chainreactors/cyber/core/types"
	cfg "github.com/chainreactors/cyber/pkg/config"
	"strings"
	"testing"
)

func TestEnvironmentKeyViewIsMetadataOnly(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "fixture-env-secret")
	view := &types.ConfigView{}
	ProjectView(view)
	ProjectView(view)
	entry := view.Extensions[ConfigKey]
	if len(entry.ConfiguredSecrets) != 1 || entry.ConfiguredSecrets[0] != "api_key" {
		t.Fatal("environment activation missing or duplicated")
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), "fixture-env-secret") || len(entry.Values.Fields) != 0 {
		t.Fatal("environment key entered editable configuration")
	}
}

func TestConfigurationUsesSecretAndEnvironment(t *testing.T) {
	resources := resource.New()
	sections := cfg.NewSections()
	if _, err := resource.Define[cfg.Section](resources, sections); err != nil {
		t.Fatal(err)
	}
	if _, err := resource.Define[cfg.Connection](resources, sections.ConnectionPoint()); err != nil {
		t.Fatal(err)
	}
	if err := Declare(resources); err != nil {
		t.Fatal(err)
	}
	resolved, err := sections.ResolveValues(cfg.Values{ConfigKey: {"enabled": true}}, nil, func(key string) (string, bool) { return "fixture-env-key", key == "TYPESAFE_API_KEY" })
	if err != nil {
		t.Fatal(err)
	}
	config, err := cfg.Get[*Config](resolved, ConfigKey)
	if err != nil || config.APIKey != "fixture-env-key" || config.Model != jevapi.DefaultModel || config.OnError != "block" {
		t.Fatalf("config failed: %v", err)
	}
	view, secrets := sections.View(resolved.Values())
	if _, present := view[ConfigKey]["api_key"]; present || len(secrets[ConfigKey]) != 1 {
		t.Fatal("configuration view exposed or lost secret metadata")
	}
	preserved := sections.Preserve(view, resolved.Values())
	if preserved[ConfigKey]["api_key"] != "fixture-env-key" {
		t.Fatal("configuration roundtrip lost secret")
	}
	for _, bad := range []Config{{Level: "unknown"}, {Timeout: "0s"}, {OnError: "allow"}, {Criteria: map[string]string{"allow": "bad"}}} {
		if bad.validate() == nil {
			t.Error("invalid configuration accepted")
		}
	}
}
