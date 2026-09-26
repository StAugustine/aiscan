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
	Enabled  bool              `config:"enabled" json:"enabled" description:"Legacy activation flag; configuring an API key always enables JEV"`
	APIKey   string            `config:"api_key" json:"api_key" description:"TypeSafe API key (or TYPESAFE_API_KEY)"`
	Model    string            `config:"model" json:"model"`
	Level    string            `config:"level" json:"level" description:"permissive, standard, or strict"`
	Timeout  string            `config:"timeout" json:"timeout" description:"Total request budget including retries"`
	OnError  string            `config:"on_error" json:"on_error" description:"review or block on screening failure; automatic consequence assessment always fails closed"`
	Criteria map[string]string `config:"criteria" json:"criteria" description:"Stage 1 risk-screen overrides for record/review/block criteria"`
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
		"record": "Read-only work or low-rate authorized observation with no writes, state changes, destructive effects or disclosure.",
		"review": "Any local or remote write, state change or other side effect, even if bounded or reversible; possible business impact, intensive activity, or uncertain effects or authorization.",
		"block":  "Destructive activity, actions outside authorized scope, credential/data exfiltration, or explicit harm.",
	},
	"standard": {
		"record": "Read-only local analysis, output to stdout, passive inspection, or low-rate probes of explicitly authorized targets with no writes or state changes.",
		"review": "Any local or remote write or state change, even if reversible; high intensity scans, exploit execution, possible service impact, or uncertain effects or authorization.",
		"block":  "Destruction, persistence, denial of service, actions outside authorized scope, data leakage, or explicit harm.",
	},
	"strict": {
		"record": "Passive inspection and read-only local analysis without writes or state changes.",
		"review": "Local writes or other reversible local side effects; active probes of explicitly authorized targets without target changes.",
		"block":  "Target changes, unknown effects or authorization, destructive activity, data leakage, or possible service harm.",
	},
}

// Separate from operator screening rules: flagging a class of operations does
// not predetermine the consequences of one concrete invocation.
var consequenceCriteria = map[string]string{
	"record": "The actual invocation is demonstrably harmless: no destructive, irreversible, disruptive, out-of-scope or disclosure consequences. Any local side effects are bounded and reversible with no enterprise impact.",
	"review": "Actual consequences remain uncertain: affected targets, authorization, reversibility, data sensitivity or service impact cannot be established from the invocation. Do not execute.",
	"block":  "The actual invocation would destroy or irreversibly alter data, disrupt services, expose secrets, persist access, act outside established authorization, or otherwise cause harm. Do not execute.",
}
