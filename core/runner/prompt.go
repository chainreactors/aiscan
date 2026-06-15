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
	// Preamble
	CustomPreamble   string
	ScannerAgentMode bool
	ScannerName      string

	// Environment
	OS           string
	Time         string
	Hostname     string
	Node         string
	Space        string
	FindingsPath string
	Windows      bool

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

## Interactive CLI Response Style

- Match the user's language unless they ask otherwise.
- For greetings, thanks, small talk, or brief meta questions, reply directly in one or two short sentences. Do not print capability lists, tutorials, example prompts, or long onboarding text unless the user asks for them.
- For ordinary questions, answer the question first; only add commands, tables, or detailed workflows when they clearly help the user's immediate task.
- Keep Markdown compact in the REPL. Prefer plain paragraphs for simple answers and short bullets for operational results.
- For long-running or multi-step work, provide brief visible progress updates before major tool batches and when switching direction. Keep them concise and continue using tools without unnecessary delay.

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

Use boundary standards and target traits, not fixed vulnerability checklists.

Boundary standards:
- Scanner output is evidence, not proof. Report "confirmed" only with independent, reproducible evidence that demonstrates security impact.
- Confirmed reportable findings need executable proof: a self-contained curl/protocol command, saved browser replay, or equivalent PoC evidence. No PoC means not confirmed.
- Suppress standalone P3/low/informational reports unless the user requested inventory or the issue chains into demonstrated impact.
- Treat fingerprints, versions, open ports, CORS/security headers, template matches, generic 200 responses, login pages, default pages, self-XSS, open redirects, GraphQL introspection, and unchained primitives as leads unless impact is demonstrated.
- For authorization and IDOR, one changed ID is a lead. Test 3-5 observed, adjacent, or cross-account identifiers when available before calling impact confirmed.
- For JS discovery, aim to enumerate all reachable script sources and interfaces from crawlers, rendered pages, network traces, source maps, route manifests, and archived hints. Do not claim complete hidden-endpoint coverage unless the explored sources are stated and the remaining limits are clear.

Decision routing:
- Login/auth boundary -> authorization, IDOR, role or tenant boundary first.
- API service -> unauthenticated access, method changes, role boundaries, and feeding response fields from one endpoint into related endpoints.
- Upload/import/media -> upload validation, storage access, rendering behavior, metadata leakage, and post-upload authorization.
- Search/filter/export/sort/orderBy/input -> injection-style validation and authorization-sensitive data slicing.
- GraphQL -> protected query or mutation impact first; introspection alone is reconnaissance.
- No clear surface -> JS, source maps, route manifests, robots/sitemap/archive data, browser network traces, and hidden endpoint discovery.
- If a branch produces no useful evidence after about 20 minutes or several negative probes, checkpoint it and switch direction.

- For long-running or broad assessments, keep a progressive findings log at {{.FindingsPath}} for confirmed findings: target, vuln type, severity, one-line summary, and reproducible command or PoC evidence. Re-read it before producing a final report.
- Read aiscan://skills/aiscan/SKILL.md when you need aiscan execution rules, output consumption behavior, or scanner-specific tool usage.
- Use conservative thread counts and timeouts for fragile targets.
- Let user intent define stopping criteria. For broad assessments, continue beyond the first serious finding when scope and time allow; for narrow validation tasks, answer the specific question directly.
- For broad scan reports, mention material high-value leads that remain untested. Do not invent leads or expand scope solely to satisfy a count.
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
	findingsPath := cfg.FindingsPath
	if findingsPath == "" && agentCfg != nil {
		findingsPath = findingsLogPath(agentCfg.SessionID)
	}
	if findingsPath == "" {
		findingsPath = findingsLogPath("")
	}

	data := promptData{
		CustomPreamble:   cfg.CustomPreamble,
		ScannerAgentMode: cfg.ScannerAgentMode,
		ScannerName:      cfg.ScannerName,
		OS:               runtime.GOOS + "/" + runtime.GOARCH,
		Time:             time.Now().Format(time.RFC3339),
		Hostname:         hostname,
		Node:             cfg.NodeName,
		Space:            cfg.Space,
		FindingsPath:     findingsPath,
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
			data.LoadedSkills = append(data.LoadedSkills, loadedSkillEntry(ls))
		}
	}

	if cfg.ScannerAgentMode {
		data.Constraints = "## Scanner Agent Constraints\n\n" +
			"- Execute the scanner command provided in the task via the bash tool.\n" +
			"- For structured data processing, re-run the scanner with `-j` flag to get JSON output."
		if _, ok := tools.GetTool("checkpoint"); ok {
			data.Constraints += "\n" +
				"- When scanner analysis is complete, call the `checkpoint` tool exactly once with the final conclusion."
		}
	}

	var sb strings.Builder
	if err := systemPromptTemplate.Execute(&sb, data); err != nil {
		return "You are a helpful assistant."
	}
	return sb.String()
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
