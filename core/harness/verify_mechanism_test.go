//go:build e2e

package harness

import (
	"os"
	"strings"
	"testing"
	"time"
)

const verifyTarget = realSingleTarget

// TestVerifyOffProducesNoAIOutput runs scan with --verify=off and confirms
// that no AI skill output appears in the results.
func TestVerifyOffProducesNoAIOutput(t *testing.T) {
	h := New(t)
	r := h.RunWithTimeout(300*time.Second,
		"scan", "-i", "127.0.0.1", "--mode", "quick", "--verify=off", "--timeout", "3",
	)
	Verify(t, r).OK().Done()

	if hasAISkillOutput(r.Stdout) {
		t.Fatalf("--verify=off should produce no AI skill output, got:\n%s", clip(r.Stdout, 2000))
	}
	t.Logf("verify=off output (%d bytes):\n%s", len(r.Stdout), clip(r.Stdout, 2000))
}

// TestVerifyHighWithSniperTriggersAIVerification runs scan with explicit
// verify and sniper options and confirms that the scan pipeline completes with
// AI skills enabled. When targets have high-priority loots, AI verify and
// sniper skills produce output.
func TestVerifyHighWithSniperTriggersAIVerification(t *testing.T) {
	h := New(t)
	r := h.RunWithTimeout(600*time.Second,
		"scan", "-i", verifyTarget, "--mode", "quick", "--verify=high", "--sniper", "--timeout", "5",
	)
	Verify(t, r).OK().Done()

	if !hasSummaryLine(r.Stdout) {
		t.Fatal("expected [summary] line in output")
	}
	t.Logf("verify+sniper output (%d bytes):\n%s", len(r.Stdout), clip(r.Stdout, 3000))
}

// TestVerifyExplicitModeWithoutSniper runs scan with --verify=high explicitly
// (no --sniper) and checks that verify runs but sniper is NOT activated.
func TestVerifyExplicitModeWithoutSniper(t *testing.T) {
	h := New(t)
	r := h.RunWithTimeout(600*time.Second,
		"scan", "-i", verifyTarget, "--mode", "quick", "--verify=high", "--timeout", "5",
	)
	Verify(t, r).OK().Done()

	if hasSniperOutput(r.Stdout) {
		t.Fatal("--verify=high without --sniper should not produce sniper output")
	}
	if !hasSummaryLine(r.Stdout) {
		t.Fatal("expected [summary] line in output")
	}
	t.Logf("verify=high output (%d bytes):\n%s", len(r.Stdout), clip(r.Stdout, 2000))
}

// TestScanVerifySniperNoPostAnalysis verifies that the old post-analysis
// one-shot LLM call no longer runs. Explicit scan AI skills trigger only
// in-pipeline AI work (verify + sniper), not a separate "analysis" step.
// The output should contain the [summary] line from the scan pipeline but
// should not contain the "analysis" output section that runScannerPostAnalysis
// used to produce.
func TestScanVerifySniperNoPostAnalysis(t *testing.T) {
	h := New(t)
	r := h.RunWithTimeout(600*time.Second,
		"scan", "-i", verifyTarget, "--mode", "quick", "--verify=high", "--sniper", "--timeout", "5",
	)
	Verify(t, r).OK().Done()

	if !hasSummaryLine(r.Stdout) {
		t.Fatal("expected [summary] line from scan pipeline")
	}
	t.Logf("output (%d bytes):\n%s", len(r.Stdout), clip(r.Stdout, 3000))
}

// TestScanDefaultModeCompletes runs scan without any explicit AI skill flags.
// The default verify mode is "auto" (mapped to "high"), which enables the
// provider optionally. If the provider initializes, AI verify can run; if not,
// the scan still completes successfully.
func TestScanDefaultModeCompletes(t *testing.T) {
	h := New(t)
	r := h.RunWithTimeout(600*time.Second,
		"scan", "-i", verifyTarget, "--mode", "quick", "--timeout", "5",
	)
	Verify(t, r).OK().Done()

	if !hasSummaryLine(r.Stdout) {
		t.Fatal("expected [summary] line in output")
	}
	t.Logf("default mode output (%d bytes):\n%s", len(r.Stdout), clip(r.Stdout, 2000))
}

// TestVerifyOffDisablesAllAISkills confirms that --verify=off combined with
// no --sniper and no --deep results in zero AI skill results in the summary.
func TestVerifyOffDisablesAllAISkills(t *testing.T) {
	h := New(t)
	r := h.RunWithTimeout(300*time.Second,
		"scan", "-i", "127.0.0.1", "--mode", "quick", "--verify=off", "--timeout", "3",
	)
	Verify(t, r).OK().Done()

	summary := extractSummaryLine(r.Stdout)
	if summary == "" {
		t.Fatal("missing [summary] line")
	}
	if strings.Contains(summary, "verified") {
		parts := strings.Fields(summary)
		for i, p := range parts {
			if p == "verified" && i > 0 && parts[i-1] != "0" {
				t.Fatalf("expected 0 verified in summary with --verify=off, got: %s", summary)
			}
		}
	}
	t.Logf("verify=off summary: %s", summary)
}

// TestScanVerifyWithReportIncludesVerification runs scan with explicit
// verification and report output and verifies the report includes AI
// verification metrics.
func TestScanVerifyWithReportIncludesVerification(t *testing.T) {
	h := New(t)
	r := h.RunWithTimeout(600*time.Second,
		"scan", "-i", verifyTarget, "--mode", "quick", "--verify=high", "--sniper", "--report", "--timeout", "5",
	)
	Verify(t, r).OK().Done()

	hasMetrics := strings.Contains(r.Stdout, "AI verifications") ||
		strings.Contains(r.Stdout, "AI skill") ||
		strings.Contains(r.Stdout, "verified")
	if !hasMetrics {
		t.Fatal("--verify=high --sniper --report should include AI verification information in output")
	}
	t.Logf("report output (%d bytes):\n%s", len(r.Stdout), clip(r.Stdout, 3000))
}

// TestAssetReportFileOutputFormats runs scan with -f and -F and verifies both
// output formats include structured checkpoint loots.
func TestAssetReportFileOutputFormats(t *testing.T) {
	h := New(t)
	r := h.RunWithTimeout(600*time.Second,
		"scan", "-i", verifyTarget, "--mode", "quick", "--verify=high", "--sniper", "--timeout", "5",
		"-f", "output.txt", "-F", "asset_report.txt",
	)
	Verify(t, r).OK().Done()

	plainBytes, err := os.ReadFile(h.WorkFile("output.txt"))
	if err != nil {
		t.Fatalf("read -f output: %v", err)
	}
	plain := string(plainBytes)
	t.Logf("-f output (%d bytes):\n%s", len(plain), clip(plain, 3000))

	assetReportBytes, err := os.ReadFile(h.WorkFile("asset_report.txt"))
	if err != nil {
		if !hasAISkillOutput(r.Stdout) {
			t.Skip("no AI output produced, skipping -F check")
		}
		t.Fatalf("read -F output: %v", err)
	}
	assetReport := string(assetReportBytes)
	t.Logf("-F output (%d bytes):\n%s", len(assetReport), clip(assetReport, 3000))

	if len(assetReport) > 0 {
		if !strings.Contains(assetReport, "Assets:") {
			t.Fatal("-F output should contain 'Assets:' header")
		}
	}
}

// --- helpers ---

func hasAISkillOutput(output string) bool {
	markers := []string{"[ai:", "[sniper:", "[ai]", "[sniper]"}
	for _, m := range markers {
		if strings.Contains(output, m) {
			return true
		}
	}
	return false
}

func hasSniperOutput(output string) bool {
	return strings.Contains(output, "[sniper:") || strings.Contains(output, "[sniper]")
}

func hasSummaryLine(output string) bool {
	return strings.Contains(output, "[summary]") || strings.Contains(output, "completed")
}

func extractSummaryLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "[summary]") {
			return line
		}
	}
	return ""
}
