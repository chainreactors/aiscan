package scan

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	assetItemService     = "service"
	assetItemPath        = "path"
	assetItemFingerprint = "fingerprint"
	assetItemFinding     = "finding"
	assetItemNote        = "note"
	assetItemResponse    = "response"
	assetItemError       = "error"
)

var firstURLPattern = regexp.MustCompile(`https?://[^\s"'<>]+`)

type assetBucket struct {
	asset     Asset
	keys      map[string]struct{}
	itemIndex map[string]int
}

type assetBuilder struct {
	buckets []*assetBucket
	byKey   map[string]*assetBucket
}

func AggregateStructuredResult(result *StructuredResult) []Asset {
	if result == nil {
		return nil
	}

	builder := newAssetBuilder()
	for _, service := range result.Services {
		builder.addService(service)
	}
	for _, endpoint := range result.WebEndpoints {
		builder.addWebEndpoint(endpoint)
	}
	for _, endpoint := range result.WebProbes {
		builder.addWebEndpoint(endpoint)
	}
	for _, fingerprint := range result.Fingerprints {
		builder.addFingerprint(fingerprint)
	}
	for _, finding := range result.Risks {
		builder.addFinding(finding, assetItemFinding)
	}
	for _, finding := range result.Vulns {
		builder.addFinding(finding, assetItemFinding)
	}
	for _, finding := range result.AI {
		kind := assetItemNote
		if finding.Kind == string(findingAIResponse) {
			kind = assetItemResponse
		} else if finding.Status == string(verificationConfirmed) {
			kind = assetItemFinding
		}
		builder.addFinding(finding, kind)
	}
	for _, err := range result.Errors {
		builder.addError(err)
	}
	return builder.assets()
}

func newAssetBuilder() *assetBuilder {
	return &assetBuilder{byKey: make(map[string]*assetBucket)}
}

func (b *assetBuilder) addService(service StructuredService) {
	target := serviceAssetTarget(service)
	hostPort := ""
	if service.IP != "" && service.Port != "" {
		hostPort = service.IP + ":" + service.Port
	}
	keys := targetKeys(target, service.Target, service.Raw, hostPort)
	data := assetData(
		"ip", service.IP,
		"port", service.Port,
		"protocol", service.Protocol,
		"service", service.Service,
		"banner", service.Banner,
		"is_web", service.IsWeb,
	)
	item := AssetItem{
		Kind:    assetItemService,
		Source:  capGogoPortscan,
		Target:  service.Target,
		Title:   firstNonEmptyString(service.Service, service.Protocol, service.Banner),
		Summary: service.Banner,
		Tags:    compactStrings(service.Protocol, service.Service, service.Port),
		Data:    data,
		Raw:     service.Raw,
	}
	b.addItem(target, keys, "service|"+strings.Join(sortedStrings(keys), "|"), item)
}

func (b *assetBuilder) addWebEndpoint(endpoint StructuredWebEndpoint) {
	if endpoint.URL != "" && !strings.Contains(endpoint.URL, "://") {
		return
	}
	target := webAssetTarget(endpoint.URL)
	keys := targetKeys(target, endpoint.URL, endpoint.Raw)
	status := ""
	if endpoint.Status > 0 {
		status = strconv.Itoa(endpoint.Status)
	}
	path := webPath(endpoint.URL)
	data := assetData(
		"url", endpoint.URL,
		"path", path,
		"host_header", endpoint.HostHeader,
		"status", endpoint.Status,
		"length", endpoint.Length,
		"title", endpoint.Title,
		"fingers", endpoint.Fingers,
		"validated", isSprayValidated(endpoint.Source),
	)
	tags := append([]string{endpoint.Source}, endpoint.Fingers...)
	if isSprayValidated(endpoint.Source) {
		tags = append(tags, "validated")
	}
	item := AssetItem{
		Kind:    assetItemPath,
		Source:  endpoint.Source,
		Target:  endpoint.URL,
		Status:  status,
		Title:   endpoint.Title,
		Summary: path,
		Tags:    compactStrings(tags...),
		Data:    data,
		Raw:     endpoint.Raw,
	}
	identity := "path|" + canonicalKey(endpoint.URL) + "|host=" + strings.ToLower(endpoint.HostHeader)
	b.addItem(target, keys, identity, item)
}

// isSprayValidated returns true when the source capability is a spray
// pipeline stage. Spray results that reach the collector have already
// survived spray's baseline comparison (body-length + simhash fuzzy
// deduplication), so they represent pages that are structurally distinct
// from the site's default response — higher signal for the -F report.
func isSprayValidated(source string) bool {
	switch source {
	case capSprayCheck, capSprayCrawl, capSprayPlugins, capSprayBrute:
		return true
	default:
		return false
	}
}

func (b *assetBuilder) addFingerprint(fingerprint StructuredFingerprint) {
	target := assetTargetFromValues(fingerprint.Target)
	keys := targetKeys(target, fingerprint.Target)
	data := assetData(
		"name", fingerprint.Name,
		"focus", fingerprint.Focus,
	)
	item := AssetItem{
		Kind:   assetItemFingerprint,
		Source: fingerprint.Source,
		Target: fingerprint.Target,
		Title:  fingerprint.Name,
		Tags:   compactStrings(fingerprint.Source, fingerprint.Name),
		Data:   data,
	}
	identity := "fingerprint|" + canonicalKey(fingerprint.Target) + "|" + strings.ToLower(fingerprint.Name)
	b.addItem(target, keys, identity, item)
}

func (b *assetBuilder) addFinding(finding StructuredFinding, itemKind string) {
	target := assetTargetFromValues(finding.Target, finding.OriginalKey, finding.Raw, finding.Summary)
	keys := targetKeys(target, finding.Target, finding.OriginalKey, finding.Raw, finding.Summary)
	status := firstNonEmptyString(finding.Status, finding.Priority)
	if itemKind == assetItemFinding && status == "" {
		status = assetItemFinding
	}
	title := firstNonEmptyString(finding.Summary, finding.Kind)
	data := assetData(
		"kind", finding.Kind,
		"priority", finding.Priority,
		"status", finding.Status,
		"skill", finding.Skill,
		"source", finding.Source,
		"original_kind", finding.OriginalKind,
		"original_key", finding.OriginalKey,
		"evidence", finding.Evidence,
	)
	item := AssetItem{
		Kind:    itemKind,
		Source:  firstNonEmptyString(finding.Skill, finding.Source),
		Target:  finding.Target,
		Status:  status,
		Title:   title,
		Summary: firstNonEmptyString(finding.Summary, finding.Raw),
		Detail:  firstNonEmptyString(finding.Detail, finding.Evidence),
		Tags:    compactStrings(finding.Kind, finding.Priority, finding.Status, finding.Skill, finding.Source),
		Data:    data,
		Raw:     finding.Raw,
	}
	identity := strings.Join(compactStrings(
		itemKind,
		finding.Kind,
		finding.Target,
		finding.OriginalKind,
		finding.OriginalKey,
		finding.Skill,
		finding.Status,
		finding.Summary,
		finding.Raw,
	), "|")
	b.addItem(target, keys, identity, item)
}

func (b *assetBuilder) addError(err StructuredError) {
	keys := targetKeys("scan")
	item := AssetItem{
		Kind:    assetItemError,
		Source:  err.Source,
		Target:  "scan",
		Status:  assetItemError,
		Summary: err.Message,
		Data:    assetData("message", err.Message),
	}
	identity := "error|" + err.Source + "|" + err.Message
	b.addItem("Scan", keys, identity, item)
}

func (b *assetBuilder) addItem(target string, keys []string, identity string, item AssetItem) {
	target = firstNonEmptyString(target, item.Target, "Scan")
	if len(keys) == 0 {
		keys = targetKeys(target)
	}
	bucket := b.findBucket(keys)
	if bucket == nil {
		bucket = &assetBucket{
			asset: Asset{
				Target: target,
			},
			keys:      make(map[string]struct{}),
			itemIndex: make(map[string]int),
		}
		b.buckets = append(b.buckets, bucket)
	}
	bucket.asset.Target = preferredAssetTarget(bucket.asset.Target, target)
	for _, key := range keys {
		if key == "" {
			continue
		}
		bucket.keys[key] = struct{}{}
		b.byKey[key] = bucket
	}
	if identity == "" {
		identity = itemIdentity(item)
	}
	if existing, ok := bucket.itemIndex[identity]; ok {
		bucket.asset.Items[existing] = mergeAssetItem(bucket.asset.Items[existing], item)
		return
	}
	bucket.itemIndex[identity] = len(bucket.asset.Items)
	bucket.asset.Items = append(bucket.asset.Items, normalizeAssetItem(item))
}

func (b *assetBuilder) findBucket(keys []string) *assetBucket {
	for _, key := range sortedStrings(keys) {
		if bucket := b.byKey[key]; bucket != nil {
			return bucket
		}
	}
	return nil
}

func (b *assetBuilder) assets() []Asset {
	out := make([]Asset, 0, len(b.buckets))
	for _, bucket := range b.buckets {
		asset := bucket.asset
		sortAssetItems(asset.Items)
		asset.Target = firstNonEmptyString(asset.Target, "Scan")
		asset.Key = preferredAssetKey(bucket.keys, asset.Target)
		asset.ID = "asset:" + asset.Key
		asset.Title = deriveAssetTitle(asset)
		asset.Status = deriveAssetStatus(asset.Items)
		out = append(out, asset)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Key < out[j].Key
	})
	return out
}

func serviceAssetTarget(service StructuredService) string {
	if service.IsWeb {
		scheme := strings.ToLower(strings.TrimSpace(service.Protocol))
		if !strings.HasPrefix(scheme, "http") {
			if service.Port == "443" {
				scheme = "https"
			} else {
				scheme = "http"
			}
		}
		if service.IP != "" && service.Port != "" {
			return scheme + "://" + service.IP + ":" + service.Port
		}
	}
	return assetTargetFromValues(service.Target, service.Raw)
}

func webAssetTarget(rawURL string) string {
	if origin := urlOrigin(rawURL); origin != "" {
		return origin
	}
	return rawURL
}

func assetTargetFromValues(values ...string) string {
	for _, value := range values {
		if origin := urlOrigin(value); origin != "" {
			return origin
		}
		if first := firstURL(value); first != "" {
			if origin := urlOrigin(first); origin != "" {
				return origin
			}
			return first
		}
	}
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return "Scan"
}

func targetKeys(values ...string) []string {
	seen := make(map[string]struct{})
	for _, value := range values {
		addTargetKeys(seen, value)
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func addTargetKeys(keys map[string]struct{}, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	addCanonicalKey(keys, value)
	withoutHost := strings.Split(value, "|host=")[0]
	addCanonicalKey(keys, withoutHost)
	if first := firstURL(withoutHost); first != "" {
		if canonicalKey(first) != canonicalKey(withoutHost) {
			addTargetKeys(keys, first)
		}
	}
	if origin := urlOrigin(withoutHost); origin != "" {
		addCanonicalKey(keys, origin)
	}
	if host := urlHost(withoutHost); host != "" {
		addCanonicalKey(keys, host)
	}
	if normalized := normalizedURL(withoutHost); normalized != "" {
		addCanonicalKey(keys, normalized)
	}
}

func addCanonicalKey(keys map[string]struct{}, value string) {
	if key := canonicalKey(value); key != "" {
		keys[key] = struct{}{}
	}
}

func canonicalKey(value string) string {
	value = strings.Trim(value, " \t\r\n\"'<>[](),")
	value = strings.TrimRight(value, "/")
	if value == "" {
		return ""
	}
	return strings.ToLower(value)
}

func normalizedURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	path := strings.TrimRight(parsed.EscapedPath(), "/")
	if path == "" || path == "/" {
		path = ""
	}
	query := ""
	if parsed.RawQuery != "" {
		query = "?" + parsed.RawQuery
	}
	return strings.ToLower(parsed.Scheme + "://" + stripDefaultPort(parsed) + path + query)
}

func urlOrigin(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Scheme + "://" + stripDefaultPort(parsed))
}

func urlHost(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(stripDefaultPort(parsed))
}

func stripDefaultPort(u *url.URL) string {
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		return host
	}
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		return host
	}
	return host + ":" + port
}

func firstURL(value string) string {
	if value == "" {
		return ""
	}
	match := firstURLPattern.FindString(value)
	return strings.Trim(match, " \t\r\n\"'<>[](),")
}

func webPath(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return firstNonEmptyString(rawURL, "/")
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	if parsed.RawQuery != "" {
		path += "?" + parsed.RawQuery
	}
	return path
}

func preferredAssetTarget(current, next string) string {
	current = strings.TrimSpace(current)
	next = strings.TrimSpace(next)
	if current == "" || strings.EqualFold(current, "scan") {
		return next
	}
	if next == "" {
		return current
	}
	if urlOrigin(next) != "" && urlOrigin(current) == "" {
		return next
	}
	return current
}

func preferredAssetKey(keys map[string]struct{}, target string) string {
	targetKey := canonicalKey(target)
	if targetKey != "" {
		if _, ok := keys[targetKey]; ok {
			return targetKey
		}
	}
	sorted := make([]string, 0, len(keys))
	for key := range keys {
		sorted = append(sorted, key)
	}
	sort.Strings(sorted)
	if len(sorted) > 0 {
		return sorted[0]
	}
	return canonicalKey(firstNonEmptyString(target, "scan"))
}

func deriveAssetTitle(asset Asset) string {
	if title := firstItemText(asset.Items, func(item AssetItem) bool {
		return (item.Kind == assetItemFinding || item.Kind == assetItemNote) && item.Status == string(verificationConfirmed)
	}); title != "" {
		return title
	}
	if title := firstItemText(asset.Items, func(item AssetItem) bool {
		return item.Kind == assetItemNote && item.Status == "info"
	}); title != "" {
		return title
	}
	if title := firstItemText(asset.Items, func(item AssetItem) bool {
		return item.Kind == assetItemFinding || item.Kind == assetItemNote
	}); title != "" {
		return title
	}
	if title := firstItemText(asset.Items, func(item AssetItem) bool {
		return item.Kind == assetItemPath && item.Title != ""
	}); title != "" {
		return title
	}
	for _, item := range asset.Items {
		if item.Kind != assetItemService || item.Data == nil {
			continue
		}
		if banner, ok := item.Data["banner"].(string); ok && strings.TrimSpace(banner) != "" {
			return strings.TrimSpace(banner)
		}
	}
	return asset.Target
}

func firstItemText(items []AssetItem, match func(AssetItem) bool) string {
	for _, item := range items {
		if !match(item) {
			continue
		}
		if text := firstNonEmptyString(item.Title, item.Summary); text != "" {
			return text
		}
	}
	return ""
}

func deriveAssetStatus(items []AssetItem) string {
	bestStatus := ""
	bestRank := 0
	for _, item := range items {
		status := item.Status
		if item.Kind == assetItemFinding && status == "" {
			status = assetItemFinding
		}
		if item.Kind == assetItemError && status == "" {
			status = assetItemError
		}
		rank := assetStatusRank(item.Kind, status)
		if rank > bestRank {
			bestRank = rank
			bestStatus = status
		}
	}
	return bestStatus
}

func assetStatusRank(kind, status string) int {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case string(verificationConfirmed):
		return 100
	case string(priorityCritical):
		return 95
	case string(priorityHigh):
		return 90
	case assetItemFinding:
		return 85
	case string(priorityMedium):
		return 70
	case "info":
		return 60
	case string(priorityLow):
		return 50
	case string(verificationInconclusive):
		return 40
	case string(verificationNotConfirmed):
		return 30
	case string(verificationFailed), assetItemError:
		return 20
	}
	if kind == assetItemFinding {
		return 85
	}
	if kind == assetItemError {
		return 20
	}
	if kind == assetItemResponse {
		return 10
	}
	return 0
}

func sortAssetItems(items []AssetItem) {
	sort.SliceStable(items, func(i, j int) bool {
		ri, rj := assetItemRank(items[i].Kind), assetItemRank(items[j].Kind)
		if ri != rj {
			return ri < rj
		}
		vi, vj := hasTag(items[i].Tags, "validated"), hasTag(items[j].Tags, "validated")
		if vi != vj {
			return vi
		}
		left := fmt.Sprintf("%s|%s|%s", items[i].Target, items[i].Title, items[i].Summary)
		right := fmt.Sprintf("%s|%s|%s", items[j].Target, items[j].Title, items[j].Summary)
		return left < right
	})
}

func hasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}

func assetItemRank(kind string) int {
	switch kind {
	case assetItemService:
		return 10
	case assetItemFingerprint:
		return 20
	case assetItemFinding:
		return 30
	case assetItemNote:
		return 40
	case assetItemResponse:
		return 45
	case assetItemPath:
		return 50
	case assetItemError:
		return 60
	default:
		return 90
	}
}

func mergeAssetItem(current, next AssetItem) AssetItem {
	current.Kind = firstNonEmptyString(current.Kind, next.Kind)
	current.Source = firstNonEmptyString(current.Source, next.Source)
	current.Target = firstNonEmptyString(current.Target, next.Target)
	current.Status = firstNonEmptyString(current.Status, next.Status)
	current.Title = firstNonEmptyString(current.Title, next.Title)
	current.Summary = firstNonEmptyString(current.Summary, next.Summary)
	current.Detail = firstNonEmptyString(current.Detail, next.Detail)
	current.Raw = firstNonEmptyString(current.Raw, next.Raw)
	current.Tags = compactStrings(append(current.Tags, next.Tags...)...)
	if current.Data == nil {
		current.Data = next.Data
	} else {
		for key, value := range next.Data {
			if isEmptyAssetData(value) {
				continue
			}
			if isEmptyAssetData(current.Data[key]) {
				current.Data[key] = value
			}
		}
	}
	return normalizeAssetItem(current)
}

func normalizeAssetItem(item AssetItem) AssetItem {
	item.Kind = strings.TrimSpace(item.Kind)
	item.Source = strings.TrimSpace(item.Source)
	item.Target = strings.TrimSpace(item.Target)
	item.Status = strings.TrimSpace(item.Status)
	item.Title = strings.TrimSpace(item.Title)
	item.Summary = strings.TrimSpace(item.Summary)
	item.Detail = strings.TrimSpace(item.Detail)
	item.Raw = strings.TrimSpace(item.Raw)
	item.Tags = compactStrings(item.Tags...)
	if len(item.Data) == 0 {
		item.Data = nil
	}
	return item
}

func itemIdentity(item AssetItem) string {
	return strings.Join(compactStrings(item.Kind, item.Source, item.Target, item.Status, item.Title, item.Summary, item.Raw), "|")
}

func assetData(values ...any) map[string]any {
	data := make(map[string]any)
	for i := 0; i+1 < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok || key == "" || isEmptyAssetData(values[i+1]) {
			continue
		}
		data[key] = values[i+1]
	}
	if len(data) == 0 {
		return nil
	}
	return data
}

func isEmptyAssetData(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	case int:
		return v == 0
	case bool:
		return !v
	case []string:
		return len(compactStrings(v...)) == 0
	default:
		return false
	}
}

func compactStrings(values ...string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
