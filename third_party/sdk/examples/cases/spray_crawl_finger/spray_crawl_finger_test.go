package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	sdkfingers "github.com/chainreactors/sdk/fingers"
	"github.com/chainreactors/sdk/pkg/types"
	"github.com/chainreactors/sdk/spray"
)

func TestSprayCrawlAndDeepFinger(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		switch r.URL.Path {
		case "/":
			_, _ = fmt.Fprint(w, `<html><body>root <a href="/admin">admin</a></body></html>`)
		case "/admin":
			_, _ = fmt.Fprint(w, `<html><body>adminmarker</body></html>`)
		case "/deep/finger":
			_, _ = fmt.Fprint(w, `<html><body>probemarker</body></html>`)
		default:
			_, _ = fmt.Fprint(w, `<html><body>generic</body></html>`)
		}
	}))
	defer srv.Close()

	fingersEng, err := sdkfingers.NewEngine(
		sdkfingers.NewConfig().WithFingers(types.Fingers{
			{
				Name:     "admin-app",
				Protocol: "http",
				Rules: types.FingerRules{
					{
						Regexps: &types.FingerRegexps{Body: []string{"adminmarker"}},
					},
				},
			},
			{
				Name:     "deep-probe",
				Protocol: "http",
				Rules: types.FingerRules{
					{
						SendDataStr: "/deep/finger",
						Regexps:     &types.FingerRegexps{Body: []string{"probemarker"}},
					},
				},
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	sprayEng := spray.NewEngine(spray.NewConfig().WithFingersEngine(fingersEng).WithMatchDetail())
	if err := sprayEng.Init(); err != nil {
		t.Fatal(err)
	}

	opt := types.NewDefaultSprayOption()
	opt.Fuzzy = true
	ctx := spray.NewContext().
		SetOption(opt).
		SetThreads(4).
		SetTimeout(2).
		SetCrawlPlugin(true).
		SetFinger(true).
		SetCrawlDepth(2)

	results, err := sprayEng.Brute(ctx, srv.URL, []string{"/"})
	if err != nil {
		t.Fatal(err)
	}

	byPath := map[string]*types.SprayResult{}
	for _, result := range results {
		if result == nil {
			continue
		}
		byPath[urlPath(result.UrlString)] = result
	}

	if root := byPath["/"]; root == nil {
		t.Fatalf("missing root result: %+v", byPath)
	}

	admin := byPath["/admin"]
	if admin == nil {
		t.Fatalf("missing /admin result: %+v", byPath)
	}
	if admin.Source != types.CrawlSource {
		t.Fatalf("expected /admin source crawl, got %s", admin.Source.Name())
	}
	if got := admin.Frameworks["admin-app"]; got == nil {
		t.Fatalf("expected admin-app framework, got %+v", admin.Frameworks)
	} else {
		assertDetail(t, got.MatchDetail, "body", "", "adminmarker")
	}

	deep := byPath["/deep/finger"]
	if deep == nil {
		t.Fatalf("missing /deep/finger result: %+v", byPath)
	}
	if deep.Source != types.FingerSource {
		t.Fatalf("expected /deep/finger source finger, got %s", deep.Source.Name())
	}
	if !strings.HasSuffix(deep.UrlString, "/deep/finger") {
		t.Fatalf("unexpected deep url: %s", deep.UrlString)
	}
	if got := deep.Frameworks["deep-probe"]; got == nil {
		t.Fatalf("expected deep-probe framework, got %+v", deep.Frameworks)
	} else {
		assertDetail(t, got.MatchDetail, "body", "", "probemarker")
	}
}

func urlPath(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return parsed.Path
}

func assertDetail(t *testing.T, detail *types.MatchDetail, matcherType, sendData, matcherValue string) {
	t.Helper()

	if detail == nil {
		t.Fatal("expected MatchDetail")
	}
	if detail.MatcherType != matcherType {
		t.Fatalf("expected matcher type %q, got %q", matcherType, detail.MatcherType)
	}
	if detail.SendData != sendData {
		t.Fatalf("expected send data %q, got %q", sendData, detail.SendData)
	}
	if detail.MatcherValue != matcherValue {
		t.Fatalf("expected matcher value %q, got %q", matcherValue, detail.MatcherValue)
	}
	if detail.RuleIndex != 0 {
		t.Fatalf("expected rule index 0, got %d", detail.RuleIndex)
	}
}
