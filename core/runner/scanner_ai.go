package runner

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/app"
	"github.com/chainreactors/aiscan/pkg/pidlock"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/skills"
)

func RunScannerWithAgent(ctx context.Context, option *cfg.Option, application *app.App, scannerArgs []string, logger telemetry.Logger) error {
	if application.Provider == nil {
		return fmt.Errorf("--ai requires a configured LLM provider")
	}

	pidLock, err := pidlock.Acquire(pidlock.AgentPIDFilePath(), logger)
	if err != nil {
		return err
	}
	defer pidLock.Release()

	command := scannerArgs[0]
	intent, err := resolveScannerIntent(option, application.Skills, command)
	if err != nil {
		return err
	}

	rt, err := NewAgentRuntime(ctx, option, logger, &RuntimeConfig{
		ExistingApp: application,
		PromptConfig: &agent.PromptConfig{
			Tools:            application.Commands,
			ScannerDocs:      application.Commands.UsageDocs(),
			Skills:           application.Skills.Skills,
			ScannerAgentMode: true,
			ScannerName:      command,
		},
	})
	if err != nil {
		return err
	}
	defer rt.Close()

	prompt := formatScannerTaskPrompt(scannerArgs, intent)
	rt.Output.Start("scanner", strings.Join(scannerArgs, " "))

	result, err := rt.Session.Config.
		WithSystemPrompt(rt.SystemPrompt).
		WithStream(false).
		WithEventHandler(rt.EventHandler()).
		Run(ctx, prompt)
	if err != nil {
		return err
	}
	if result != nil && strings.TrimSpace(result.Output) != "" {
		rt.Output.Final(result.Output)
	}
	return nil
}

func resolveScannerIntent(option *cfg.Option, store *skills.Store, command string) (string, error) {
	var sections []string
	if skill, ok := store.ByName(scannerSkillName(command)); ok {
		sections = append(sections, skills.FormatInvocation(skill, ""))
	}

	intent := strings.TrimSpace(option.Prompt)
	if intent == "" && option.TaskFile != "" {
		data, err := os.ReadFile(option.TaskFile)
		if err != nil {
			return "", fmt.Errorf("read task file: %w", err)
		}
		intent = strings.TrimSpace(string(data))
	}
	if intent == "" {
		intent = "Process the scanner output according to the user's intent. If no specific intent is provided, briefly explain the important evidence in the output."
	}
	intent, err := cfg.ApplySelectedSkills(intent, filterAutoSkill(option.Skills, command), store)
	if err != nil {
		return "", err
	}
	sections = append(sections, intent)
	return strings.Join(sections, "\n\n"), nil
}

func scannerSkillName(command string) string {
	switch command {
	case "gogo", "spray", "katana", "zombie", "neutron", "passive", "scan":
		if !cfg.ScannerCommandAvailable(command) {
			return ""
		}
		return command
	default:
		return ""
	}
}

func filterAutoSkill(selected []string, command string) []string {
	auto := scannerSkillName(command)
	if auto == "" {
		return selected
	}
	out := make([]string, 0, len(selected))
	for _, name := range selected {
		if strings.TrimSpace(name) == auto {
			continue
		}
		out = append(out, name)
	}
	return slices.Clip(out)
}

func formatScannerTaskPrompt(scannerArgs []string, intent string) string {
	command := strings.Join(scannerArgs, " ")
	if strings.TrimSpace(intent) == "" {
		return fmt.Sprintf("Execute: %s", command)
	}
	return fmt.Sprintf("Execute: %s\n\nUser intent: %s", command, strings.TrimSpace(intent))
}
