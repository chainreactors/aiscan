package web

import (
	"net"
	"strconv"
	"strings"
	"time"

	scanpkg "github.com/chainreactors/aiscan/pkg/tools/scan"
)

type partialStructuredBuilder struct {
	target    string
	startedAt time.Time
	services  map[string]scanpkg.StructuredService
	endpoints map[string]scanpkg.StructuredWebEndpoint
	probes    map[string]scanpkg.StructuredWebEndpoint
	fingers   map[string]scanpkg.StructuredFingerprint
	risks     map[string]scanpkg.StructuredFinding
	vulns     map[string]scanpkg.StructuredFinding
	ai        map[string]scanpkg.StructuredFinding
	errors    map[string]scanpkg.StructuredError
	lineCount int
}

func newPartialStructuredBuilder(target string, startedAt time.Time) *partialStructuredBuilder {
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	return &partialStructuredBuilder{
		target:    target,
		startedAt: startedAt,
		services:  make(map[string]scanpkg.StructuredService),
		endpoints: make(map[string]scanpkg.StructuredWebEndpoint),
		probes:    make(map[string]scanpkg.StructuredWebEndpoint),
		fingers:   make(map[string]scanpkg.StructuredFingerprint),
		risks:     make(map[string]scanpkg.StructuredFinding),
		vulns:     make(map[string]scanpkg.StructuredFinding),
		ai:        make(map[string]scanpkg.StructuredFinding),
		errors:    make(map[string]scanpkg.StructuredError),
	}
}

func (b *partialStructuredBuilder) ObserveLine(line string) {
	if b == nil {
		return
	}
	label, body, ok := splitProgressLine(line)
	if !ok {
		return
	}
	b.lineCount++
	switch {
	case label == "service":
		b.observeService(body, line)
	case label == "web":
		b.observeWeb(body, line)
	case label == "fingerprint":
		b.observeFingerprint(body, line)
	case label == "risk":
		b.observeFinding(b.risks, "weakpass-finding", "high", body, line)
	case label == "vuln":
		b.observeFinding(b.vulns, "vuln-finding", "high", body, line)
	case label == "error":
		key := canonicalPartialKey(body)
		b.errors[key] = scanpkg.StructuredError{Message: body}
	case isAILabel(label):
		b.observeAI(label, body, line)
	}
}

func (b *partialStructuredBuilder) Result(now time.Time) *scanpkg.StructuredResult {
	if b == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	result := &scanpkg.StructuredResult{
		Summary: scanpkg.StructuredSummary{
			Targets:      boolCount(strings.TrimSpace(b.target) != ""),
			Services:     len(b.services),
			Webs:         len(b.endpoints),
			Probes:       len(b.probes),
			Fingerprints: len(b.fingers),
			Risks:        len(b.risks),
			Vulns:        len(b.vulns),
			Verified:     partialVerifiedCount(b.ai),
			Errors:       len(b.errors),
			Requests:     int64(len(b.probes)),
			Tasks:        int64(b.lineCount),
			Duration:     now.Sub(b.startedAt).Round(time.Millisecond).String(),
			StartedAt:    b.startedAt,
		},
	}
	result.Services = mapValues(b.services)
	result.WebEndpoints = mapValues(b.endpoints)
	result.WebProbes = mapValues(b.probes)
	result.Fingerprints = mapValues(b.fingers)
	result.Risks = mapValues(b.risks)
	result.Vulns = mapValues(b.vulns)
	result.AI = mapValues(b.ai)
	result.Errors = mapValues(b.errors)
	result.Assets = scanpkg.AggregateStructuredResult(result)
	return result
}

func (b *partialStructuredBuilder) observeService(body, raw string) {
	fields := progressFields(body)
	if len(fields) == 0 {
		return
	}
	target := fields[0]
	host, port := splitHostPortLoose(target)
	service := ""
	protocol := ""
	if len(fields) > 2 {
		protocol = fields[2]
		service = fields[2]
	}
	item := scanpkg.StructuredService{
		Target:   target,
		IP:       host,
		Port:     port,
		Protocol: protocol,
		Service:  service,
		Banner:   strings.Join(fields[1:], " "),
		Raw:      strings.TrimSpace(strings.TrimPrefix(raw, "[service]")),
	}
	b.services[canonicalPartialKey(target)] = item
}

func (b *partialStructuredBuilder) observeWeb(body, raw string) {
	fields := progressFields(body)
	if len(fields) == 0 {
		return
	}
	endpoint := scanpkg.StructuredWebEndpoint{
		URL:    fields[0],
		Source: "stream",
		Raw:    strings.TrimSpace(strings.TrimPrefix(raw, "[web]")),
	}
	if len(fields) > 1 && isStatusCode(fields[1]) {
		endpoint.Status, _ = strconv.Atoi(fields[1])
		if len(fields) > 2 && isPositiveInt(fields[2]) {
			endpoint.Length, _ = strconv.Atoi(fields[2])
		}
		endpoint.Title = titleFromWebFields(fields)
		endpoint.Fingers = fingersFromFields(fields)
		b.probes[webProbeKey(endpoint)] = endpoint
		for _, finger := range endpoint.Fingers {
			b.addFingerprint(endpoint.URL, finger, endpoint.Source, false)
		}
		return
	}
	b.endpoints[canonicalPartialKey(endpoint.URL)] = endpoint
}

func (b *partialStructuredBuilder) observeFingerprint(body, raw string) {
	fields := progressFields(body)
	if len(fields) == 0 {
		return
	}
	target := fields[0]
	for _, finger := range fingersFromFields(fields[1:]) {
		b.addFingerprint(target, finger, "stream", true)
	}
	if len(fields) == 1 {
		b.addFingerprint(target, strings.TrimSpace(strings.TrimPrefix(raw, "[fingerprint] "+target)), "stream", true)
	}
}

func (b *partialStructuredBuilder) addFingerprint(target, name, source string, focus bool) {
	name = strings.TrimSpace(name)
	if target == "" || name == "" {
		return
	}
	key := canonicalPartialKey(target) + "|" + strings.ToLower(name) + "|" + source
	b.fingers[key] = scanpkg.StructuredFingerprint{Target: target, Name: name, Source: source, Focus: focus}
}

func (b *partialStructuredBuilder) observeFinding(bucket map[string]scanpkg.StructuredFinding, kind, priority, body, raw string) {
	fields := progressFields(body)
	target := firstField(fields)
	summary := strings.TrimSpace(body)
	if len(fields) > 1 {
		summary = strings.Join(fields[1:], " ")
	}
	item := scanpkg.StructuredFinding{
		Kind:     kind,
		Target:   target,
		Priority: priority,
		Summary:  summary,
		Raw:      strings.TrimSpace(raw),
	}
	bucket[canonicalPartialKey(kind+"|"+raw)] = item
}

func (b *partialStructuredBuilder) observeAI(label, body, raw string) {
	fields := progressFields(body)
	target := firstField(fields)
	status := aiStatusFromLabel(label)
	skill := aiSkillFromLabel(label)
	kind := "ai-skill"
	if strings.HasSuffix(label, ":response") || strings.HasSuffix(label, ":failed") {
		kind = "ai-response"
	}
	if len(fields) > 1 && looksLikeAIStatus(fields[1]) {
		status = fields[1]
	}
	summary := ""
	detail := ""
	start := 1
	if len(fields) > 1 && looksLikeAIStatus(fields[1]) {
		start = 2
	}
	if len(fields) > start {
		summary = fields[start]
	}
	if len(fields) > start+1 {
		detail = strings.Join(fields[start+1:], " ")
	}
	item := scanpkg.StructuredFinding{
		Kind:     kind,
		Target:   target,
		Priority: aiPriority(status, kind),
		Status:   status,
		Summary:  summary,
		Detail:   detail,
		Skill:    skill,
		Source:   "stream",
		Raw:      strings.TrimSpace(raw),
	}
	b.ai[canonicalPartialKey(label+"|"+raw)] = item
}

func splitProgressLine(line string) (label, body string, ok bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "[") {
		return "", "", false
	}
	end := strings.IndexByte(line, ']')
	if end <= 1 {
		return "", "", false
	}
	label = strings.ToLower(strings.TrimSpace(line[1:end]))
	body = strings.TrimSpace(line[end+1:])
	return label, body, label != ""
}

func progressFields(value string) []string {
	var fields []string
	var b strings.Builder
	inQuote := false
	escaped := false
	flush := func() {
		if b.Len() == 0 {
			return
		}
		fields = append(fields, b.String())
		b.Reset()
	}
	for _, r := range value {
		switch {
		case escaped:
			b.WriteRune(r)
			escaped = false
		case r == '\\' && inQuote:
			escaped = true
		case r == '"':
			inQuote = !inQuote
		case (r == ' ' || r == '\t') && !inQuote:
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return fields
}

func titleFromWebFields(fields []string) string {
	if len(fields) <= 4 {
		return ""
	}
	var parts []string
	for _, field := range fields[4:] {
		if strings.HasPrefix(field, "[") || strings.HasPrefix(field, "sim:") {
			break
		}
		parts = append(parts, field)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func fingersFromFields(fields []string) []string {
	var out []string
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if !strings.HasPrefix(field, "[") || !strings.HasSuffix(field, "]") {
			continue
		}
		field = strings.TrimSuffix(strings.TrimPrefix(field, "["), "]")
		for _, part := range strings.Split(field, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return uniqueStrings(out)
}

func splitHostPortLoose(target string) (string, string) {
	host, port, err := net.SplitHostPort(target)
	if err == nil {
		return host, port
	}
	idx := strings.LastIndexByte(target, ':')
	if idx <= 0 || idx == len(target)-1 {
		return target, ""
	}
	return target[:idx], target[idx+1:]
}

func webProbeKey(endpoint scanpkg.StructuredWebEndpoint) string {
	return canonicalPartialKey(endpoint.URL) + "|" + endpoint.Source + "|" + strconv.Itoa(endpoint.Status) + "|" + endpoint.Title
}

func canonicalPartialKey(value string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(value), "/"))
}

func isStatusCode(value string) bool {
	if len(value) != 3 {
		return false
	}
	n, err := strconv.Atoi(value)
	return err == nil && n >= 100 && n <= 599
}

func isPositiveInt(value string) bool {
	n, err := strconv.Atoi(value)
	return err == nil && n >= 0
}

func isAILabel(label string) bool {
	base := aiSkillFromLabel(label)
	return base == "ai" || base == "verify" || base == "sniper" || base == "deep"
}

func aiSkillFromLabel(label string) string {
	base, _, _ := strings.Cut(label, ":")
	if base == "ai" {
		return "verify"
	}
	return base
}

func aiStatusFromLabel(label string) string {
	_, suffix, ok := strings.Cut(label, ":")
	if !ok {
		return ""
	}
	switch suffix {
	case "verified":
		return "confirmed"
	case "rejected":
		return "not_confirmed"
	case "response":
		return "response"
	case "failed":
		return "failed"
	default:
		return suffix
	}
}

func looksLikeAIStatus(value string) bool {
	switch value {
	case "confirmed", "not_confirmed", "inconclusive", "info", "failed", "response":
		return true
	default:
		return false
	}
}

func aiPriority(status, kind string) string {
	if kind == "ai-response" {
		return "low"
	}
	if status == "confirmed" {
		return "high"
	}
	return "medium"
}

func partialVerifiedCount(ai map[string]scanpkg.StructuredFinding) int {
	count := 0
	for _, item := range ai {
		if item.Status == "confirmed" {
			count++
		}
	}
	return count
}

func firstField(fields []string) string {
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func mapValues[T any](values map[string]T) []T {
	out := make([]T, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		key := strings.ToLower(strings.TrimSpace(value))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, strings.TrimSpace(value))
	}
	return out
}
