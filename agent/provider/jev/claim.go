package jev

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"

	"github.com/chainreactors/cyber/core/decision"
)

type ClaimType = decision.ClaimType

const (
	ClaimChoice = decision.ClaimType_choice
	ClaimScore  = decision.ClaimType_score
	ClaimNoul   = decision.ClaimType_noul
)

// Claim is a semantic judgment. Context supplies facts and option meanings;
// ordered options define the closed choice or score scale. Noul has no options.
// Persistence, evidence, compilation and execution are owned by consumers.
type Claim struct {
	Type    ClaimType `json:"type"`
	Context string    `json:"context"`
	Options []string  `json:"options,omitempty"`
}

func (c Claim) Validate() error {
	if strings.TrimSpace(c.Context) == "" || len(c.Context) > 64<<10 {
		return errors.New("invalid Claim context")
	}
	switch c.Type {
	case ClaimChoice:
		if len(c.Options) < 2 || len(c.Options) > 64 {
			return errors.New("choice Claim needs two to sixty-four options")
		}
	case ClaimScore:
		if len(c.Options) < 2 || len(c.Options) > 10 {
			return errors.New("score Claim needs two to ten ordered levels")
		}
	case ClaimNoul:
		if len(c.Options) != 0 {
			return errors.New("noul Claim cannot have options")
		}
	default:
		return errors.New("unsupported Claim type")
	}
	seen := map[string]bool{}
	for _, option := range c.Options {
		if strings.TrimSpace(option) == "" || len(option) > 1024 || seen[option] {
			return errors.New("invalid or duplicate Claim option")
		}
		seen[option] = true
	}
	return nil
}

func (c Claim) Description() string {
	if len(c.Options) == 0 {
		return c.Type.String() + ": " + c.Context
	}
	return c.Type.String() + ": " + c.Context + "\nOptions (in order):\n" + strings.Join(c.Options, "\n")
}

func (c Claim) Proto() *decision.Claim {
	return &decision.Claim{Type: c.Type, Context: c.Context, Options: slices.Clone(c.Options)}
}

func (c Claim) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type    string   `json:"type"`
		Context string   `json:"context"`
		Options []string `json:"options,omitempty"`
	}{c.Type.String(), c.Context, c.Options})
}

func (c *Claim) UnmarshalJSON(data []byte) error {
	var value struct {
		Type    string   `json:"type"`
		Context string   `json:"context"`
		Options []string `json:"options,omitempty"`
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&value); err != nil {
		return err
	}
	if d.Decode(new(json.RawMessage)) != io.EOF {
		return errors.New("extra Claim data")
	}
	kind, ok := decision.ClaimType_value[value.Type]
	if !ok || kind == 0 {
		return errors.New("unsupported Claim type")
	}
	claim := Claim{Type: ClaimType(kind), Context: value.Context, Options: value.Options}
	if err := claim.Validate(); err != nil {
		return err
	}
	*c = claim
	return nil
}

// Evaluation is a closed result union; only one of choice, score or noul exists.
type Evaluation = decision.Evaluation

func (c Claim) Choice(e *Evaluation) (string, error) {
	if e != nil && c.Type == ClaimChoice {
		if v, ok := e.Value.(*decision.Evaluation_Choice); ok && slices.Contains(c.Options, v.Choice) {
			return v.Choice, nil
		}
	}
	return "", errors.New("invalid JEV choice binding")
}
func (c Claim) Score(e *Evaluation) (float64, error) {
	if e != nil && c.Type == ClaimScore {
		if v, ok := e.Value.(*decision.Evaluation_Score); ok {
			return boundedNumber(v.Score, float64(len(c.Options)-1), "score")
		}
	}
	return 0, errors.New("invalid JEV score response")
}
func (c Claim) Noul(e *Evaluation) (float64, error) {
	if e != nil && c.Type == ClaimNoul {
		if v, ok := e.Value.(*decision.Evaluation_Noul); ok {
			return boundedNumber(v.Noul, 1, "noul")
		}
	}
	return 0, errors.New("invalid JEV noul response")
}
func boundedNumber(value, upper float64, kind string) (float64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > upper {
		return 0, fmt.Errorf("invalid JEV %s response", kind)
	}
	return value, nil
}
