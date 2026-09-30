package jev

import (
	"fmt"
	"maps"
	"strings"
	"time"

	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"github.com/chainreactors/cyber/core/resource"
	cfg "github.com/chainreactors/cyber/pkg/config"
)

const ConfigKey = "jev"

type Config struct {
	Enabled   bool              `config:"enabled" json:"enabled" description:"Legacy risk-screen activation; acceleration requires mode auto"`
	APIKey    string            `config:"api_key" json:"api_key" description:"TypeSafe API key (or TYPESAFE_API_KEY)"`
	Model     string            `config:"model" json:"model"`
	Level     string            `config:"level" json:"level" description:"permissive, standard, or strict"`
	Timeout   string            `config:"timeout" json:"timeout" description:"Total request budget including retries"`
	OnError   string            `config:"on_error" json:"on_error" description:"review or block on screening failure; automatic consequence assessment always fails closed"`
	Criteria  map[string]string `config:"criteria" json:"criteria" description:"Stage 1 risk-screen overrides for record/review/block criteria"`
	Mode      string            `config:"mode" json:"mode" description:"Optional accelerator: off (default), auto"`
	Directory string            `config:"directory" json:"directory" description:"Claim/Reflex library and execution evidence directory; default .cyber/jev"`
}

func defaults(c Config) Config {
	if c.Mode == "" {
		c.Mode = "off"
	}
	if c.Model == "" {
		c.Model = jevapi.DefaultModel
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
	if c.Mode != "off" && c.Mode != "auto" {
		return fmt.Errorf("jev mode must be off or auto")
	}
	if c.Level != "standard" && c.Level != "strict" && c.Level != "permissive" {
		return fmt.Errorf("invalid legacy jev level")
	}
	validAction := func(v string) bool { return v == "record" || v == "review" || v == "block" }
	if !validAction(c.OnError) {
		return fmt.Errorf("invalid legacy jev on_error")
	}
	for k, v := range c.Criteria {
		if !validAction(k) || strings.TrimSpace(v) == "" {
			return fmt.Errorf("invalid legacy jev criteria")
		}
	}
	if d, err := time.ParseDuration(c.Timeout); err != nil || d <= 0 {
		return fmt.Errorf("jev timeout must be positive")
	}
	return nil
}

var configSection = cfg.Section{
	Key: ConfigKey, New: func() any { c := defaults(Config{}); return &c }, Secrets: []string{"api_key"},
	Validate: func(v any) error { return v.(*Config).validate() },
	Environment: func(s cfg.Sources) (map[string]any, map[string]any, error) {
		if value, ok := s.LookupEnv("TYPESAFE_API_KEY"); ok && strings.TrimSpace(value) != "" {
			return nil, map[string]any{"api_key": value}, nil
		}
		return nil, nil, nil
	},
}

func Declare(resources *resource.Registry) error {
	_, err := resource.Add[cfg.Section](resources, configSection)
	if err != nil {
		return err
	}
	_, err = resource.Add[cfg.Connection](resources, cfg.Connection{Section: ConfigKey, Test: testConnection})
	return err
}
