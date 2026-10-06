package jev

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"github.com/dop251/goja"
)

const (
	maxClaims      = 32
	maxReflexes    = 16
	maxSourceBytes = 8 << 10
	libraryFormat  = "claim/2"
)

// Claim describes a semantic judgment; evidence and execution belong to its consumers.
type Claim = jevapi.Claim

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
	Task string `json:"task"`
}

func (c *claimRecord) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	task := fields["task"]
	delete(fields, "task")
	claim, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	var value Claim
	if err = json.Unmarshal(claim, &value); err != nil {
		return err
	}
	var source string
	if len(task) != 0 {
		if err = json.Unmarshal(task, &source); err != nil {
			return err
		}
	}
	*c = claimRecord{Claim: value, Task: source}
	return nil
}

func (c claimRecord) MarshalJSON() ([]byte, error) {
	data, err := json.Marshal(c.Claim)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	fields["task"], err = json.Marshal(c.Task)
	if err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

type reflexRecord struct {
	Reflex
	Claims    []string          `json:"claims"`
	Contracts map[string]string `json:"contracts,omitempty"`
	Blocker   string            `json:"blocker,omitempty"`
}
type library struct {
	Format     string                  `json:"format"`
	Claims     map[string]claimRecord  `json:"claims"`
	Reflexes   map[string]reflexRecord `json:"reflexes"`
	Candidates map[string]reflexRecord `json:"candidates,omitempty"`
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
