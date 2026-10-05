package jev

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/dop251/goja"
)

const (
	maxClaims      = 32
	maxReflexes    = 16
	maxSourceBytes = 8 << 10
)

// Claim records a reusable judgment for compilation, never an execution route.
type Claim struct {
	Text     string            `json:"text,omitempty"`
	When     string            `json:"when,omitempty"`
	Question string            `json:"question,omitempty"`
	Options  map[string]string `json:"options,omitempty"`
}

func (c *Claim) UnmarshalJSON(data []byte) error {
	var text string
	if len(data) > 0 && data[0] == '"' {
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		*c = Claim{Text: text}
		return nil
	}
	type plain Claim
	var value plain
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	*c = Claim(value)
	return nil
}

// Reflex is one runtime-generated JavaScript function. The historical field
// name Observe contains the whole executable function, not a second observer.
type Reflex struct {
	APIVersion  int                       `json:"api_version,omitempty"`
	Parameters  json.RawMessage           `json:"parameters_schema,omitempty"`
	Steps       map[string]StepDefinition `json:"steps,omitempty"`
	LegacySuite string                    `json:"suite,omitempty"` // Decode old libraries only; never admissible for execution.
	Proof       *VerificationRecord       `json:"verification,omitempty"`
	When        string                    `json:"when"`
	Decide      string                    `json:"decide"`
	Observe     string                    `json:"observe"`
	Readers     map[string]string         `json:"readers,omitempty"`
	program     *goja.Program
	arguments   map[string]any // Compilation evidence only; never persisted.
}

type claimRecord struct {
	Claim
	Task     string `json:"task"`
	Consumed bool   `json:"consumed"`
}

func (c *claimRecord) UnmarshalJSON(data []byte) error {
	type claimPlain Claim
	var value struct {
		claimPlain
		Task     string `json:"task"`
		Consumed bool   `json:"consumed"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*c = claimRecord{Claim: Claim(value.claimPlain), Task: value.Task, Consumed: value.Consumed}
	return nil
}

type reflexRecord struct {
	Reflex
	Claims    []string          `json:"claims"`
	Contracts map[string]string `json:"contracts,omitempty"`
	Blocker   string            `json:"blocker,omitempty"`
}
type library struct {
	Claims     map[string]claimRecord  `json:"claims"`
	Reflexes   map[string]reflexRecord `json:"reflexes"`
	Candidates map[string]reflexRecord `json:"candidates,omitempty"`
	Compiled   map[string]bool         `json:"compiled"` // Derived compatibility field, never an execution index.
}

func (c Claim) validate() error {
	if c.Text != "" {
		if strings.TrimSpace(c.Text) == "" || len(c.Text) > 8192 {
			return errors.New("invalid natural-language Claim")
		}
		return nil
	}
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

func (c Claim) description() string {
	if c.Text != "" {
		return c.Text
	}
	return strings.TrimSpace(c.When + "\n" + c.Question + "\n" + jsonText(c.Options))
}
func (r *Reflex) validate() error {
	if strings.TrimSpace(r.When) == "" || strings.TrimSpace(r.Decide) == "" || len(r.When)+len(r.Decide) > 8192 || strings.TrimSpace(r.Observe) == "" || len(r.Observe) > 16<<10 {
		return errors.New("invalid Reflex scene")
	}
	r.program = nil
	for id, source := range r.Readers {
		if strings.TrimSpace(id) == "" || len(id) > 64 {
			return errors.New("reader: invalid reader identifier")
		}
		if err := validateReader(id, source); err != nil {
			return err
		}
	}
	code := strings.TrimSpace(r.Observe)
	if !strings.HasPrefix(code, "js:") {
		return errors.New("Observe must be a js: JavaScript program")
	}
	var err error
	source := strings.TrimSpace(strings.TrimPrefix(code, "js:"))
	if err := validateReader("reflex", source); err != nil {
		return fmt.Errorf("reflex must be a function(context, arguments): %w", err)
	}
	r.program, err = goja.Compile("reflex", "("+source+")", true)
	if err != nil {
		return fmt.Errorf("observe syntax: %w", err)
	}
	return nil
}
