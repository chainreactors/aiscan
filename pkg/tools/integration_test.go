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
	anicmd "github.com/chainreactors/aiscan/pkg/tools/ani"
	inacmd "github.com/chainreactors/aiscan/pkg/tools/ina"
	"github.com/chainreactors/aiscan/pkg/tools/scan/engine"
)

func TestIntegrationInaFofa(t *testing.T) {
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
	cmd := inacmd.New(set.Ina)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := cmd.Execute(ctx, []string{`domain="anthropic.com"`})
	if err != nil {
		t.Fatalf("ina Execute: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatalf("no assets returned: %q", out)
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("first line not JSON: %v\n%s", err, lines[0])
	}
	if first["source"] != "fofa" {
		t.Errorf("source field: got %v", first["source"])
	}
	if first["ip"] == "" {
		t.Errorf("missing ip: %+v", first)
	}
	t.Logf("ina/fofa returned %d assets, first=%v", len(lines), first)
}

func TestIntegrationInaHunter(t *testing.T) {
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
	cmd := inacmd.New(set.Ina)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := cmd.Execute(ctx, []string{"-s", "hunter", `domain.suffix="anthropic.com"`})
	if err != nil {
		t.Fatalf("ina hunter Execute: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Logf("hunter returned empty (may be quota/WAF); output: %q", out)
		return
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("first line not JSON: %v\n%s", err, lines[0])
	}
	if first["source"] != "hunter" {
		t.Errorf("source field: got %v", first["source"])
	}
	t.Logf("ina/hunter returned %d assets, first=%v", len(lines), first)
}

func TestIntegrationAniAqc(t *testing.T) {
	if os.Getenv("AISCAN_INTEGRATION") == "" {
		t.Skip("set AISCAN_INTEGRATION=1 to run")
	}
	set := &engine.Set{}
	set.SetupAni(engine.ReconOptions{AniDepth: 1, AniPercent: 0.5}, telemetry.NopLogger())
	if set.Ani == nil {
		t.Fatal("expected Ani engine to be initialized")
	}
	cmd := anicmd.New(set.Ani)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := cmd.Execute(ctx, []string{"-n", "默安科技"})
	if err != nil {
		t.Fatalf("ani Execute: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatalf("no assets returned: %q", out)
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("first line not JSON: %v\n%s", err, lines[0])
	}
	if first["source"] != "aqc_unauth" {
		t.Errorf("source field: got %v", first["source"])
	}
	if first["name"] == "" {
		t.Errorf("missing company name: %+v", first)
	}
	t.Logf("ani/aqc returned %d records, first=%v", len(lines), first)
}

func TestIntegrationAniTycUnauth(t *testing.T) {
	if os.Getenv("AISCAN_INTEGRATION") == "" {
		t.Skip("set AISCAN_INTEGRATION=1 to run")
	}
	set := &engine.Set{}
	set.SetupAni(engine.ReconOptions{AniDepth: 1, AniPercent: 0.5}, telemetry.NopLogger())
	cmd := anicmd.New(set.Ani)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := cmd.Execute(ctx, []string{"-s", "tyc_unauth", "-n", "默安科技"})
	if err != nil {
		// tyc_unauth 反爬比较激进, 命中机器人验证就跳过
		t.Logf("tyc_unauth Execute failed (likely 429/captcha): %v", err)
		t.Skip("tyc_unauth unreachable from this network")
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Logf("no assets returned: %q", out)
		return
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("first line not JSON: %v\n%s", err, lines[0])
	}
	if first["source"] != "tyc_unauth" {
		t.Errorf("source field: got %v", first["source"])
	}
	t.Logf("ani/tyc_unauth returned %d records, first=%v", len(lines), first)
}

func TestIntegrationAniTyc(t *testing.T) {
	if os.Getenv("AISCAN_INTEGRATION") == "" {
		t.Skip("set AISCAN_INTEGRATION=1 to run")
	}
	token := os.Getenv("ANI_TYC_TOKEN")
	if token == "" {
		t.Skip("ANI_TYC_TOKEN required")
	}
	set := &engine.Set{}
	set.SetupAni(engine.ReconOptions{AniDepth: 1, AniPercent: 0.5, AniTycToken: token}, telemetry.NopLogger())
	cmd := anicmd.New(set.Ani)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := cmd.Execute(ctx, []string{"-s", "tyc", "-n", "默安科技"})
	if err != nil {
		t.Fatalf("ani tyc Execute: %v", err)
	}
	t.Logf("ani/tyc output (first 500 bytes): %s", truncForTest(out, 500))
}

func TestIntegrationAniQcc(t *testing.T) {
	if os.Getenv("AISCAN_INTEGRATION") == "" {
		t.Skip("set AISCAN_INTEGRATION=1 to run")
	}
	cookie := os.Getenv("ANI_QCC_COOKIE")
	if cookie == "" {
		t.Skip("ANI_QCC_COOKIE required")
	}
	set := &engine.Set{}
	set.SetupAni(engine.ReconOptions{AniDepth: 1, AniPercent: 0.5, AniQccCookie: cookie}, telemetry.NopLogger())
	cmd := anicmd.New(set.Ani)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := cmd.Execute(ctx, []string{"-s", "qcc", "-n", "默安科技"})
	if err != nil {
		t.Fatalf("ani qcc Execute: %v", err)
	}
	t.Logf("ani/qcc output (first 500 bytes): %s", truncForTest(out, 500))
}

func TestIntegrationAniAqcAuth(t *testing.T) {
	if os.Getenv("AISCAN_INTEGRATION") == "" {
		t.Skip("set AISCAN_INTEGRATION=1 to run")
	}
	cookie := os.Getenv("ANI_AQC_COOKIE")
	if cookie == "" {
		t.Skip("ANI_AQC_COOKIE required")
	}
	set := &engine.Set{}
	set.SetupAni(engine.ReconOptions{AniDepth: 1, AniPercent: 0.5, AniAqcCookie: cookie}, telemetry.NopLogger())
	cmd := anicmd.New(set.Ani)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := cmd.Execute(ctx, []string{"-s", "aqc", "-n", "默安科技"})
	if err != nil {
		t.Fatalf("ani aqc Execute: %v", err)
	}
	t.Logf("ani/aqc(authed) output (first 500 bytes): %s", truncForTest(out, 500))
}

func truncForTest(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
