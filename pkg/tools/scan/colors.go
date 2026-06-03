package scan

import (
	"regexp"

	"github.com/chainreactors/logs"
	"github.com/chainreactors/parsers"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func stripANSI(value string) string {
	return ansiPattern.ReplaceAllString(value, "")
}

type renderColor struct {
	enabled bool
}

func newRenderColor(enabled bool) renderColor {
	return renderColor{enabled: enabled}
}

func (rc renderColor) Green(s string) string {
	if !rc.enabled {
		return s
	}
	return logs.Green(s)
}

func (rc renderColor) GreenBold(s string) string {
	if !rc.enabled {
		return s
	}
	return logs.GreenBold(s)
}

func (rc renderColor) Red(s string) string {
	if !rc.enabled {
		return s
	}
	return logs.Red(s)
}

func (rc renderColor) RedBold(s string) string {
	if !rc.enabled {
		return s
	}
	return logs.RedBold(s)
}

func (rc renderColor) Yellow(s string) string {
	if !rc.enabled {
		return s
	}
	return logs.Yellow(s)
}

func (rc renderColor) YellowBold(s string) string {
	if !rc.enabled {
		return s
	}
	return logs.YellowBold(s)
}

func (rc renderColor) Cyan(s string) string {
	if !rc.enabled {
		return s
	}
	return logs.Cyan(s)
}

func (rc renderColor) Dim(s string) string {
	if !rc.enabled {
		return s
	}
	return "\033[90m" + s + "\033[0m"
}

func (rc renderColor) Status(s string) string {
	if !rc.enabled {
		return s
	}
	return parsers.RenderStatus(s)
}

func (rc renderColor) ForPriority(p priority) func(string) string {
	if !rc.enabled {
		return func(s string) string { return s }
	}
	switch p {
	case priorityLow:
		return logs.Cyan
	case priorityMedium:
		return logs.Yellow
	case priorityHigh:
		return logs.Red
	case priorityCritical:
		return logs.RedBold
	default:
		return rc.Dim
	}
}

func (rc renderColor) ForVerificationStatus(status verificationStatus) func(string) string {
	if !rc.enabled {
		return func(s string) string { return s }
	}
	switch status {
	case verificationConfirmed:
		return logs.Green
	case verificationNotConfirmed, verificationFailed:
		return logs.Red
	default:
		return logs.Yellow
	}
}

func (rc renderColor) ForAISkill(status string) func(string) string {
	if !rc.enabled {
		return func(s string) string { return s }
	}
	switch status {
	case "confirmed":
		return logs.Green
	case "not_confirmed":
		return rc.Dim
	case "info":
		return logs.Yellow
	default:
		return logs.Yellow
	}
}
