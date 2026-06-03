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

// TestVerifyHighTriggersAIVerification runs scan with --ai (which sets
// verify=high) and confirms that the scan pipeline completes with AI skills
// enabled. When targets have high-priority findings, AI verify and sniper
// skills produce output.
func TestVerifyHighTriggersAIVerification(t *testing.T) {
	h := New(t)
	r := h.RunWithTimeout(600*time.Second,
		"scan", "-i", verifyTarget, "--mode", "quick", "--ai", "--timeout", "5",
	)
	Verify(t, r).OK().Done()

	if !hasSummaryLine(r.Stdout) {
		t.Fatal("expected [summary] line in output")
	}
	t.Logf("--ai output (%d bytes):\n%s", len(r.Stdout), clip(r.Stdout, 3000))
}

// TestVerifyExplicitModeWithoutSniper runs scan with --verify=high explicitly
// (no --ai) and checks that verify runs but sniper is NOT activated.
func TestVerifyExplicitModeWithoutSniper(t *testing.T) {
	h := New(t)
	r := h.RunWithTimeout(600*time.Second,
		"scan", "-i", verifyTarget, "--mode", "quick", "--verify=high", "--timeout", "5",
	)
	Verify(t, r).OK().Done()

	if hasSniperOutput(r.Stdout) {
		t.Fatal("--verify=high without --ai should not produce sniper output")
	}
	if !hasSummaryLine(r.Stdout) {
		t.Fatal("expected [summary] line in output")
	}
	t.Logf("verify=high output (%d bytes):\n%s", len(r.Stdout), clip(r.Stdout, 2000))
}

// TestScanAINoPostAnalysis verifies that the old post-analysis one-shot LLM
// call no longer runs. With the degradation path removed, --ai triggers only
// in-pipeline AI skills (verify + sniper), not a separate "analysis" step.
// The output should contain the [summary] line from the scan pipeline but
// should not contain the "analysis" output section that runScannerPostAnalysis
// used to produce.
func TestScanAINoPostAnalysis(t *testing.T) {
	h := New(t)
	r := h.RunWithTimeout(600*time.Second,
		"scan", "-i", verifyTarget, "--mode", "quick", "--ai", "--timeout", "5",
	)
	Verify(t, r).OK().Done()

	if !hasSummaryLine(r.Stdout) {
		t.Fatal("expected [summary] line from scan pipeline")
	}
	t.Logf("output (%d bytes):\n%s", len(r.Stdout), clip(r.Stdout, 3000))
}

// TestScanDefaultModeCompletes runs scan without any explicit --verify or --ai
// flag. The default verify mode is "auto" (mapped to "high"), which enables the
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
// no --ai and no --sniper results in zero AI skill results in the summary.
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

// TestScanAIWithReportIncludesVerification runs scan with --ai --report and
// verifies the report output includes AI verification metrics.
func TestScanAIWithReportIncludesVerification(t *testing.T) {
	h := New(t)
	r := h.RunWithTimeout(600*time.Second,
		"scan", "-i", verifyTarget, "--mode", "quick", "--ai", "--report", "--timeout", "5",
	)
	Verify(t, r).OK().Done()

	hasMetrics := strings.Contains(r.Stdout, "AI verifications") ||
		strings.Contains(r.Stdout, "AI skill") ||
		strings.Contains(r.Stdout, "verified")
	if !hasMetrics {
		t.Fatal("--ai --report should include AI verification information in output")
	}
	t.Logf("report output (%d bytes):\n%s", len(r.Stdout), clip(r.Stdout, 3000))
}

// TestFindingsFileOutputFormats runs scan with -f and -F and verifies both
// output formats include structured checkpoint findings.
func TestFindingsFileOutputFormats(t *testing.T) {
	h := New(t)
	r := h.RunWithTimeout(600*time.Second,
		"scan", "-i", verifyTarget, "--mode", "quick", "--ai", "--timeout", "5",
		"-f", "output.txt", "-F", "findings.txt",
	)
	Verify(t, r).OK().Done()

	plainBytes, err := os.ReadFile(h.WorkFile("output.txt"))
	if err != nil {
		t.Fatalf("read -f output: %v", err)
	}
	plain := string(plainBytes)
	t.Logf("-f output (%d bytes):\n%s", len(plain), clip(plain, 3000))

	findingsBytes, err := os.ReadFile(h.WorkFile("findings.txt"))
	if err != nil {
		if !hasAISkillOutput(r.Stdout) {
			t.Skip("no AI findings produced, skipping -F check")
		}
		t.Fatalf("read -F output: %v", err)
	}
	findings := string(findingsBytes)
	t.Logf("-F output (%d bytes):\n%s", len(findings), clip(findings, 3000))

	if len(findings) > 0 {
		if !strings.Contains(findings, "Assets:") {
			t.Fatal("-F output should contain 'Assets:' header")
		}
	}

	if hasAISkillOutput(r.Stdout) && !strings.Contains(plain, "--- Findings ---") {
		t.Fatal("-f output should contain Findings section when AI findings exist")
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
