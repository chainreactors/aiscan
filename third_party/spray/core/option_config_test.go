package core

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderConfigTableKeepsRowsAligned(t *testing.T) {
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#87CEEB")).Bold(true)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#98FB98"))
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("99"))
	rows := [][]string{
		{keyStyle.Render("🌐 URL"), valueStyle.Render("[http://beijing.gov/]")},
		{keyStyle.Render("💡 Word"), valueStyle.Render("{?}")},
		{keyStyle.Render("🛑 BlackStatus"), valueStyle.Render("[400 410]")},
		{keyStyle.Render("✅ WhiteStatus"), valueStyle.Render("[200]")},
		{keyStyle.Render("🔄 FuzzyStatus"), valueStyle.Render("[500 501 502 503 301 302 404]")},
		{keyStyle.Render("🔒 UniqueStatus"), valueStyle.Render("[403 200 404]")},
		{keyStyle.Render("⏱ Timeout"), valueStyle.Render("8s")},
		{keyStyle.Render("🏊 PoolSize"), valueStyle.Render("5")},
		{keyStyle.Render("🧵 Threads"), valueStyle.Render("30")},
	}

	out := renderConfigTable(rows, borderStyle)
	lines := strings.Split(out, "\n")
	if len(lines) != len(rows)+2 {
		t.Fatalf("line count = %d, want %d\n%s", len(lines), len(rows)+2, out)
	}

	wantWidth := lipgloss.Width(lines[0])
	for i, line := range lines {
		if got := lipgloss.Width(line); got != wantWidth {
			t.Fatalf("line %d width = %d, want %d\n%s", i, got, wantWidth, out)
		}
	}
	if !strings.Contains(out, "FuzzyStatus") || !strings.Contains(out, "Threads") {
		t.Fatalf("config table missing expected labels:\n%s", out)
	}
}

func TestPrintConfigTimeoutLabelUsesStableWidth(t *testing.T) {
	opt := &Option{
		InputOptions: InputOptions{
			URL: []string{"http://127.0.0.1:9"},
		},
		OutputOptions: OutputOptions{
			NoStat: true,
		},
		MiscOptions: MiscOptions{
			Timeout:  5,
			PoolSize: 5,
			Threads:  1,
		},
	}
	runner := &Runner{Option: opt}
	runner.Word = "{?}"

	out := opt.PrintConfig(runner)
	if strings.Contains(out, "⏱️ Timeout") {
		t.Fatalf("timeout label should not use emoji presentation selector:\n%s", out)
	}
	if !strings.Contains(out, "⏱ Timeout") {
		t.Fatalf("timeout label missing stable-width text presentation:\n%s", out)
	}
}
