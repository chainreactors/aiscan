package runner

import (
	"strings"

	outputpkg "github.com/chainreactors/aiscan/pkg/output"
	"github.com/mattn/go-runewidth"
)

type terminalRow struct {
	Label string
	Value string
}

type terminalSection struct {
	Title string
	Rows  []terminalRow
}

func renderTerminalSections(title string, sections []terminalSection, width int, colorEnabled bool) string {
	var lines []string
	title = strings.TrimSpace(title)
	if title != "" {
		lines = append(lines, ansiTitle(title, colorEnabled))
	}
	for si, section := range sections {
		if si > 0 || title != "" {
			lines = append(lines, "")
		}
		if strings.TrimSpace(section.Title) != "" {
			lines = append(lines, ansiDim(section.Title, colorEnabled))
		}
		labelWidth := 0
		for _, row := range section.Rows {
			if w := terminalDisplayWidth(row.Label); w > labelWidth {
				labelWidth = w
			}
		}
		for _, row := range section.Rows {
			label := padDisplayRight(row.Label, labelWidth)
			if label != "" {
				label = ansiAccent(label, colorEnabled)
			}
			lines = append(lines, strings.TrimRight(label+"  "+row.Value, " "))
		}
	}
	return renderFixedBox(strings.Join(lines, "\n"), width, colorEnabled)
}

func renderTerminalTable(headers []string, rows [][]string, colorEnabled bool) string {
	if len(headers) == 0 {
		return ""
	}
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = terminalDisplayWidth(header)
	}
	for _, row := range rows {
		for i := 0; i < len(widths) && i < len(row); i++ {
			if w := terminalDisplayWidth(row[i]); w > widths[i] {
				widths[i] = w
			}
		}
	}

	var b strings.Builder
	for i, header := range headers {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(ansiAccent(padDisplayRight(header, widths[i]), colorEnabled))
	}
	for _, row := range rows {
		b.WriteByte('\n')
		for i := range widths {
			if i > 0 {
				b.WriteString("  ")
			}
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			b.WriteString(padDisplayRight(cell, widths[i]))
		}
	}
	return b.String()
}

func terminalDisplayWidth(s string) int {
	return runewidth.StringWidth(outputpkg.StripANSI(s))
}

func padDisplayRight(s string, width int) string {
	padding := width - terminalDisplayWidth(s)
	if padding <= 0 {
		return s
	}
	return s + strings.Repeat(" ", padding)
}

func compactTerminalCell(value string, limit int) string {
	value = compactAgentLine(value, limit)
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}
