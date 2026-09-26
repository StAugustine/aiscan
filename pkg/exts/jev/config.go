package jev

import (
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/chainreactors/cyber/core/guardrail"
	"github.com/chainreactors/cyber/core/resource"
	cfg "github.com/chainreactors/cyber/pkg/config"
)

const ConfigKey = "jev"
const endpoint = "https://api.typesafe.ai/v1/systemone"
const defaultModel = "jev-1.13.0"

type Config struct {
	Enabled  bool              `config:"enabled" json:"enabled" description:"Enable JEV tool admission checks"`
	APIKey   string            `config:"api_key" json:"api_key" description:"TypeSafe API key (or TYPESAFE_API_KEY)"`
	Model    string            `config:"model" json:"model"`
	Level    string            `config:"level" json:"level" description:"permissive, standard, or strict"`
	Timeout  string            `config:"timeout" json:"timeout" description:"Total request budget including retries"`
	OnError  string            `config:"on_error" json:"on_error" description:"record, review, or block on provider failure"`
	Criteria map[string]string `config:"criteria" json:"criteria" description:"Operator overrides for record/review/block criteria"`
}

func defaults(c Config) Config {
	if c.Model == "" {
		c.Model = defaultModel
	}
	if c.Level == "" {
		c.Level = "standard"
	}
	if c.Timeout == "" {
		c.Timeout = "10s"
	}
	if c.OnError == "" {
		c.OnError = "block"
	}
	c.Criteria = maps.Clone(c.Criteria)
	return c
}

func (c Config) validate() error {
	c = defaults(c)
	if _, ok := presets[c.Level]; !ok {
		return fmt.Errorf("jev level must be permissive, standard, or strict")
	}
	if action(c.OnError) == guardrail.Action_ACTION_UNSPECIFIED {
		return fmt.Errorf("jev on_error must be record, review, or block")
	}
	if d, err := time.ParseDuration(c.Timeout); err != nil || d <= 0 {
		return fmt.Errorf("jev timeout must be a positive duration")
	}
	for k, v := range c.Criteria {
		if action(k) == guardrail.Action_ACTION_UNSPECIFIED || strings.TrimSpace(v) == "" {
			return fmt.Errorf("jev criteria require nonempty record/review/block entries")
		}
	}
	return nil
}

func Declare(resources *resource.Registry) error {
	_, err := resource.Add[cfg.Section](resources, cfg.Section{
		Key: ConfigKey, New: func() any { c := defaults(Config{}); return &c }, Secrets: []string{"api_key"},
		Validate: func(v any) error { return v.(*Config).validate() },
		Environment: func(s cfg.Sources) (map[string]any, map[string]any, error) {
			if value, ok := s.LookupEnv("TYPESAFE_API_KEY"); ok && strings.TrimSpace(value) != "" {
				return nil, map[string]any{"api_key": value}, nil
			}
			return nil, nil, nil
		},
	})
	if err != nil {
		return err
	}
	_, err = resource.Add[cfg.Connection](resources, cfg.Connection{Section: ConfigKey, Test: testConnection})
	return err
}

func action(value string) guardrail.Action {
	switch value {
	case "record":
		return guardrail.Action_ACTION_RECORD
	case "review":
		return guardrail.Action_ACTION_REVIEW
	case "block":
		return guardrail.Action_ACTION_BLOCK
	default:
		return guardrail.Action_ACTION_UNSPECIFIED
	}
}

var presets = map[string]map[string]string{
	"permissive": {
		"record": "Low-risk or reversible work within clearly established authorization.",
		"review": "Possible business impact, intensive activity, or uncertain effects or authorization.",
		"block":  "Destructive activity, actions outside authorized scope, credential/data exfiltration, or explicit harm.",
	},
	"standard": {
		"record": "Local analysis and reporting, passive inspection, or low-rate probes of explicitly authorized targets with no target changes.",
		"review": "Target changes, high intensity scans, exploit execution, possible service impact, or uncertain effects or authorization.",
		"block":  "Destruction, persistence, denial of service, actions outside authorized scope, data leakage, or explicit harm.",
	},
	"strict": {
		"record": "Passive inspection and low-risk local analysis or reporting only.",
		"review": "Active probes of explicitly authorized targets without target changes.",
		"block":  "Target changes, unknown effects or authorization, destructive activity, data leakage, or possible service harm.",
	},
}
