package scan

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func formatAssetReport(result *StructuredResult, color bool) string {
	if result == nil {
		return "Assets: 0 total\n"
	}
	rc := newRenderColor(color)

	var sb strings.Builder
	fmt.Fprintf(&sb, "Assets: %d total\n", len(result.Assets))
	fmt.Fprintf(&sb, "Summary: %d target(s), %d service(s), %d web endpoint(s), %d probe(s), %d fingerprint(s), %d finding(s), %d AI item(s), %d error(s), %s\n\n",
		result.Summary.Targets,
		result.Summary.Services,
		result.Summary.Webs,
		result.Summary.Probes,
		result.Summary.Fingerprints,
		result.Summary.Risks+result.Summary.Vulns,
		len(result.AI),
		result.Summary.Errors,
		result.Summary.Duration,
	)

	if len(result.Assets) == 0 {
		return sb.String()
	}
	for i, asset := range result.Assets {
		title := firstNonEmptyString(asset.Title, asset.Target, asset.Key)
		fmt.Fprintf(&sb, "%d. %s\n", i+1, rc.GreenBold(title))
		if asset.Target != "" && asset.Target != title {
			fmt.Fprintf(&sb, "   target: %s\n", asset.Target)
		}
		if asset.Status != "" {
			fmt.Fprintf(&sb, "   status: %s\n", asset.Status)
		}
		writeAssetTopItems(&sb, asset.Items, rc)
		writeAssetSitemap(&sb, asset, rc)
		if i < len(result.Assets)-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func writeAssetTopItems(sb *strings.Builder, items []AssetItem, rc renderColor) {
	for _, item := range items {
		switch item.Kind {
		case assetItemPath:
			continue
		case assetItemService:
			line := strings.Join(compactStrings(
				assetDataString(item.Data, "protocol"),
				assetDataString(item.Data, "service"),
				assetDataString(item.Data, "port"),
			), " ")
			if line == "" {
				line = firstNonEmptyString(item.Title, item.Target, item.Raw)
			}
			fmt.Fprintf(sb, "   %s %s\n", rc.Cyan("service:"), line)
		case assetItemFingerprint:
			name := firstNonEmptyString(item.Title, item.Summary, item.Target)
			fmt.Fprintf(sb, "   %s %s\n", rc.Cyan("fingerprint:"), name)
		case assetItemFinding, assetItemNote, assetItemResponse:
			line := firstNonEmptyString(item.Summary, item.Title, item.Detail, item.Raw)
			if item.Status != "" {
				line = rc.Yellow("["+item.Status+"]") + " " + line
			}
			label := firstNonEmptyString(item.Source, item.Kind)
			fmt.Fprintf(sb, "   %s %s\n", rc.Yellow(label+":"), line)
			if item.Detail != "" && item.Detail != line && !strings.Contains(line, item.Detail) {
				for _, dl := range strings.Split(strings.TrimSpace(item.Detail), "\n") {
					if dl = strings.TrimSpace(dl); dl != "" {
						fmt.Fprintf(sb, "      %s\n", rc.Dim(dl))
					}
				}
			}
		case assetItemError:
			fmt.Fprintf(sb, "   %s %s\n", rc.Red("error:"), item.Summary)
		}
	}
}

// --- sitemap rendering ---

type sitemapEntry struct {
	path      string
	status    string
	length    int
	title     string
	fingers   []string
	validated bool
}

type sitemapNode struct {
	segment     string
	status      string
	length      int
	title       string
	fingers     []string
	validated   bool
	isLeaf      bool
	annotations []string
	children    []*sitemapNode
}

func writeAssetSitemap(sb *strings.Builder, asset Asset, rc renderColor) {
	var entries []sitemapEntry
	for _, item := range asset.Items {
		if item.Kind != assetItemPath {
			continue
		}
		p := firstNonEmptyString(assetDataString(item.Data, "path"), webPath(item.Target), item.Target)
		if p == "" {
			continue
		}
		entries = append(entries, sitemapEntry{
			path:      p,
			status:    item.Status,
			length:    assetDataInt(item.Data, "length"),
			title:     item.Title,
			fingers:   assetDataStrings(item.Data, "fingers"),
			validated: hasTag(item.Tags, "validated"),
		})
	}
	if len(entries) == 0 {
		return
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })

	sb.WriteString("   sitemap:\n")
	root := buildSitemapTree(entries)
	attachAnnotations(root, collectAnnotations(asset))
	renderNode(sb, root, "   ", true, rc)
}

func buildSitemapTree(entries []sitemapEntry) *sitemapNode {
	root := &sitemapNode{segment: "/"}
	for _, e := range entries {
		parts := splitPath(e.path)
		if len(parts) == 0 {
			root.isLeaf = true
			root.status = e.status
			root.length = e.length
			root.title = e.title
			root.fingers = mergeStrings(root.fingers, e.fingers)
			root.validated = root.validated || e.validated
			continue
		}
		node := root
		for i, part := range parts {
			child := findChild(node, part)
			if child == nil {
				child = &sitemapNode{segment: part}
				node.children = append(node.children, child)
			}
			if i == len(parts)-1 {
				child.isLeaf = true
				child.status = e.status
				child.length = e.length
				child.title = e.title
				child.fingers = mergeStrings(child.fingers, e.fingers)
				child.validated = child.validated || e.validated
			}
			node = child
		}
	}
	return root
}

func collectAnnotations(asset Asset) map[string][]string {
	out := make(map[string][]string)
	for _, item := range asset.Items {
		switch item.Kind {
		case assetItemFingerprint:
			p := pathFromTarget(item.Target, asset.Target)
			if p != "" {
				out[p] = appendUniq(out[p], item.Title)
			}
		case assetItemFinding, assetItemNote, assetItemResponse:
			p := pathFromTarget(item.Target, asset.Target)
			if p == "" {
				p = "/"
			}
			skill := firstNonEmptyString(item.Source, item.Kind)
			label := skill
			if item.Status != "" {
				label += ":" + item.Status
			}
			summary := firstNonEmptyString(item.Title, item.Summary)
			if summary != "" && len(summary) <= 40 {
				label += " " + summary
			}
			out[p] = appendUniq(out[p], label)
		}
	}
	return out
}

func attachAnnotations(root *sitemapNode, anns map[string][]string) {
	if a, ok := anns["/"]; ok {
		root.annotations = append(root.annotations, a...)
	}
	for path, a := range anns {
		if path == "/" {
			continue
		}
		parts := splitPath(path)
		node := root
		for _, part := range parts {
			child := findChild(node, part)
			if child == nil {
				child = &sitemapNode{segment: part, isLeaf: true}
				node.children = append(node.children, child)
			}
			node = child
		}
		node.annotations = append(node.annotations, a...)
	}
}

// renderNode renders a sitemap node in spray-inspired format:
//
//	[200] /path  10487 "title" [fingers] {annotations}
func renderNode(sb *strings.Builder, node *sitemapNode, indent string, isRoot bool, rc renderColor) {
	var line strings.Builder

	// tree prefix
	if isRoot {
		line.WriteString(indent)
	} else {
		line.WriteString(indent)
		line.WriteString("├── ")
	}

	// [STATUS] — fixed 5 chars, blank for directories
	if node.isLeaf && node.status != "" {
		line.WriteString(rc.Status(fmt.Sprintf("[%-3s]", node.status)))
	} else {
		line.WriteString("     ")
	}
	line.WriteString(" ")

	// PATH — validated=GreenBold, leaf=plain, directory=Dim
	path := "/" + node.segment
	if isRoot {
		path = "/"
	}
	if node.validated {
		line.WriteString(rc.GreenBold(path))
	} else if node.isLeaf {
		line.WriteString(path)
	} else {
		line.WriteString(rc.Dim(path))
	}

	// LENGTH — after path
	if node.isLeaf && node.length > 0 {
		line.WriteString("  " + rc.YellowBold(fmt.Sprintf("%d", node.length)))
	}

	// TITLE — skip static resource titles
	if node.title != "" && !isStaticTitle(node.title) {
		line.WriteString("  " + rc.Green(strconv.Quote(node.title)))
	}

	// FINGERPRINTS
	if len(node.fingers) > 0 {
		line.WriteString(" " + rc.Cyan("["+strings.Join(node.fingers, ",")+"]"))
	}

	// ANNOTATIONS (findings, AI results)
	for _, ann := range node.annotations {
		line.WriteString(" " + rc.Yellow("{"+ann+"}"))
	}

	sb.WriteString(line.String())
	sb.WriteByte('\n')

	// children
	for _, child := range node.children {
		childIndent := indent
		if !isRoot {
			childIndent += "│   "
		}
		renderNode(sb, child, childIndent, false, rc)
	}
}

// --- helpers ---

func findChild(node *sitemapNode, segment string) *sitemapNode {
	for _, c := range node.children {
		if c.segment == segment {
			return c
		}
	}
	return nil
}

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	parts := strings.Split(p, "/")
	if idx := strings.Index(parts[len(parts)-1], "?"); idx >= 0 {
		parts[len(parts)-1] = parts[len(parts)-1][:idx]
	}
	return parts
}

func pathFromTarget(target, assetTarget string) string {
	if target == "" {
		return ""
	}
	p := webPath(target)
	if p == target && assetTarget != "" {
		if strings.HasPrefix(target, assetTarget) {
			p = strings.TrimPrefix(target, assetTarget)
			if p == "" {
				p = "/"
			}
		}
	}
	return p
}

func isStaticTitle(title string) bool {
	switch strings.ToLower(title) {
	case "js data", "css data", "ico data", "image data":
		return true
	}
	return false
}

func mergeStrings(a, b []string) []string {
	if len(b) == 0 {
		return a
	}
	seen := make(map[string]struct{}, len(a))
	for _, s := range a {
		seen[strings.ToLower(s)] = struct{}{}
	}
	for _, s := range b {
		if _, ok := seen[strings.ToLower(s)]; !ok {
			a = append(a, s)
			seen[strings.ToLower(s)] = struct{}{}
		}
	}
	return a
}

func appendUniq(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}

func assetDataString(data map[string]any, key string) string {
	if len(data) == 0 {
		return ""
	}
	switch value := data[key].(type) {
	case string:
		return value
	case int:
		if value == 0 {
			return ""
		}
		return strconv.Itoa(value)
	case float64:
		if value == 0 {
			return ""
		}
		return strconv.Itoa(int(value))
	default:
		return ""
	}
}

func assetDataInt(data map[string]any, key string) int {
	if len(data) == 0 {
		return 0
	}
	switch v := data[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

func assetDataStrings(data map[string]any, key string) []string {
	if len(data) == 0 {
		return nil
	}
	switch v := data[key].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
