package output

import (
	"github.com/chainreactors/parsers"
)

const (
	ANSIReset   = "\033[0m"
	ANSIBold    = "\033[1m"
	ANSIDim     = "\033[90m"
	ANSIRed     = "\033[38;5;203m"
	ANSIGreen   = "\033[38;5;114m"
	ANSIYellow  = "\033[38;5;214m"
	ANSIBlue    = "\033[38;5;75m"
	ANSIMagenta = "\033[38;5;177m"
	ANSICyan    = "\033[38;5;81m"
	ANSIBorder  = "\033[38;5;240m"
)

type Color struct {
	Enabled bool
}

func NewColor(enabled bool) Color {
	return Color{Enabled: enabled}
}

func (c Color) Code(code string) string {
	if !c.Enabled {
		return ""
	}
	return code
}

func (c Color) Green(s string) string {
	if !c.Enabled {
		return s
	}
	return ANSIGreen + s + ANSIReset
}

func (c Color) GreenBold(s string) string {
	if !c.Enabled {
		return s
	}
	return ANSIBold + ANSIGreen + s + ANSIReset
}

func (c Color) Red(s string) string {
	if !c.Enabled {
		return s
	}
	return ANSIRed + s + ANSIReset
}

func (c Color) RedBold(s string) string {
	if !c.Enabled {
		return s
	}
	return ANSIBold + ANSIRed + s + ANSIReset
}

func (c Color) Yellow(s string) string {
	if !c.Enabled {
		return s
	}
	return ANSIYellow + s + ANSIReset
}

func (c Color) YellowBold(s string) string {
	if !c.Enabled {
		return s
	}
	return ANSIBold + ANSIYellow + s + ANSIReset
}

func (c Color) Cyan(s string) string {
	if !c.Enabled {
		return s
	}
	return ANSICyan + s + ANSIReset
}

func (c Color) Blue(s string) string {
	if !c.Enabled {
		return s
	}
	return ANSIBlue + s + ANSIReset
}

func (c Color) Magenta(s string) string {
	if !c.Enabled {
		return s
	}
	return ANSIMagenta + s + ANSIReset
}

func (c Color) Bold(s string) string {
	if !c.Enabled {
		return s
	}
	return ANSIBold + s + ANSIReset
}

func (c Color) Dim(s string) string {
	if !c.Enabled {
		return s
	}
	return ANSIDim + s + ANSIReset
}

func (c Color) Status(s string) string {
	if !c.Enabled {
		return s
	}
	return parsers.RenderStatus(s)
}

func (c Color) ForPriority(p string) func(string) string {
	if !c.Enabled {
		return func(s string) string { return s }
	}
	switch p {
	case "low":
		return c.Cyan
	case "medium":
		return c.Yellow
	case "high":
		return c.Red
	case "critical":
		return c.RedBold
	default:
		return c.Dim
	}
}

func (c Color) ForStatus(status string) func(string) string {
	if !c.Enabled {
		return func(s string) string { return s }
	}
	switch status {
	case "confirmed":
		return c.Green
	case "not_confirmed", "failed":
		return c.Red
	case "info":
		return c.Yellow
	default:
		return c.Yellow
	}
}
