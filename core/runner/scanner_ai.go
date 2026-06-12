package runner

import (
	"context"
	"fmt"
	"os"
	"strings"

	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/app"
	"github.com/chainreactors/aiscan/pkg/pidlock"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/pkg/tools/scan"
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
		PromptConfig: &PromptConfig{
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

	prompt := scan.FormatAgentTaskPrompt(scannerArgs, intent)
	rt.Output.Start("scanner", strings.Join(scannerArgs, " "))

	result, err := agent.NewAgent(rt.Config.
		WithSystemPrompt(rt.SystemPrompt).
		WithStream(false)).
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
	skillName := scan.ScannerSkillName(command)
	if skillName != "" && cfg.ScannerCommandAvailable(command) {
		if skill, ok := store.ByName(skillName); ok {
			sections = append(sections, store.FormatInvocation(skill, ""))
		}
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
	intent, err := cfg.ApplySelectedSkills(intent, scan.FilterAutoSkill(option.Skills, command), store)
	if err != nil {
		return "", err
	}
	sections = append(sections, intent)
	return strings.Join(sections, "\n\n"), nil
}

// injectScanSubSkills maps scan AI flags (--verify, --sniper, --deep) to their
// corresponding skill names and appends them to option.Skills. It also injects
// --verify=<level> into processedArgs when the level comes from config (not CLI)
// so the agent can see the threshold.
func injectScanSubSkills(option *cfg.Option, originalArgs []string, processedArgs []string) {
	if len(processedArgs) == 0 || processedArgs[0] != "scan" {
		return
	}
	origFlags := originalArgs
	if len(origFlags) > 0 {
		origFlags = origFlags[1:]
	}
	procFlags := processedArgs[1:]

	verifyMode, explicit := scannerVerifyMode(origFlags)
	needVerify := false
	if explicit && verifyMode != "off" {
		needVerify = true
	} else {
		switch verifyMode {
		case "low", "medium", "high", "critical":
			needVerify = true
		}
	}
	if needVerify {
		appendSkillIfMissing(&option.Skills, "scan/verify")
	}
	if HasScannerFlag(procFlags, "--sniper") {
		appendSkillIfMissing(&option.Skills, "scan/sniper")
	}
	if HasScannerFlag(procFlags, "--deep") {
		appendSkillIfMissing(&option.Skills, "scan/deep")
	}
}

func appendSkillIfMissing(skills *[]string, name string) {
	for _, s := range *skills {
		if strings.TrimSpace(s) == name {
			return
		}
	}
	*skills = append(*skills, name)
}
