package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/chainreactors/aiscan/pkg/provider"
)

const (
	maxOutputSize  = 50 * 1024 // 50KB
	defaultTimeout = 300       // 5 minutes
)

type CommandInterceptor interface {
	Has(name string) bool
	Execute(ctx context.Context, cmdLine string) (string, error)
}

type BashTool struct {
	interceptor CommandInterceptor
	workDir     string
	timeout     int
}

func NewBashTool(workDir string, timeout int, interceptor CommandInterceptor) *BashTool {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &BashTool{
		interceptor: interceptor,
		workDir:     workDir,
		timeout:     timeout,
	}
}

func (t *BashTool) Name() string { return "bash" }

func (t *BashTool) Description() string {
	desc := "Execute a shell command and return its output."
	if t.interceptor != nil {
		desc += " Also supports scanner pseudo-commands (scan, cyberhub, gogo, spray, zombie, neutron) - use them like normal CLI tools."
	}
	return desc
}

func (t *BashTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionDefinition{
			Name:        "bash",
			Description: t.Description(),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "The shell command to execute, or a scanner pseudo-command (e.g., 'scan -i 192.168.1.0/24', 'cyberhub search poc nginx', 'gogo -i 192.168.1.0/24 -p top100', 'spray -u http://target', 'zombie -i ssh://root@10.0.0.1:22 -p password')",
					},
				},
				"required": []string{"command"},
			},
		},
	}
}

func (t *BashTool) Execute(ctx context.Context, arguments string) (string, error) {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	cmdLine := strings.TrimSpace(args.Command)
	if cmdLine == "" {
		return "", fmt.Errorf("empty command")
	}

	if t.interceptor != nil {
		firstToken := firstCommandToken(cmdLine)
		if firstToken != "" && t.interceptor.Has(firstToken) {
			return t.executeIntercepted(ctx, cmdLine)
		}
	}

	return t.execShell(ctx, cmdLine)
}

func (t *BashTool) executeIntercepted(ctx context.Context, cmdLine string) (string, error) {
	return t.interceptor.Execute(ctx, cmdLine)
}

func (t *BashTool) execShell(ctx context.Context, cmdLine string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(t.timeout)*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", cmdLine)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", cmdLine)
	}
	cmd.Dir = t.workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	var sb strings.Builder
	if stdout.Len() > 0 {
		sb.Write(stdout.Bytes())
	}
	if stderr.Len() > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("[stderr] ")
		sb.Write(stderr.Bytes())
	}

	output := sb.String()
	if len(output) > maxOutputSize {
		original := len(output)
		output = output[:maxOutputSize] + fmt.Sprintf(
			"\n\n[truncated: showing %d of %d bytes]", maxOutputSize, original)
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return output, fmt.Errorf("command timed out after %ds", t.timeout)
		}
		if output == "" {
			return "", err
		}
		return output + "\n[exit code: non-zero]", nil
	}

	return output, nil
}

func firstCommandToken(input string) string {
	input = strings.TrimSpace(input)
	var sb strings.Builder
	var quote rune
	escaped := false
	for _, r := range input {
		if escaped {
			sb.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			sb.WriteRune(r)
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			break
		}
		sb.WriteRune(r)
	}
	return sb.String()
}
