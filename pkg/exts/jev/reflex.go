package jev

import (
	"errors"
	"strings"

	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"github.com/dop251/goja"
)

const (
	maxClaims      = 32
	maxReflexes    = 16
	libraryVersion = 3
)

// Claim declares a one-shot finite judgment, never the answer to a past task.
type Claim struct {
	When     string            `json:"when"`
	Question string            `json:"question"`
	Options  map[string]string `json:"options"`
}

// Reflex owns a reusable finite scene. Observe is a pure, runtime-generated
// expression over native interaction data; tools require no observer callbacks.
type Reflex struct {
	When    string `json:"when"`
	Decide  string `json:"decide"`
	Observe string `json:"observe"`
	program *goja.Program
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

// native projects Claim semantics directly into JEV's native choice protocol.
func (c Claim) native() jevapi.Question {
	return jevapi.Question{Type: "choice", Instructions: c.Question, Criteria: c.Options}
}

// native projects a Reflex judgment with options bound from current facts.
func (r Reflex) native(options map[string]string) jevapi.Question {
	return (Claim{Question: decisionInstructions + r.Decide, Options: options}).native()
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
func (r *Reflex) validate() error {
	if strings.TrimSpace(r.When) == "" || strings.TrimSpace(r.Decide) == "" || len(r.When)+len(r.Decide) > 8192 || strings.TrimSpace(r.Observe) == "" || len(r.Observe) > 16<<10 {
		return errors.New("invalid Reflex scene")
	}
	r.program = nil
	code := strings.TrimSpace(r.Observe)
	if !strings.HasPrefix(code, "js:") {
		return errors.New("Observe must be a js: JavaScript program")
	}
	var err error
	r.program, err = goja.Compile("observe", strings.TrimSpace(strings.TrimPrefix(code, "js:")), true)
	return err
}
