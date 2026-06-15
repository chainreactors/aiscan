package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type agentTip struct {
	ID   string
	Text string
}

var defaultAgentTips = []agentTip{
	{ID: "status", Text: "Use /status to inspect the model, IOA state, render mode, and loaded skills."},
	{ID: "continue", Text: "Use /continue when you want aiscan to keep working without adding new context."},
	{ID: "reset", Text: "Use /reset before switching targets so stale context does not leak into the next task."},
	{ID: "forwarded", Text: "Set AISCAN_RENDER=forwarded when another agent is consuming this PTY transcript."},
	{ID: "fast-repl", Text: "Set AISCAN_REPL=fast only when you explicitly want lightweight line input on a slow remote PTY."},
	{ID: "evidence", Text: "For reports, ask aiscan to keep only findings with reproducible evidence."},
}

type agentTipHistory struct {
	Shown map[string]int64 `json:"shown"`
}

func agentTipsEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AISCAN_TIPS"))) {
	case "1", "true", "on", "yes", "enabled":
		return true
	case "0", "false", "off", "no", "disabled":
		return false
	default:
		return false
	}
}

func chooseAgentTip() (agentTip, bool) {
	tips := configuredAgentTips()
	if len(tips) == 0 {
		return agentTip{}, false
	}
	history := loadAgentTipHistory()
	var (
		chosen agentTip
		ok     bool
		oldest int64
	)
	for _, tip := range tips {
		lastShown := history.Shown[tip.ID]
		if !ok || lastShown < oldest {
			chosen = tip
			oldest = lastShown
			ok = true
		}
	}
	return chosen, ok
}

func configuredAgentTips() []agentTip {
	override := strings.TrimSpace(os.Getenv("AISCAN_TIPS_OVERRIDE"))
	if override == "" {
		return defaultAgentTips
	}
	parts := strings.Split(override, "|")
	tips := make([]agentTip, 0, len(parts))
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tips = append(tips, agentTip{
			ID:   "custom-" + strconv.Itoa(i),
			Text: part,
		})
	}
	return tips
}

func loadAgentTipHistory() agentTipHistory {
	history := agentTipHistory{Shown: make(map[string]int64)}
	data, err := os.ReadFile(agentTipHistoryPath())
	if err != nil {
		return history
	}
	if err := json.Unmarshal(data, &history); err != nil {
		return agentTipHistory{Shown: make(map[string]int64)}
	}
	if history.Shown == nil {
		history.Shown = make(map[string]int64)
	}
	return history
}

func recordAgentTipShown(tip agentTip) {
	if strings.TrimSpace(tip.ID) == "" {
		return
	}
	history := loadAgentTipHistory()
	history.Shown[tip.ID] = time.Now().UnixNano()
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return
	}
	path := agentTipHistoryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

func agentTipHistoryPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(configDir) == "" {
		return filepath.Join(os.TempDir(), "aiscan_tip_history.json")
	}
	return filepath.Join(configDir, "aiscan", "tip_history.json")
}
