package jev

import (
	"errors"
	"strings"
)

const (
	maxClaims   = 32
	maxReflexes = 16
)

// Claim declares a one-shot finite judgment, never the answer to a past task.
type Claim struct {
	When     string            `json:"when"`
	Question string            `json:"question"`
	Options  map[string]string `json:"options"`
}

// Reflex defines a reusable scene. Its action space is bound from Sources on
// every observation, so it contains neither a recorded path nor executable code.
type Reflex struct {
	When    string   `json:"when"`
	Decide  string   `json:"decide"`
	Sources []string `json:"sources"`
}

type claimRecord struct {
	Claim
	Task     string `json:"task"`
	Consumed bool   `json:"consumed"`
}
type reflexRecord struct {
	Reflex
	Claims []string `json:"claims"`
}
type library struct {
	Version  int                     `json:"version"`
	Claims   map[string]claimRecord  `json:"claims"`
	Reflexes map[string]reflexRecord `json:"reflexes"`
	Compiled map[string]bool         `json:"compiled"`
}

func (c Claim) validate() error {
	if strings.TrimSpace(c.When) == "" || strings.TrimSpace(c.Question) == "" || len(c.When)+len(c.Question) > 4096 || len(c.Options) < 2 || len(c.Options) > 16 || strings.TrimSpace(c.Options[Defer]) == "" {
		return errors.New("invalid Claim decision space")
	}
	for id, option := range c.Options {
		if strings.TrimSpace(id) == "" || len(id) > 64 || strings.TrimSpace(option) == "" || len(option) > 1024 {
			return errors.New("invalid Claim option")
		}
	}
	return nil
}
func (r Reflex) validate() error {
	if strings.TrimSpace(r.When) == "" || strings.TrimSpace(r.Decide) == "" || len(r.When)+len(r.Decide) > 8192 || len(r.Sources) == 0 || len(r.Sources) > 8 {
		return errors.New("invalid Reflex scene")
	}
	seen := map[string]bool{}
	for _, source := range r.Sources {
		if source == "" || strings.ContainsAny(source, " /\\\t\r\n") || seen[source] {
			return errors.New("invalid Reflex source")
		}
		seen[source] = true
	}
	return nil
}
