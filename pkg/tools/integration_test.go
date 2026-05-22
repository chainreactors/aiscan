//go:build integration

//	Run with: AISCAN_INTEGRATION=1 FOFA_EMAIL=... FOFA_KEY=... \
//	  go test -tags integration ./pkg/tools/... -run TestIntegration -v
package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/aiscan/pkg/telemetry"
	passivecmd "github.com/chainreactors/aiscan/pkg/tools/passive"
	"github.com/chainreactors/aiscan/pkg/tools/scan/engine"
)

func TestIntegrationPassiveFofa(t *testing.T) {
	if os.Getenv("AISCAN_INTEGRATION") == "" {
		t.Skip("set AISCAN_INTEGRATION=1 to run")
	}
	email := os.Getenv("FOFA_EMAIL")
	key := os.Getenv("FOFA_KEY")
	if email == "" || key == "" {
		t.Skip("FOFA_EMAIL / FOFA_KEY required")
	}
	set := &engine.Set{}
	set.SetupIna(engine.ReconOptions{FofaEmail: email, FofaKey: key, Limit: 5}, telemetry.NopLogger())
	if set.Ina == nil {
		t.Fatal("expected Ina engine to be initialized")
	}
	cmd := passivecmd.New(nil, set.Ina)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := cmd.Execute(ctx, []string{"-s", "fofa", `domain="anthropic.com"`})
	if err != nil {
		t.Fatalf("passive fofa Execute: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatalf("no assets returned: %q", out)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON array: %v\n%s", err, out)
	}
	if got[0]["ip"] == "" {
		t.Errorf("missing ip: %+v", got[0])
	}
	t.Logf("passive/fofa returned %d assets, first=%v", len(got), got[0])
}

func TestIntegrationPassiveHunter(t *testing.T) {
	if os.Getenv("AISCAN_INTEGRATION") == "" {
		t.Skip("set AISCAN_INTEGRATION=1 to run")
	}
	token := os.Getenv("HUNTER_TOKEN")
	apikey := os.Getenv("HUNTER_API_KEY")
	if token == "" && apikey == "" {
		t.Skip("HUNTER_TOKEN or HUNTER_API_KEY required")
	}
	set := &engine.Set{}
	set.SetupIna(engine.ReconOptions{
		HunterToken:  token,
		HunterAPIKey: apikey,
		IngressProxy: os.Getenv("RECON_PROXY"),
		Limit:        3,
	}, telemetry.NopLogger())
	if set.Ina == nil {
		t.Fatal("expected Ina engine to be initialized")
	}
	cmd := passivecmd.New(nil, set.Ina)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := cmd.Execute(ctx, []string{"-s", "hunter", `domain.suffix="anthropic.com"`})
	if err != nil {
		t.Fatalf("passive hunter Execute: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Logf("hunter returned empty (may be quota/WAF); output: %q", out)
		return
	}
	t.Logf("passive/hunter output (first 500 bytes): %s", truncForTest(out, 500))
}

func TestIntegrationPassiveAqc(t *testing.T) {
	if os.Getenv("AISCAN_INTEGRATION") == "" {
		t.Skip("set AISCAN_INTEGRATION=1 to run")
	}
	set := &engine.Set{}
	set.SetupAni(engine.ReconOptions{AniDepth: 1, AniPercent: 0.5}, telemetry.NopLogger())
	if set.Ani == nil {
		t.Fatal("expected Ani engine to be initialized")
	}
	cmd := passivecmd.New(set.Ani, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := cmd.Execute(ctx, []string{"-s", "aqc_unauth", "-n", "默安科技"})
	if err != nil {
		t.Fatalf("passive aqc Execute: %v", err)
	}
	var got map[string]map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON object: %v\n%s", err, out)
	}
	if len(got) == 0 {
		t.Fatalf("no companies returned: %q", out)
	}
	t.Logf("passive/aqc returned %d companies", len(got))
}

func TestIntegrationPassiveTycUnauth(t *testing.T) {
	if os.Getenv("AISCAN_INTEGRATION") == "" {
		t.Skip("set AISCAN_INTEGRATION=1 to run")
	}
	set := &engine.Set{}
	set.SetupAni(engine.ReconOptions{AniDepth: 1, AniPercent: 0.5}, telemetry.NopLogger())
	cmd := passivecmd.New(set.Ani, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := cmd.Execute(ctx, []string{"-s", "tyc_unauth", "-n", "默安科技"})
	if err != nil {
		// tyc_unauth 反爬比较激进, 命中机器人验证就跳过
		t.Logf("tyc_unauth Execute failed (likely 429/captcha): %v", err)
		t.Skip("tyc_unauth unreachable from this network")
	}
	t.Logf("passive/tyc_unauth output (first 500 bytes): %s", truncForTest(out, 500))
}

func TestIntegrationPassiveTyc(t *testing.T) {
	if os.Getenv("AISCAN_INTEGRATION") == "" {
		t.Skip("set AISCAN_INTEGRATION=1 to run")
	}
	token := os.Getenv("ANI_TYC_TOKEN")
	if token == "" {
		t.Skip("ANI_TYC_TOKEN required")
	}
	set := &engine.Set{}
	set.SetupAni(engine.ReconOptions{AniDepth: 1, AniPercent: 0.5, AniTycToken: token}, telemetry.NopLogger())
	cmd := passivecmd.New(set.Ani, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := cmd.Execute(ctx, []string{"-s", "tyc", "-n", "默安科技"})
	if err != nil {
		t.Fatalf("passive tyc Execute: %v", err)
	}
	t.Logf("passive/tyc output (first 500 bytes): %s", truncForTest(out, 500))
}

func TestIntegrationPassiveQcc(t *testing.T) {
	if os.Getenv("AISCAN_INTEGRATION") == "" {
		t.Skip("set AISCAN_INTEGRATION=1 to run")
	}
	cookie := os.Getenv("ANI_QCC_COOKIE")
	if cookie == "" {
		t.Skip("ANI_QCC_COOKIE required")
	}
	set := &engine.Set{}
	set.SetupAni(engine.ReconOptions{AniDepth: 1, AniPercent: 0.5, AniQccCookie: cookie}, telemetry.NopLogger())
	cmd := passivecmd.New(set.Ani, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := cmd.Execute(ctx, []string{"-s", "qcc", "-n", "默安科技"})
	if err != nil {
		t.Fatalf("passive qcc Execute: %v", err)
	}
	t.Logf("passive/qcc output (first 500 bytes): %s", truncForTest(out, 500))
}

func TestIntegrationPassiveAqcAuth(t *testing.T) {
	if os.Getenv("AISCAN_INTEGRATION") == "" {
		t.Skip("set AISCAN_INTEGRATION=1 to run")
	}
	cookie := os.Getenv("ANI_AQC_COOKIE")
	if cookie == "" {
		t.Skip("ANI_AQC_COOKIE required")
	}
	set := &engine.Set{}
	set.SetupAni(engine.ReconOptions{AniDepth: 1, AniPercent: 0.5, AniAqcCookie: cookie}, telemetry.NopLogger())
	cmd := passivecmd.New(set.Ani, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := cmd.Execute(ctx, []string{"-s", "aqc", "-n", "默安科技"})
	if err != nil {
		t.Fatalf("passive aqc(authed) Execute: %v", err)
	}
	t.Logf("passive/aqc(authed) output (first 500 bytes): %s", truncForTest(out, 500))
}

func truncForTest(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
