package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/agent/inbox"
	"github.com/chainreactors/aiscan/pkg/agent/provider"
	"github.com/chainreactors/aiscan/skills"
)

type SubAgentConfig struct {
	Base        agent.Config // base config inherited by subagents
	ParentInbox inbox.Inbox
	SkillStore  *skills.Store
}

type subAgentInfo struct {
	Name      string
	Type      string
	StartedAt time.Time
	Cancel    context.CancelFunc
}

type SubAgentTool struct {
	cfg     SubAgentConfig
	mu      sync.Mutex
	running map[string]*subAgentInfo
}

func NewSubAgentTool(cfg SubAgentConfig) *SubAgentTool {
	return &SubAgentTool{
		cfg:     cfg,
		running: make(map[string]*subAgentInfo),
	}
}

func (t *SubAgentTool) Name() string { return "subagent" }

func (t *SubAgentTool) Description() string {
	return "Create a subagent to handle an independent task in parallel. Use for complex multi-step work that benefits from separate LLM reasoning."
}

func (t *SubAgentTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionDefinition{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"enum":        []string{"create", "list", "kill"},
						"description": "Action to perform. Default: create",
					},
					"prompt": map[string]any{
						"type":        "string",
						"description": "Task description for the subagent (required for create)",
					},
					"type": map[string]any{
						"type":        "string",
						"description": "Agent type name (a skill with agent:true). Omit to use default configuration.",
					},
					"name": map[string]any{
						"type":        "string",
						"description": "Human-readable label for tracking. Auto-generated if omitted.",
					},
					"background": map[string]any{
						"type":        "boolean",
						"description": "true (default): run in background, notify on completion. false: block until done and return result.",
					},
				},
				"required": []string{"prompt"},
			},
		},
	}
}

func (t *SubAgentTool) Execute(ctx context.Context, arguments string) (string, error) {
	var args struct {
		Action     string `json:"action"`
		Prompt     string `json:"prompt"`
		Type       string `json:"type"`
		Name       string `json:"name"`
		Background *bool  `json:"background"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("parse arguments: %w", err)
	}

	switch args.Action {
	case "list":
		return t.list(), nil
	case "kill":
		return t.kill(args.Name)
	case "", "create":
		return t.create(ctx, args.Prompt, args.Type, args.Name, args.Background)
	default:
		return "", fmt.Errorf("unknown action: %s", args.Action)
	}
}

func (t *SubAgentTool) create(ctx context.Context, prompt, typeName, name string, background *bool) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("prompt is required")
	}

	var skill *skills.Skill
	if typeName != "" && t.cfg.SkillStore != nil {
		s, ok := t.cfg.SkillStore.ByName(typeName)
		if !ok {
			return "", fmt.Errorf("agent type %q not found", typeName)
		}
		if !s.Agent {
			return "", fmt.Errorf("skill %q is not configured as an agent type (add 'agent: true' to its SKILL.md)", typeName)
		}
		skill = &s
	}

	if name == "" {
		if typeName != "" {
			name = typeName
		} else {
			name = labelFromPrompt(prompt)
		}
	}
	name = t.uniqueName(name)

	bg := true
	if background != nil {
		bg = *background
	} else if skill != nil {
		bg = skill.AgentBackground
	}

	cfg := t.cfg.Base
	if skill != nil {
		prompt = skills.FormatInvocation(*skill, prompt)
		if skill.AgentModel != "" {
			cfg = cfg.WithModel(skill.AgentModel)
		}
	}

	if !bg {
		r, err := cfg.Run(ctx, prompt)
		if err != nil {
			return fmt.Sprintf("subagent %q failed: %s", name, err), nil
		}
		return fmt.Sprintf("<subagent_result name=%q type=%q status=\"completed\">\n%s\n</subagent_result>", name, typeName, r.Output), nil
	}

	childCtx, cancel := context.WithCancel(ctx)
	t.track(name, typeName, cancel)

	go func() {
		defer t.untrack(name)
		defer cancel()

		childCfg := cfg.WithInbox(inbox.NewBuffered(16))
		r, err := childCfg.Run(childCtx, prompt)
		result := ""
		if r != nil {
			result = r.Output
		}

		status := "completed"
		content := result
		if err != nil {
			status = "failed"
			if result != "" {
				content = fmt.Sprintf("Error: %s\n\nPartial output:\n%s", err, result)
			} else {
				content = fmt.Sprintf("Error: %s", err)
			}
		}

		msg := inbox.NewMessage(inbox.OriginSystem, "user",
			fmt.Sprintf("<subagent_completion name=%q type=%q status=%q>\n%s\n</subagent_completion>", name, typeName, status, content))
		msg.Meta = map[string]any{
			"subagent": name,
			"type":     typeName,
			"status":   status,
		}
		t.cfg.ParentInbox.Push(msg)
	}()

	return fmt.Sprintf("Started subagent %q (type=%s). Completion will be injected automatically when done.", name, typeName), nil
}

func (t *SubAgentTool) list() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.running) == 0 {
		return "No subagents running."
	}
	var sb strings.Builder
	sb.WriteString("Running subagents:\n")
	for name, info := range t.running {
		elapsed := time.Since(info.StartedAt).Round(time.Second)
		sb.WriteString(fmt.Sprintf("  - %s (type=%s, running %s)\n", name, info.Type, elapsed))
	}
	return sb.String()
}

func (t *SubAgentTool) kill(name string) (string, error) {
	t.mu.Lock()
	info, ok := t.running[name]
	t.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("no running subagent named %q", name)
	}
	info.Cancel()
	return fmt.Sprintf("Subagent %q cancelled.", name), nil
}

func (t *SubAgentTool) track(name, typeName string, cancel context.CancelFunc) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.running[name] = &subAgentInfo{
		Name:      name,
		Type:      typeName,
		StartedAt: time.Now(),
		Cancel:    cancel,
	}
}

func (t *SubAgentTool) untrack(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.running, name)
}

func (t *SubAgentTool) uniqueName(base string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.running[base]; !exists {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if _, exists := t.running[candidate]; !exists {
			return candidate
		}
	}
}

func labelFromPrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if len(prompt) > 30 {
		prompt = prompt[:30]
	}
	words := strings.Fields(prompt)
	if len(words) > 4 {
		words = words[:4]
	}
	return strings.Join(words, "-")
}
