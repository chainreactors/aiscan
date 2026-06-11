package fingers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chainreactors/sdk/pkg/types"
)

func TestWithMatchDetailPassiveMatch(t *testing.T) {
	eng := newDetailTestEngine(t, NewConfig().WithMatchDetail(), &types.Finger{
		Name:     "passive-app",
		Protocol: "http",
		Rules: types.FingerRules{{
			Regexps: &types.FingerRegexps{Body: []string{"PassiveMarker"}},
		}},
	})

	frames, err := eng.Match(rawHTTP("PassiveMarker"))
	if err != nil {
		t.Fatal(err)
	}
	detail := frames["passive-app"].MatchDetail
	if detail == nil {
		t.Fatal("expected MatchDetail from WithMatchDetail")
	}
	if detail.RuleIndex != 0 || detail.MatcherType != "body" || detail.MatcherValue != "passivemarker" {
		t.Fatalf("unexpected detail: %+v", *detail)
	}
	if detail.SendData != "" {
		t.Fatalf("passive match should not set send_data, got %q", detail.SendData)
	}
}

func TestWithMatchDetailActiveHTTPMatch(t *testing.T) {
	eng := newDetailTestEngine(t, NewConfig().WithMatchDetail(), &types.Finger{
		Name:        "active-app",
		Protocol:    "http",
		SendDataStr: "/probe",
		Rules: types.FingerRules{{
			Regexps: &types.FingerRegexps{Body: []string{"ActiveMarker"}},
		}},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/probe" {
			_, _ = w.Write([]byte("ActiveMarker"))
			return
		}
		_, _ = w.Write([]byte("no marker"))
	}))
	defer srv.Close()

	results, err := eng.HTTPMatch(NewContext().WithLevel(1), []string{srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || len(results[0].Results) != 1 {
		t.Fatalf("expected one active match, got %+v", results)
	}
	detail := results[0].Results[0].Framework.MatchDetail
	if detail == nil {
		t.Fatal("expected active MatchDetail")
	}
	if detail.MatcherType != "body" || detail.MatcherValue != "activemarker" || detail.SendData != "/probe" {
		t.Fatalf("unexpected active detail: %+v", *detail)
	}
}

func TestMatchDetailDisabledByDefault(t *testing.T) {
	eng := newDetailTestEngine(t, NewConfig(), &types.Finger{
		Name:     "plain-app",
		Protocol: "http",
		Rules: types.FingerRules{{
			Regexps: &types.FingerRegexps{Body: []string{"PlainMarker"}},
		}},
	})

	frames, err := eng.Match(rawHTTP("PlainMarker"))
	if err != nil {
		t.Fatal(err)
	}
	if fw := frames["plain-app"]; fw == nil || fw.MatchDetail != nil {
		t.Fatalf("MatchDetail should be nil by default, got %+v", fw)
	}
}

func newDetailTestEngine(t *testing.T, config *Config, finger *types.Finger) *Engine {
	t.Helper()
	if config == nil {
		config = NewConfig()
	}
	eng, err := NewEngine(config.WithFingers(types.Fingers{finger}))
	if err != nil {
		t.Fatal(err)
	}
	return eng
}

func rawHTTP(body string) []byte {
	return []byte("HTTP/1.1 200 OK\r\nContent-Type: text/html\r\n\r\n" + body)
}
