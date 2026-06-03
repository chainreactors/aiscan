package cmd

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/app"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/skills"
)

// runScannerWithAgent runs a full agent loop that executes a scanner command
// and analyzes results interactively. The agent can invoke tools, follow up
// on findings, and produce a structured report.
//
// Entry point: aiscan gogo --ai, aiscan spray --ai, etc. (non-scan commands)
func runScannerWithAgent(ctx context.Context, option *Option, application *app.App, scannerArgs []string, logger telemetry.Logger) error {
	if application.Provider == nil {
		return fmt.Errorf("--ai requires a configured LLM provider")
	}

	pidLock, err := acquireAgentPIDFile(agentPIDFilePath(), logger)
	if err != nil {
		return err
	}
	defer pidLock.Release()

	command := scannerArgs[0]
	intent, err := resolveScannerIntent(option, application.Skills, command)
	if err != nil {
		return err
	}

	systemPrompt := agent.BuildSystemPrompt(&agent.PromptConfig{
		Tools:            application.Commands,
		ScannerDocs:      application.Commands.UsageDocs(),
		Skills:           application.Skills.Skills,
		ScannerAgentMode: true,
		ScannerName:      command,
	})

	output := newAgentOutput(option)
	output.Start("scanner", strings.Join(scannerArgs, " "))

	sess := newAgentSession(sessionConfig{
		Application: application,
		Option:      option,
		Logger:      logger,
	})
	defer sess.Cleanup()

	prompt := formatScannerTaskPrompt(scannerArgs, intent)
	logger.Debugf("system prompt length: %d chars", len(systemPrompt))

	result, err := sess.Config.
		WithSystemPrompt(systemPrompt).
		WithStream(false).
		WithEventHandler(output.HandleEvent).
		Run(ctx, prompt)
	if err != nil {
		return err
	}
	if result != nil && strings.TrimSpace(result.Output) != "" {
		output.Final(result.Output)
	}
	return nil
}


// --- intent resolution ---

func resolveScannerIntent(option *Option, store *skills.Store, command string) (string, error) {
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
	intent, err := applySelectedSkills(intent, filterAutoSkill(option.Skills, command), store)
	if err != nil {
		return "", err
	}
	sections = append(sections, intent)
	return strings.Join(sections, "\n\n"), nil
}

func scannerSkillName(command string) string {
	switch command {
	case "gogo", "spray", "katana", "zombie", "neutron", "passive", "scan":
		if !scannerCommandAvailable(command) {
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

// --- prompt formatting ---

func formatScannerTaskPrompt(scannerArgs []string, intent string) string {
	command := strings.Join(scannerArgs, " ")
	if strings.TrimSpace(intent) == "" {
		return fmt.Sprintf("Execute: %s", command)
	}
	return fmt.Sprintf("Execute: %s\n\nUser intent: %s", command, strings.TrimSpace(intent))
}

