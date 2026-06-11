package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	sdkfingers "github.com/chainreactors/sdk/fingers"
	"github.com/chainreactors/sdk/pkg/types"
)

func TestMatchDetailUsage(t *testing.T) {
	eng, err := sdkfingers.NewEngine(
		sdkfingers.NewConfig().WithMatchDetail().WithFingers(types.Fingers{{
			Name:     "demo-app",
			Protocol: "http",
			Rules: types.FingerRules{{
				Regexps: &types.FingerRegexps{Body: []string{"DemoMarker"}},
			}},
		}}),
	)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("response with DemoMarker"))
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	frameworks, err := eng.MatchHTTP(resp)
	if err != nil {
		t.Fatal(err)
	}
	fw := frameworks["demo-app"]
	if fw == nil {
		t.Fatalf("expected demo-app, got %v", frameworks)
	}
	if fw.MatchDetail == nil {
		t.Fatal("expected MatchDetail")
	}
	if fw.MatchDetail.MatcherType != "body" || fw.MatchDetail.MatcherValue != "demomarker" {
		t.Fatalf("unexpected detail: %+v", *fw.MatchDetail)
	}
}
