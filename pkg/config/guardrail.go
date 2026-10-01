package config

import (
	"github.com/chainreactors/cyber/core/types"
	"google.golang.org/protobuf/proto"
)

// GuardrailModeChange recognizes only an interaction-mode edit. Policy,
// credentials and every other configuration field still use full validation.
func GuardrailModeChange(current, next *types.DistributeConfig) (string, bool) {
	if current == nil || next == nil {
		return "", false
	}
	section := next.GetExtensions()["guardrail"]
	mode := section.GetFields()["mode"].GetStringValue()
	if mode != "safe" && mode != "auto" {
		return "", false
	}
	left, right := proto.CloneOf(current), proto.CloneOf(next)
	for _, config := range []*types.DistributeConfig{left, right} {
		if values := config.GetExtensions()["guardrail"]; values != nil {
			delete(values.Fields, "mode")
			if len(values.Fields) == 0 {
				delete(config.Extensions, "guardrail")
			}
		}
	}
	return mode, proto.Equal(left, right)
}
