package scan

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/chainreactors/parsers"
)

const (
	ansiReset  = "\x1b[0m"
	ansiDim    = "\x1b[2m"
	ansiCyan   = "\x1b[36m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
	ansiBold   = "\x1b[1m"
)

type outputOptions struct {
	Color bool
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func stripANSI(value string) string {
	return ansiPattern.ReplaceAllString(value, "")
}

func colorize(enabled bool, code, value string) string {
	if !enabled || value == "" {
		return value
	}
	return code + value + ansiReset
}

func colorForPriority(priority priority) string {
	switch priority {
	case priorityLow:
		return ansiCyan
	case priorityMedium:
		return ansiYellow
	case priorityHigh:
		return ansiRed
	case priorityCritical:
		return ansiBold + ansiRed
	default:
		return ansiDim
	}
}

func formatEventLine(event event, opts outputOptions) string {
	source := event.Source
	if source == "" || source == "input" {
		source = "scan"
	}
	prefix := colorize(opts.Color, ansiDim, "["+source+"]")

	switch event.Kind {
	case eventTarget:
		switch target := event.Target.(type) {
		case serviceTarget:
			if target.Result == nil {
				return ""
			}
			return sanitizeOutputLine(fmt.Sprintf("%s %s", prefix, formatServiceResult(target.Result)), opts)
		case webTarget:
			if target.URL == "" {
				return ""
			}
			line := fmt.Sprintf("%s %s %s", prefix, target.URL, colorize(opts.Color, ansiGreen, "type=web"))
			if target.HostHeader != "" {
				line += " " + colorize(opts.Color, ansiDim, formatKV("host", target.HostHeader))
			}
			return sanitizeOutputLine(line, opts)
		case webProbeTarget:
			if !reportableSprayResult(target.Result) {
				return ""
			}
			return sanitizeOutputLine(fmt.Sprintf("%s %s", prefix, formatSprayResult(target.Result)), opts)
		}
	case eventFinding:
		switch finding := event.Finding.(type) {
		case fingerprintFinding:
			names := parsers.NormalizeNames(finding.Fingers)
			if len(names) == 0 {
				return ""
			}
			return formatPriorityLine(prefix, finding.Priority(), "fingerprint", finding.Target, []string{
				formatKV("names", strings.Join(names, ",")),
			}, opts)
		case weakpassFinding:
			if finding.Result == nil {
				return ""
			}
			return formatPriorityLine(prefix, finding.Priority(), "weakpass", finding.Result.URI(), weakpassFields(finding.Result), opts)
		case vulnFinding:
			if finding.Message == "" {
				return ""
			}
			return formatPriorityLine(prefix, finding.Priority(), "vuln", vulnTarget(finding.Message), []string{
				formatKV("evidence", finding.Message),
			}, opts)
		case verificationFinding:
			return formatPriorityLine(prefix, finding.Priority(), "verify", finding.Target, verificationFields(finding), opts)
		}
	case eventError:
		if event.Error.Message == "" {
			return ""
		}
		return sanitizeOutputLine(fmt.Sprintf("%s %s", prefix, colorize(opts.Color, ansiRed, "type=error")+" "+formatKV("message", event.Error.Message)), opts)
	}
	return ""
}

func formatPriorityLine(prefix string, priority priority, label, target string, fields []string, opts outputOptions) string {
	priorityText := strings.ToUpper(string(priority))
	if priorityText == "" {
		priorityText = "INFO"
	}
	parts := []string{prefix}
	if target != "" {
		parts = append(parts, target)
	}
	parts = append(parts,
		colorize(opts.Color, colorForPriority(priority), "type="+label),
		colorize(opts.Color, colorForPriority(priority), "level="+strings.ToLower(priorityText)),
	)
	parts = append(parts, fields...)
	return sanitizeOutputLine(strings.Join(parts, " "), opts)
}

func sanitizeOutputLine(line string, opts outputOptions) string {
	line = strings.TrimSpace(line)
	if !opts.Color {
		line = stripANSI(line)
	}
	return line
}

func formatServiceResult(result *parsers.GOGOResult) string {
	target := result.GetTarget()
	if result.IsHttp() {
		target = result.GetBaseURL()
	}
	parts := []string{target, "type=service"}
	parts = appendNonEmptyKV(parts, "proto", result.Protocol)
	parts = appendNonEmptyKV(parts, "status", result.Status)
	parts = appendNonEmptyKV(parts, "midware", result.Midware)
	parts = appendNonEmptyKV(parts, "host", result.Host)
	parts = appendNonEmptyKV(parts, "title", result.Title)
	if frameworks := strings.Trim(result.Frameworks.String(), "|"); frameworks != "" {
		parts = append(parts, formatKV("frameworks", frameworks))
	}
	if vulns := strings.TrimSpace(result.Vulns.String()); vulns != "" {
		parts = append(parts, formatKV("vulns", vulns))
	}
	if extract := strings.TrimSpace(result.GetExtractStat()); extract != "" {
		parts = append(parts, formatKV("extracts", extract))
	}
	if result.Timing > 0 {
		parts = append(parts, formatKV("time", fmt.Sprintf("%dms", result.Timing)))
	}
	return strings.Join(parts, " ")
}

func formatSprayResult(result *parsers.SprayResult) string {
	parts := []string{result.UrlString, "type=web_probe"}
	parts = appendNonEmptyKV(parts, "probe", result.Source.Name())
	if result.Status > 0 {
		parts = append(parts, formatKV("status", strconv.Itoa(result.Status)))
	}
	parts = append(parts, formatKV("length", strconv.Itoa(result.BodyLength)))
	if result.ExceedLength {
		parts = append(parts, "exceed=true")
	}
	if result.Spended > 0 {
		parts = append(parts, formatKV("time", fmt.Sprintf("%dms", result.Spended)))
	}
	parts = appendNonEmptyKV(parts, "host", result.Host)
	parts = appendNonEmptyKV(parts, "redirect", result.RedirectURL)
	parts = appendNonEmptyKV(parts, "title", result.Title)
	if result.Distance != 0 {
		parts = append(parts, formatKV("sim", strconv.Itoa(int(result.Distance))))
	}
	parts = appendNonEmptyKV(parts, "reason", result.Reason)
	if frameworks := strings.TrimSpace(result.Get("frame")); frameworks != "" {
		parts = append(parts, formatKV("frameworks", frameworks))
	}
	if extracts := strings.TrimSpace(result.Extracteds.String()); extracts != "" {
		parts = append(parts, formatKV("extracts", extracts))
	}
	return strings.Join(parts, " ")
}

func weakpassFields(result *parsers.ZombieResult) []string {
	fields := make([]string, 0, 4)
	fields = appendNonEmptyKV(fields, "user", result.Username)
	fields = appendNonEmptyKV(fields, "pass", result.Password)
	fields = appendNonEmptyKV(fields, "service", result.Service)
	fields = append(fields, formatKV("mod", result.Mod.String()))
	return fields
}

func verificationFields(finding verificationFinding) []string {
	parts := []string{formatKV("status", string(finding.Status))}
	if finding.OriginalPriority != "" {
		parts = append(parts, formatKV("priority", string(finding.OriginalPriority)))
	}
	if finding.OriginalKind != "" {
		parts = append(parts, formatKV("finding", string(finding.OriginalKind)))
	}
	parts = appendNonEmptyKV(parts, "summary", finding.Summary)
	parts = appendNonEmptyKV(parts, "evidence", finding.Evidence)
	return parts
}

func appendNonEmptyKV(parts []string, key, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return parts
	}
	return append(parts, formatKV(key, value))
}

func formatKV(key, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return key + "="
	}
	if needsQuoting(value) {
		return key + "=" + strconv.Quote(value)
	}
	return key + "=" + value
}

func needsQuoting(value string) bool {
	return strings.ContainsAny(value, " \t\r\n\"")
}
