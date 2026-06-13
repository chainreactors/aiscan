package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func RenderRecordsTerminal(w io.Writer, records []Record) error {
	c := NewColor(true)
	for _, r := range records {
		switch r.Type {
		case TypeScanStart:
			d, _ := ParseRecordData[ScanStart](r)
			fmt.Fprintf(w, "%s[scan]%s %s%s%s targets=%s mode=%s\n",
				c.Code(ANSIBold+ANSICyan), c.Code(ANSIReset),
				c.Code(ANSIDim), r.Timestamp.Format("15:04:05"), c.Code(ANSIReset),
				strings.Join(d.Targets, ","), d.Mode)

		case TypeService:
			d := parseServiceView(r)
			fmt.Fprintf(w, "%s[service]%s %s %s %s\n",
				c.Code(ANSIGreen), c.Code(ANSIReset),
				d.displayTarget(), d.Protocol, d.displayBanner())

		case TypeWeb:
			d := parseWebView(r)
			fingers := ""
			if names := d.fingerNames(); len(names) > 0 {
				fingers = " [" + strings.Join(names, ",") + "]"
			}
			fmt.Fprintf(w, "%s[web]%s %s %d %s%s\n",
				c.Code(ANSIGreen), c.Code(ANSIReset),
				d.URL, d.Status, d.Title, fingers)

		case TypeLoot:
			d, _ := ParseRecordData[Loot](r)
			prefix := d.Kind
			color := ANSIYellow
			if d.Priority == "high" || d.Priority == "critical" {
				color = ANSIRed
			}
			fmt.Fprintf(w, "%s[%s]%s %s %s\n",
				c.Code(color), c.Code(ANSIReset), prefix, d.Target, d.Description)

		case TypeScanEnd:
			d, _ := ParseRecordData[ScanEnd](r)
			fmt.Fprintf(w, "%s[done]%s %.1fs targets=%d services=%d webs=%d loots=%d errors=%d\n",
				c.Code(ANSIDim), c.Code(ANSIReset),
				d.Duration, d.Targets, d.Services, d.Webs, d.Loots, d.Errors)
		}
	}
	return nil
}

// serviceView handles both old record.Service format and new parsers.GOGOResult format.
type serviceView struct {
	Target   string `json:"target"`
	Ip       string `json:"ip"`
	Port     any    `json:"port"`
	Protocol string `json:"protocol"`
	Banner   string `json:"banner"`
	Midware  string `json:"midware"`
}

func (s serviceView) displayTarget() string {
	if s.Target != "" {
		return s.Target
	}
	if s.Ip != "" {
		if p := anyToString(s.Port); p != "" {
			return s.Ip + ":" + p
		}
		return s.Ip
	}
	return ""
}

func (s serviceView) displayBanner() string {
	if s.Banner != "" {
		return s.Banner
	}
	return s.Midware
}

func parseServiceView(r Record) serviceView {
	var v serviceView
	_ = json.Unmarshal(r.Data, &v)
	return v
}

// webView handles both old record.Web format and new parsers.SprayResult format.
type webView struct {
	URL        string          `json:"url"`
	Status     int             `json:"status"`
	Title      string          `json:"title"`
	Fingers    []string        `json:"fingers"`
	ContentLen int             `json:"content_len"`
	BodyLength int             `json:"body_length"`
	Frameworks json.RawMessage `json:"frameworks"`
}

func (v webView) fingerNames() []string {
	if len(v.Fingers) > 0 {
		return v.Fingers
	}
	if len(v.Frameworks) == 0 {
		return nil
	}
	var frames []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(v.Frameworks, &frames) == nil {
		var names []string
		for _, f := range frames {
			if f.Name != "" {
				names = append(names, f.Name)
			}
		}
		return names
	}
	return nil
}

func parseWebView(r Record) webView {
	var v webView
	_ = json.Unmarshal(r.Data, &v)
	return v
}

func anyToString(v any) string {
	switch p := v.(type) {
	case string:
		return p
	case float64:
		return strconv.Itoa(int(p))
	default:
		return ""
	}
}
