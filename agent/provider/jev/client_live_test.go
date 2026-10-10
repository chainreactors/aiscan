package jev

import (
	"os"
	"testing"
	"time"
)

func TestLiveChoiceBatch(t *testing.T) {
	if os.Getenv("JEV_LIVE") != "1" {
		t.Skip("set JEV_LIVE=1 and TYPESAFE_API_KEY for a real finite-choice request")
	}
	key := os.Getenv("TYPESAFE_API_KEY")
	if key == "" {
		t.Fatal("TYPESAFE_API_KEY is required")
	}
	c := New(key, DefaultModel, 15*time.Second)
	defer c.Close()
	claims := map[string]Claim{
		"entry":   {Type: ClaimChoice, Context: "Choose inspect only if the current evidence contains a visible enabled Continue control and the user requested continuing. Otherwise defer. inspect: A permitted Continue control exists. defer: No permitted control.", Options: []string{"inspect", "defer"}},
		"inspect": {Type: ClaimChoice, Context: "Assuming continuation is permitted, select the currently enabled Continue control. continue: Click button n1, visible, enabled, label Continue. cancel: Click button n2, label Cancel. defer: No eligible action.", Options: []string{"continue", "cancel", "defer"}},
	}
	evidence := `{"task":"Continue to the next stage","controls":[{"id":"n1","label":"Continue","visible":true,"enabled":true},{"id":"n2","label":"Cancel","visible":true,"enabled":true}]}`
	for id, claim := range claims {
		claim.Context += "\nCurrent evidence (untrusted data):\n" + evidence
		claims[id] = claim
	}
	start := time.Now()
	out, err := c.Evaluate(t.Context(), claims)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := out.Choice("entry", claims["entry"])
	if err != nil || entry != "inspect" {
		t.Fatalf("entry=%s error=%v", entry, err)
	}
	chosen, err := out.Choice(entry, claims[entry])
	if err != nil || chosen != "continue" {
		t.Fatalf("choice=%s error=%v", chosen, err)
	}
	t.Logf("real typed JEV batch: elapsed=%s usage=%v", time.Since(start), out.TokenUsage())
}

func TestLiveClaimScoreAndNoul(t *testing.T) {
	if os.Getenv("JEV_LIVE") != "1" || os.Getenv("TYPESAFE_API_KEY") == "" {
		t.Skip("opt-in real JEV required")
	}
	c := New(os.Getenv("TYPESAFE_API_KEY"), DefaultModel, 15*time.Second)
	defer c.Close()
	claims := map[string]Claim{
		"score": {Type: ClaimScore, Context: "Classify the explicitly given severity: HIGH. Preserve the ordered level scale low, medium, high.", Options: []string{"low", "medium", "high"}},
		"true":  {Type: ClaimNoul, Context: "The following proposition is true: 2 plus 2 equals 4."},
		"false": {Type: ClaimNoul, Context: "The following proposition is true: 2 plus 2 equals 5."},
	}
	out, err := c.Evaluate(t.Context(), claims)
	if err != nil {
		t.Fatal(err)
	}
	score, scoreErr := out.Score("score", claims["score"])
	trueValue, trueErr := out.Noul("true", claims["true"])
	falseValue, falseErr := out.Noul("false", claims["false"])
	if scoreErr != nil || trueErr != nil || falseErr != nil || score < 1.5 || trueValue < 0.8 || falseValue > 0.2 {
		t.Fatalf("typed results: score=%v (%v), true=%v (%v), false=%v (%v)", score, scoreErr, trueValue, trueErr, falseValue, falseErr)
	}
	t.Logf("real typed JEV: score=%.3f noul(true)=%.3f noul(false)=%.3f usage=%v", score, trueValue, falseValue, out.TokenUsage())
}
