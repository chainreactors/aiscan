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
	Ctx            context.Context
	Option         *cfg.Option
	App            *app.App
	Agent          *agent.Agent
	Controller     Controller
	EvalCriteria   string
	OnEvalChange   func(string)
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
	ArgsNone    ArgSpec = iota
	ArgsExact1
	ArgsOptional
)

// BuiltinCommands returns the standard REPL commands.
func BuiltinCommands() []Command {
	return []Command{
		{
			Name: "/help", Description: "查看命令面板",
			Args: ArgsNone,
			Run:  func(_ context.Context, _ *Session, _ []string) error { return nil },
		},
		{
			Name: "/status", Description: "查看模型、渲染模式、IOA 和 skills",
			Args: ArgsNone,
			Run:  func(_ context.Context, _ *Session, _ []string) error { return nil },
		},
		{
			Name: "/reset", Description: "清空当前会话上下文",
			Args: ArgsNone,
			Run: func(_ context.Context, s *Session, _ []string) error {
				if s.Controller != nil && s.Controller.Running() {
					return fmt.Errorf("task is running — use /stop first")
				}
				s.Agent.Reset()
				return nil
			},
		},
		{
			Name: "/continue", Description: "不追加输入，继续上一轮任务",
			Args: ArgsNone,
			Run: func(_ context.Context, s *Session, _ []string) error {
				return s.Controller.Continue()
			},
		},
		{
			Name: "/stop", Description: "停止当前正在运行的任务",
			Args: ArgsNone,
			Run: func(_ context.Context, s *Session, _ []string) error {
				if !s.Controller.Stop() {
					return fmt.Errorf("no running task")
				}
				return nil
			},
		},
		{
			Name: "/followup", Description: "排队到当前任务结束后再发送",
			Args: ArgsExact1,
			Run: func(ctx context.Context, s *Session, args []string) error {
				return RunPrompt(s, "follow-up", args[0])
			},
		},
		{
			Name: "/eval", Description: "设置/查看/关闭 goal evaluation (/eval off 关闭)",
			Args: ArgsOptional,
			Run: func(_ context.Context, s *Session, args []string) error {
				text := strings.TrimSpace(strings.Join(args, " "))
				switch text {
				case "":
					if s.EvalCriteria == "" {
						fmt.Println("Goal evaluation: off")
					} else {
						fmt.Printf("Goal evaluation: on\n  criteria: %s\n", s.EvalCriteria)
					}
				case "off":
					s.EvalCriteria = ""
					if s.OnEvalChange != nil {
						s.OnEvalChange("")
					}
					fmt.Println("Goal evaluation disabled.")
				default:
					s.EvalCriteria = text
					if s.OnEvalChange != nil {
						s.OnEvalChange(text)
					}
					fmt.Printf("Goal evaluation enabled: %s\n", text)
				}
				return nil
			},
		},
		{
			Name: "/exit", Aliases: []string{"/quit"}, Description: "退出交互模式",
			Args: ArgsNone,
		},
	}
}

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

// ProviderCommands returns the /provider command group.
func ProviderCommands() []Command {
	return []Command{
		{
			Name:        "/provider",
			Description: "查看/管理 LLM provider 链",
			Args:        ArgsOptional,
		},
	}
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
