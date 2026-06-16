package command

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chainreactors/aiscan/pkg/agent/inbox"
	"github.com/chainreactors/aiscan/pkg/agent/tmux"
	"github.com/chainreactors/aiscan/pkg/truncate"
)

const (
	defaultTimeout          = 300
	autoBackgroundThreshold = 15 * time.Second
)

const monitorInterval = 10 * time.Second

type BashTool struct {
	workDir      string
	timeout      int
	scannerProxy string
	tasks        *tmux.Manager
	commandNames func() []string
	inbox        inbox.Inbox
}

func NewBashTool(workDir string, timeout int) *BashTool {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &BashTool{
		workDir: workDir,
		timeout: timeout,
		tasks:   tmux.NewManager(),
	}
}

func (t *BashTool) Manager() *tmux.Manager         { return t.tasks }
func (t *BashTool) SetScannerProxy(proxy string)    { t.scannerProxy = proxy }
func (t *BashTool) SetCommandNames(fn func() []string) { t.commandNames = fn }
func (t *BashTool) SetInbox(ib inbox.Inbox)              { t.inbox = ib }
func (t *BashTool) Name() string                    { return "bash" }
func (t *BashTool) Close()                          { t.tasks.Shutdown() }

func (t *BashTool) WithScannerProxy(proxy string) *BashTool {
	t.scannerProxy = proxy
	return t
}

func (t *BashTool) Description() string {
	desc := "Execute a shell command and return its output."
	if t.commandNames != nil {
		names := t.commandNames()
		if len(names) > 0 {
			desc += " IMPORTANT: This tool also handles pseudo-commands (" + strings.Join(names, ", ") + "). Pass them as the command parameter."
		}
	}
	return desc
}

type BashArgs struct {
	Command string `json:"command" jsonschema:"description=The command to execute. For shell commands: any valid sh command. For pseudo-commands (scan, gogo, tmux, etc.): pass them directly here."`
}

func (t *BashTool) Definition() ToolDefinition {
	return ToolDef("bash", t.Description(), BashArgs{})
}

func (t *BashTool) Execute(ctx context.Context, arguments string) (ToolResult, error) {
	args, err := ParseArgs[BashArgs](arguments)
	if err != nil {
		return ToolResult{}, err
	}

	cmdLine := strings.TrimSpace(args.Command)
	if cmdLine == "" {
		return ToolResult{}, fmt.Errorf("empty command")
	}
	if isOnlyCommentsOrBlank(cmdLine) {
		return TextResult("ok"), nil
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(t.timeout)*time.Second)
	defer cancel()

	info, runErr := t.tasks.RunCommand(cmdLine, tmux.RunOpts{
		Timeout: time.Duration(t.timeout) * time.Second,
		WorkDir: t.workDir,
		Env:     t.proxyEnv(),
	})
	if runErr != nil {
		return ToolResult{}, runErr
	}

	return t.waitOrBackground(info.ID, ctx)
}

func (t *BashTool) waitOrBackground(id string, ctx context.Context) (ToolResult, error) {
	done := t.tasks.Done(id)
	select {
	case <-done:
		return t.collectResult(id, ctx), nil
	case <-time.After(autoBackgroundThreshold):
		info, _ := t.tasks.Get(id)
		t.startMonitor(info)
		return TextResult(fmt.Sprintf(
			"Command auto-backgrounded (exceeded %s).\nsession id=%s name=%s\nIncremental output will be delivered automatically. Use `tmux kill -t %s` to stop.",
			autoBackgroundThreshold, info.ID, info.Name, info.ID)), nil
	case <-ctx.Done():
		_ = t.tasks.Kill(id)
		<-done
		return t.collectResult(id, ctx), nil
	}
}

func (t *BashTool) collectResult(id string, ctx context.Context) ToolResult {
	raw := t.tasks.PeekOrEmpty(id, truncate.DefaultMaxLines)
	r := truncate.Tail(raw, truncate.Options{})
	output := r.Content
	if r.Truncated {
		startLine := r.TotalLines - r.OutputLines + 1
		output += fmt.Sprintf(
			"\n\n[truncated: showing lines %d-%d of %d (%s of %s). Use tmux read to access earlier output.]",
			startLine, r.TotalLines, r.TotalLines, truncate.FormatSize(r.OutputBytes), truncate.FormatSize(r.TotalBytes))
	}
	if ctx.Err() != nil {
		output += fmt.Sprintf("\n[command timed out after %ds]", t.timeout)
	}
	info, _ := t.tasks.Get(id)
	if info.ExitCode != 0 && info.State != tmux.StateRunning {
		output += fmt.Sprintf("\n[exit code: %d]", info.ExitCode)
	}
	return TextResult(output)
}

func (t *BashTool) proxyEnv() []string {
	if t.scannerProxy == "" {
		return nil
	}
	return []string{
		"ALL_PROXY=" + t.scannerProxy,
		"all_proxy=" + t.scannerProxy,
		"HTTP_PROXY=" + t.scannerProxy,
		"http_proxy=" + t.scannerProxy,
		"HTTPS_PROXY=" + t.scannerProxy,
		"https_proxy=" + t.scannerProxy,
	}
}

func (t *BashTool) startMonitor(info tmux.Info) {
	if t.inbox == nil {
		return
	}
	t.tasks.Monitor(info.ID, monitorInterval, func(output string) {
		msg := inbox.NewMessage(inbox.OriginSession, "user",
			fmt.Sprintf("<session_output id=%q name=%q>\n%s\n</session_output>", info.ID, info.Name, output))
		msg.Priority = inbox.PriorityLow
		msg.Meta = map[string]any{
			"session_id":   info.ID,
			"session_name": info.Name,
			"type":         "incremental",
		}
		_ = t.inbox.Push(msg)
	})
}

func isOnlyCommentsOrBlank(cmdLine string) bool {
	for _, line := range strings.Split(cmdLine, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			return false
		}
	}
	return true
}
