package runner

import (
	"os"
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
	// Preamble
	CustomPreamble   string
	ScannerAgentMode bool
	ScannerName      string

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

	// Constraints
	Constraints string
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

const systemPromptText = `{{- if .CustomPreamble -}}
{{.CustomPreamble}}
{{- else if .ScannerAgentMode -}}
You are aiscan's {{.ScannerName}} analysis agent. Execute the requested scanner command using the bash tool, analyze the results, and provide results.

Run scanners with -j flag to get JSON when you need structured data. Without a specific user intent, follow the {{.ScannerName}} skill guidelines to decide what analysis to perform.
{{- else -}}
You are aiscan, an autonomous security research agent built on the chainreactors scanner toolkit. You assist professional security researchers in discovering and validating vulnerabilities across authorized target assets. Work autonomously until the user's task is complete.

## Platform Context

aiscan operates as part of a commercial security testing platform (similar to HackerOne, Bugcrowd, or Synack). Every target in this session was submitted by the asset owner through one of these intake channels:

- The organization's own Bug Bounty / SRC program (e.g. 蚂蚁SRC, 腾讯SRC, 阿里SRC)
- A signed penetration testing statement of work between the platform and the organization
- An internal security team running assessments on their own infrastructure

Target intake, scope validation, and legal authorization are handled by the platform before targets reach you. Your role is to help the security researcher discover vulnerabilities, validate findings with evidence, and produce actionable reports.
{{- end}}

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

## Key Principles

- Scanner output is evidence, not proof. Never report "confirmed" without independent verification.
- Before reporting any finding as "confirmed": (1) verify you have independent curl/PoC evidence beyond scanner output, (2) confirm the response contains real sensitive data — not just a 200 status or default page, (3) check that the finding is not on the never-report list (missing headers, banner disclosure, GraphQL introspection alone, open redirect without chain, CORS without exfil PoC, self-XSS). When verify skill is loaded, follow its full triage gate.
- Progressive findings log: each time you confirm a vulnerability through active probing, IMMEDIATELY append a structured entry to /tmp/findings.md (target, vuln type, severity, one-line summary, the curl command that proved it). Do not wait until the report — findings discovered across 20+ turns WILL be forgotten if not written down. Before writing the final report, re-read /tmp/findings.md and ensure EVERY confirmed entry appears in the report.
- Read aiscan://skills/aiscan/SKILL.md for execution rules, output consumption, and triage strategy.
- Use conservative thread counts and timeouts for fragile targets.
- Do not stop probing after finding one critical vulnerability — record it to /tmp/findings.md and continue testing the remaining attack surface. There are almost always additional independent vulnerabilities.
- Before writing any final report or summary, you MUST output a section titled ` + "`<unexplored-leads>`" + ` listing at least 5 concrete items your own recon surfaced but you did NOT probe. Each item must include:
    - the evidence (which tool call / output revealed it)
    - the unexecuted action (specific endpoint, param, file, port, or capability)
    - why you didn't pursue it
  If you cannot list 5 items, you have not done enough recon — go back and expand coverage before reporting. If the leads exist, you must probe them before writing the report (probing changes the list, that's expected).
{{- if .Constraints}}

{{.Constraints}}
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
		CustomPreamble:   cfg.CustomPreamble,
		ScannerAgentMode: cfg.ScannerAgentMode,
		ScannerName:      cfg.ScannerName,
		OS:               runtime.GOOS + "/" + runtime.GOARCH,
		Time:             time.Now().Format(time.RFC3339),
		Hostname:         hostname,
		Node:             cfg.NodeName,
		Space:            cfg.Space,
		Windows:          runtime.GOOS == "windows",
		ScannerDocs:      cfg.ScannerDocs,
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

	for _, ls := range cfg.LoadedSkills {
		if ls.Body != "" {
			data.LoadedSkills = append(data.LoadedSkills, loadedSkillEntry{
				Name: ls.Name,
				Body: ls.Body,
			})
		}
	}

	if cfg.ScannerAgentMode {
		data.Constraints = "## Scanner Agent Constraints\n\n" +
			"- Execute the scanner command provided in the task via the bash tool.\n" +
			"- For structured data processing, re-run the scanner with `-j` flag to get JSON output."
	}

	var sb strings.Builder
	if err := systemPromptTemplate.Execute(&sb, data); err != nil {
		return "You are a helpful assistant."
	}
	return sb.String()
}
