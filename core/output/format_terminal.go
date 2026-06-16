package output

import (
	"encoding/json"
	"strconv"
)

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
