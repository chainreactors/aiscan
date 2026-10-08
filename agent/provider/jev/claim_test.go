package jev

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/cyber/core/decision"
)

func TestClaimDecisionMethodsUseClosedTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		var request struct {
			Questions map[string]inferenceQuestion `json:"questions"`
		}
		if err := json.Unmarshal(data, &request); err != nil {
			t.Fatal(err)
		}
		q := request.Questions["claim"]
		var body string
		switch q.Type {
		case "choice":
			body = `{"answers":{"claim":{"type":"choice","choice":"inspect"}}}`
		case "score":
			body = `{"answers":{"claim":{"type":"score","score":1.5}}}`
		case "noul":
			body = `{"answers":{"claim":{"type":"noul","noul":0.75}}}`
		default:
			t.Fatalf("unexpected wire type %q", q.Type)
		}
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	client := New("", "", time.Second)
	defer client.Close()
	client.Endpoint = server.URL
	if got, err := client.Choice(context.Background(), &Claim{Type: ClaimChoice, Context: "What next?", Options: []string{"inspect", "defer"}}); err != nil || got != "inspect" {
		t.Fatalf("choice=%q err=%v", got, err)
	}
	if got, err := client.Score(context.Background(), &Claim{Type: ClaimScore, Context: "How severe?", Options: []string{"low", "medium", "high"}}); err != nil || got != 1.5 {
		t.Fatalf("score=%v err=%v", got, err)
	}
	if got, err := client.Noul(context.Background(), &Claim{Type: ClaimNoul, Context: "Is it complete?"}); err != nil || got != 0.75 {
		t.Fatalf("noul=%v err=%v", got, err)
	}
}

func TestClaimJSONRejectsLegacyAndInvalidDefinitions(t *testing.T) {
	for _, raw := range []string{
		`{"text":"legacy"}`,
		`{"type":"noul","context":"valid","question":"legacy"}`,
		`{"type":"noul","context":"valid","criteria":{}}`,
		`{"type":"unknown","context":"valid"}`,
		`{"type":1,"context":"valid"}`,
		`{"type":"noul","context":" "}`,
		`{"type":"noul","context":"valid","options":["yes","no"]}`,
		`{"type":"choice","context":"valid","options":["only"]}`,
		`{"type":"choice","context":"valid","options":["same","same"]}`,
		`{"type":"score","context":"valid","options":["low",""]}`,
	} {
		t.Run(raw, func(t *testing.T) {
			original := Claim{Type: ClaimNoul, Context: "original"}
			c := original
			if json.Unmarshal([]byte(raw), &c) == nil || !reflect.DeepEqual(c, original) {
				t.Fatal("invalid definition accepted or mutated its destination")
			}
		})
	}
	for _, c := range []Claim{
		{Type: ClaimNoul, Context: strings.Repeat("x", (64<<10)+1)},
		{Type: ClaimChoice, Context: "Choose", Options: []string{strings.Repeat("x", 1025), "other"}},
		{Type: ClaimScore, Context: "Score", Options: make([]string, 11)},
		{Type: ClaimChoice, Context: "Choose", Options: make([]string, 65)},
	} {
		if c.Validate() == nil {
			t.Fatal("unbounded Claim accepted")
		}
	}
}

func TestClaimSerializationPreservesOrderedMeaning(t *testing.T) {
	c := Claim{Type: ClaimScore, Context: "请求的严重程度：从低到高。", Options: []string{"low", "medium", "high"}}
	data, err := json.Marshal(c)
	if err != nil || !strings.Contains(string(data), `"type":"score"`) {
		t.Fatal("Claim type is not a textual closed type", err)
	}
	var decoded Claim
	if err := json.Unmarshal(data, &decoded); err != nil || !reflect.DeepEqual(decoded, c) {
		t.Fatal("ordered Claim changed during serialization", err)
	}
	p := c.Proto()
	p.Options[0] = "changed"
	if c.Options[0] != "low" {
		t.Fatal("protobuf projection shares mutable option storage")
	}
}

func TestEvaluationRejectsWrongHeadsAndUnboundedNumbers(t *testing.T) {
	choice := Claim{Type: ClaimChoice, Context: "Choose", Options: []string{"read", "defer"}}
	for _, value := range []*Evaluation{nil, {}, {Value: &decision.Evaluation_Noul{Noul: 1}}, {Value: &decision.Evaluation_Choice{Choice: "invented"}}} {
		if _, err := choice.Choice(value); err == nil {
			t.Fatal("invalid choice authorized a binding")
		}
	}
	score := Claim{Type: ClaimScore, Context: "Severity", Options: []string{"low", "medium", "high"}}
	noul := Claim{Type: ClaimNoul, Context: "Complete"}
	for _, number := range []float64{-0.1, 2.1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := score.Score(&Evaluation{Value: &decision.Evaluation_Score{Score: number}}); err == nil {
			t.Fatal("invalid score accepted", number)
		}
	}
	for _, number := range []float64{-0.1, 1.1, math.NaN(), math.Inf(1)} {
		if _, err := noul.Noul(&Evaluation{Value: &decision.Evaluation_Noul{Noul: number}}); err == nil {
			t.Fatal("invalid noul accepted", number)
		}
	}
	if _, err := score.Score(&Evaluation{Value: &decision.Evaluation_Noul{Noul: 0.5}}); err == nil {
		t.Fatal("noul used as score")
	}
	for _, number := range []float64{0, 2} {
		if got, err := score.Score(&Evaluation{Value: &decision.Evaluation_Score{Score: number}}); got != number || err != nil {
			t.Fatal("valid score boundary rejected", number, err)
		}
	}
}

func TestEvaluateSharesOnlyExplicitIdenticalEvidence(t *testing.T) {
	var requests []inferenceRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope inferenceRequest
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Error(err)
		}
		requests = append(requests, envelope)
		fmtBody := `{"answers":{}}`
		_, _ = w.Write([]byte(fmtBody))
	}))
	defer server.Close()
	client := New("", "", time.Second)
	defer client.Close()
	client.Endpoint = server.URL
	contexts := [][2]string{
		{"First\n{\"fact\":1}", "Second\n{\"fact\":1}"},
		{"First" + evidenceMarker + "{\n\"fact\":1\n}", "Second" + evidenceMarker + "{\n\"fact\":1\n}"},
		{"First" + evidenceMarker + `{"fact":1}`, "Second" + evidenceMarker + `{"fact":2}`},
	}
	for _, pair := range contexts {
		claims := map[string]Claim{"a": {Type: ClaimNoul, Context: pair[0]}, "b": {Type: ClaimNoul, Context: pair[1]}}
		if _, err := client.Evaluate(t.Context(), claims); err != nil {
			t.Fatal(err)
		}
		if claims["a"].Context != pair[0] || claims["b"].Context != pair[1] {
			t.Fatal("transport changed Claim context")
		}
	}
	for _, i := range []int{0, 2} {
		if string(requests[i].State) != `{}` || requests[i].Questions["a"].Instructions != contexts[i][0] || requests[i].Questions["b"].Instructions != contexts[i][1] {
			t.Fatal("ordinary or different evidence was silently removed")
		}
	}
	if requests[1].Questions["a"].Instructions != "First" || requests[1].Questions["b"].Instructions != "Second" || !strings.Contains(string(requests[1].State), `"fact":1`) {
		t.Fatal("explicit identical evidence was not shared")
	}
}
