package tui

import (
	"context"
	"fmt"
	"strings"

	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/app"
	"github.com/chainreactors/aiscan/skills"
)

// Session holds the dependencies commands need to operate on.
type Session struct {
	Ctx          context.Context
	Option       *cfg.Option
	App          *app.App
	Agent        *agent.Agent
	Controller   Controller
	EvalCriteria string
	OnEvalChange func(string)
}

// Controller is the async execution interface that tui implements.
type Controller interface {
	SubmitPrompt(label, displayText, prompt string) error
	Continue() error
	Stop() bool
	Running() bool
}

// Command describes a REPL command independent of any UI framework.
type Command struct {
	Name        string
	Aliases     []string
	Description string
	Args        ArgSpec
	Hidden      bool
	Run         func(ctx context.Context, s *Session, args []string) error
}

type ArgSpec int

const (
	ArgsNone ArgSpec = iota
	ArgsExact1
	ArgsOptional
)

// SkillCommands generates commands for each non-internal skill.
func SkillCommands(s *Session) []Command {
	if s.App == nil || s.App.Skills == nil {
		return nil
	}
	var cmds []Command
	for _, skill := range s.App.Skills.Skills {
		if strings.TrimSpace(skill.Name) == "" || skill.Internal {
			continue
		}
		sk := skill
		cmds = append(cmds, Command{
			Name:        "/" + sk.Name,
			Description: sk.Description,
			Args:        ArgsOptional,
			Run: func(ctx context.Context, s *Session, args []string) error {
				prompt := s.App.Skills.FormatInvocation(sk, strings.Join(args, " "))
				return RunPrompt(s, "skill "+sk.Name, prompt)
			},
		})
	}
	return cmds
}

// RunPrompt expands skills and submits a prompt to the controller.
func RunPrompt(s *Session, label, input string) error {
	prompt := skills.ExpandCommand(input, s.App.Skills)
	prompt, err := cfg.ApplySelectedSkills(prompt, s.Option.Skills, s.App.Skills)
	if err != nil {
		return err
	}
	return s.Controller.SubmitPrompt(label, input, prompt)
}

// StatusInfo collects current session state for display.
type ProviderInfo struct {
	Name   string
	Model  string
	Active bool
}

type StatusInfo struct {
	Provider  string
	Model     string
	Providers []ProviderInfo
	Mode      string
	Task      string
	IOA       string
	History   string
	Skills    string
}

func CollectStatus(s *Session, mode, historyPath string) StatusInfo {
	info := StatusInfo{
		Mode:    mode,
		History: historyPath,
	}
	if s.App != nil {
		info.Provider = s.App.ProviderConfig.Provider
		info.Model = s.App.ProviderConfig.Model
		if info.Provider != "" {
			info.Providers = append(info.Providers, ProviderInfo{
				Name: info.Provider, Model: info.Model, Active: true,
			})
		}
		for _, fb := range s.App.ProviderFallbacks {
			info.Providers = append(info.Providers, ProviderInfo{
				Name: fb.Provider.Name(), Model: fb.Model,
			})
		}
	}
	if info.Provider == "" {
		info.Provider = "not configured"
	}
	if info.Model == "" {
		info.Model = "-"
	}
	if s.Controller != nil && s.Controller.Running() {
		info.Task = "running"
	} else {
		info.Task = "idle"
	}
	info.IOA = "disabled"
	if s.Option != nil && strings.TrimSpace(s.Option.IOAURL) != "" {
		info.IOA = strings.TrimSpace(s.Option.IOAURL)
		if s.Option.Space != "" {
			info.IOA += " · space " + s.Option.Space
		}
	}
	if s.App != nil && s.App.Skills != nil {
		var names []string
		for _, sk := range s.App.Skills.Skills {
			if strings.TrimSpace(sk.Name) == "" || sk.Internal {
				continue
			}
			names = append(names, "/"+sk.Name)
		}
		const max = 6
		if len(names) > max {
			info.Skills = strings.Join(names[:max], "  ") + fmt.Sprintf("  +%d", len(names)-max)
		} else if len(names) > 0 {
			info.Skills = strings.Join(names, "  ")
		}
	}
	return info
}
