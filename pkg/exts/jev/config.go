package jev

import (
	"fmt"
	"strings"
	"time"

	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"github.com/chainreactors/cyber/core/resource"
	cfg "github.com/chainreactors/cyber/pkg/config"
)

const ConfigKey = "jev"

type Config struct {
	APIKey            string `config:"api_key" json:"api_key" description:"TypeSafe API key (or TYPESAFE_API_KEY)"`
	Model             string `config:"model" json:"model"`
	Timeout           string `config:"timeout" json:"timeout" description:"Total request budget including retries"`
	Mode              string `config:"mode" json:"mode" description:"Optional accelerator: off (default), auto"`
	Directory         string `config:"directory" json:"directory" description:"Claim/Reflex library and execution evidence directory; default .cyber/jev"`
	DeclarationEffort string `config:"declaration_effort" json:"declaration_effort,omitempty" description:"Optional provider reasoning effort for background Claim/Compile; empty uses provider default"`
}

func defaults(c Config) Config {
	if c.Mode == "" {
		c.Mode = "off"
	}
	if c.Model == "" {
		c.Model = jevapi.DefaultModel
	}
	if c.Timeout == "" {
		c.Timeout = "10s"
	}
	return c
}

func (c Config) validate() error {
	c = defaults(c)
	if c.Mode != "off" && c.Mode != "auto" {
		return fmt.Errorf("jev mode must be off or auto")
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
