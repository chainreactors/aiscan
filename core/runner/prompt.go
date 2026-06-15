package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"

	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/command"
	"github.com/chainreactors/aiscan/skills"
)

type PromptConfig struct {
	Tools            *command.CommandRegistry
	ScannerDocs      string
	CustomPreamble   string
	Skills           []skills.Skill
	LoadedSkills     []LoadedSkill // skill body 直接嵌入 prompt
	ScannerAgentMode bool
	ScannerName      string
	NodeName         string
	Space            string
	FindingsPath     string
}

// LoadedSkill is a skill whose full body is embedded directly into the prompt.
type LoadedSkill struct {
	Name string
	Body string
}

// SystemPromptFunc returns an agent.SystemPromptFunc that builds the system
// prompt dynamically on each turn.
func SystemPromptFunc(cfg *PromptConfig) agent.SystemPromptFunc {
	return func(agentCfg *agent.Config) string {
		return BuildSystemPrompt(cfg, agentCfg)
	}
}

// promptData is the template context passed to the system prompt template.
type promptData struct {
	Preamble string

	// Environment
	OS       string
	Time     string
	Hostname string
	Node     string
	Space    string
	Windows  bool

	// Tools
	Tools []toolEntry

	// Pseudo-commands
	ScannerDocs string

	// Skills
	Skills []skillEntry

	// Loaded skills (body embedded)
	LoadedSkills []loadedSkillEntry
}

type toolEntry struct {
	Name        string
	Description string
}

type skillEntry struct {
	Name        string
	Description string
	Location    string
}

type loadedSkillEntry struct {
	Name string
	Body string
}

var systemPromptTemplate = template.Must(template.New("system").Parse(systemPromptText))

const systemPromptText = `{{- .Preamble}}

## Environment

Operating System: {{.OS}}
Current Time: {{.Time}}
{{- if .Hostname}}
Hostname: {{.Hostname}}
{{- end}}
{{- if .Node}}
Node: {{.Node}}
{{- end}}
{{- if .Space}}
Space: {{.Space}}
{{- end}}
{{- if .Windows}}
Shell: cmd.exe — do NOT use Unix shell syntax (2>&1, |, /dev/null). Pseudo-commands run in-process and need no shell redirections.
{{- end}}
{{if .Tools}}
## Available Tools
{{range .Tools}}
### {{.Name}}
{{.Description}}
{{end}}
{{- end}}
{{- if .ScannerDocs}}
## Pseudo-Commands (IMPORTANT: use the bash tool)

Pseudo-commands are NOT system binaries — they are built into the bash tool. Call the bash tool with the pseudo-command as the "command" parameter.

Example: bash {"command": "scan -i 192.168.1.0/24 --mode quick"}

Available pseudo-commands:
{{.ScannerDocs}}
Read the corresponding skill file for detailed usage: ` + "`aiscan://skills/<command>/SKILL.md`" + `.
{{end}}
{{- if .Skills}}
## Available Skills

The following skills provide specialized instructions for specific security scanning tasks.
Use the read tool to load a skill file when the task matches its description.
When a skill references relative paths, resolve them relative to the skill base directory.

<available_skills>
{{- range .Skills}}
  <skill>
    <name>{{.Name}}</name>
    <description>{{.Description}}</description>
    <location>{{.Location}}</location>
  </skill>
{{- end}}
</available_skills>
{{end}}
{{- range .LoadedSkills}}

## Skill: {{.Name}}

{{.Body}}
{{- end}}
`

// BuildSystemPrompt assembles the system prompt from config.
func BuildSystemPrompt(cfg *PromptConfig, agentCfg *agent.Config) string {
	if cfg == nil {
		cfg = &PromptConfig{}
	}
	tools := cfg.Tools
	if tools == nil && agentCfg != nil {
		tools = agentCfg.Tools
	}
	if tools == nil {
		tools = command.NewRegistry()
	}

	hostname, _ := os.Hostname()

	data := promptData{
		Preamble:    buildPreamble(cfg),
		OS:          runtime.GOOS + "/" + runtime.GOARCH,
		Time:        time.Now().Format(time.RFC3339),
		Hostname:    hostname,
		Node:        cfg.NodeName,
		Space:       cfg.Space,
		Windows:     runtime.GOOS == "windows",
		ScannerDocs: cfg.ScannerDocs,
	}

	for _, t := range tools.Tools() {
		data.Tools = append(data.Tools, toolEntry{Name: t.Name(), Description: t.Description()})
	}

	for _, s := range cfg.Skills {
		if !s.Internal {
			data.Skills = append(data.Skills, skillEntry{
				Name:        s.Name,
				Description: s.Description,
				Location:    s.Location,
			})
		}
	}

	// Always load the base aiscan skill.
	loadedNames := make(map[string]bool)
	baseBody := skills.ReadBody("aiscan")
	if baseBody != "" {
		findingsPath := cfg.FindingsPath
		if findingsPath == "" && agentCfg != nil {
			findingsPath = findingsLogPath(agentCfg.SessionID)
		}
		if findingsPath == "" {
			findingsPath = findingsLogPath("")
		}
		baseBody = strings.ReplaceAll(baseBody, "{{findings_path}}", findingsPath)
		data.LoadedSkills = append(data.LoadedSkills, loadedSkillEntry{Name: "aiscan", Body: baseBody})
		loadedNames["aiscan"] = true
	}

	for _, ls := range cfg.LoadedSkills {
		if ls.Body != "" && !loadedNames[ls.Name] {
			data.LoadedSkills = append(data.LoadedSkills, loadedSkillEntry(ls))
			loadedNames[ls.Name] = true
		}
	}

	var sb strings.Builder
	if err := systemPromptTemplate.Execute(&sb, data); err != nil {
		return "You are a helpful assistant."
	}
	return sb.String()
}

func buildPreamble(cfg *PromptConfig) string {
	if cfg.CustomPreamble != "" {
		return cfg.CustomPreamble
	}
	if cfg.ScannerAgentMode {
		preamble := skills.ReadFile("scan/agent.md")
		if preamble != "" {
			return strings.ReplaceAll(preamble, "{{scanner_name}}", cfg.ScannerName)
		}
		return fmt.Sprintf("You are aiscan's %s analysis agent.", cfg.ScannerName)
	}
	return "You are aiscan, an autonomous security research agent."
}

func findingsLogPath(sessionID string) string {
	safe := sanitizeFileToken(sessionID)
	if safe == "" {
		safe = fmt.Sprintf("pid-%d", os.Getpid())
	}
	return filepath.Join(os.TempDir(), "aiscan-findings-"+safe+".md")
}

func sanitizeFileToken(value string) string {
	var sb strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			sb.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			sb.WriteRune(r)
		case r >= '0' && r <= '9':
			sb.WriteRune(r)
		case r == '-' || r == '_':
			sb.WriteRune(r)
		default:
			sb.WriteByte('-')
		}
	}
	return strings.Trim(sb.String(), "-")
}
