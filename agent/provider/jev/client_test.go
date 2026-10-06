package jev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chainreactors/cyber/internal/jevwire"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientUsesReusableHTTP1WhenServerAlsoOffersHTTP2(t *testing.T) {
	var connections, requests atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.ProtoMajor != 1 {
			t.Errorf("unexpected inference protocol: %s", r.Proto)
		}
		_, _ = w.Write([]byte(`{"answers":{"claim":{"type":"choice","choice":"yes"}},"usage":{"input_tokens":20,"output_tokens":1}}`))
	}))
	server.EnableHTTP2 = true
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.StartTLS()
	defer server.Close()
	client := New("test-key", "", time.Second)
	defer client.Close()
	client.Endpoint = server.URL
	transport := client.http.Transport.(*http.Transport)
	// Keep the provider's ALPN settings; replacing the entire TLS config would
	// conceal a mismatch inherited from an initialized default transport.
	transport.TLSClientConfig.RootCAs = server.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	for range 2 {
		out, err := client.Choice(t.Context(), &Claim{Type: ClaimChoice, Context: "Ready?", Options: []string{"yes", "no"}})
		if err != nil || out != "yes" {
			t.Fatalf("response=%v err=%v", out, err)
		}
	}
	if requests.Load() != 2 || connections.Load() != 1 {
		t.Fatalf("requests=%d connections=%d: inference connection was not reused", requests.Load(), connections.Load())
	}
}

func TestEvaluatePreservesTypedClaimsAndNativeMetadata(t *testing.T) {
	claims := map[string]Claim{
		"route":   {Type: ClaimChoice, Context: "Select browser for an interactive page, otherwise other.", Options: []string{"browser", "other"}},
		"impact":  {Type: ClaimScore, Context: "Rate impact from none to large.", Options: []string{"none", "bounded", "large"}},
		"present": {Type: ClaimNoul, Context: "Is the element present?"},
	}
	questions := map[string]jevwire.Question{
		"route":   {Type: "choice", Instructions: claims["route"].Context, Criteria: json.RawMessage(`{"browser":"browser","other":"other"}`)},
		"impact":  {Type: "score", Instructions: claims["impact"].Context, Criteria: json.RawMessage(`["none","bounded","large"]`)},
		"present": {Type: "noul", Instructions: claims["present"].Context},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model     string                     `json:"model"`
			Questions map[string]json.RawMessage `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if request.Model != DefaultModel || len(request.Questions) != 3 {
			t.Error("native batch was changed")
		}
		for id, question := range questions {
			want, _ := json.Marshal(question)
			if string(request.Questions[id]) != string(want) {
				t.Errorf("question %s changed: %s", id, request.Questions[id])
			}
		}
		_, _ = w.Write([]byte(`{"answers":{"route":{"type":"choice","choice":"browser","probabilities":{"browser":0.8,"other":0.2},"confidence":0.6},"impact":{"type":"score","score":1.25,"legend":{"0":"none","1":"bounded","2":"large"},"probabilities":{"0":0,"1":0.75,"2":0.25},"confidence":0.5},"present":{"type":"noul","noul":0}},"usage":{"input_tokens":24,"output_tokens":12}}`))
	}))
	defer server.Close()
	client := New("key", DefaultModel, time.Second)
	defer client.Close()
	client.Endpoint = server.URL
	out, err := client.Evaluate(t.Context(), claims)
	if err != nil {
		t.Fatal(err)
	}
	if choice, err := out.Choice("route", claims["route"]); err != nil || choice != "browser" {
		t.Fatalf("choice: %q %v", choice, err)
	}
	if score, err := out.Score("impact", claims["impact"]); err != nil || score != 1.25 {
		t.Fatalf("score: %v %v", score, err)
	}
	if probability, err := out.Noul("present", claims["present"]); err != nil || probability != 0 {
		t.Fatalf("noul: %v %v", probability, err)
	}
	if out.Values["impact"].Probabilities["1"] != 0.75 || out.Values["route"].Probabilities["browser"] != 0.8 || out.Values["route"].Confidence != 0.6 {
		t.Fatal("native evidence lost")
	}
}

func TestNumericAnswersRejectMissingWrongTypeAndOutOfRangeValues(t *testing.T) {
	for _, kind := range []ClaimType{ClaimScore, ClaimNoul} {
		for _, head := range []string{
			`{"type":"` + kind.String() + `"}`, `{"type":"choice","choice":"yes"}`,
			`{"type":"` + kind.String() + `","` + kind.String() + `":-0.1}`,
			`{"type":"` + kind.String() + `","` + kind.String() + `":1.1}`,
			`{"type":"` + kind.String() + `","` + kind.String() + `":"NaN"}`,
			`{"type":"` + kind.String() + `","` + kind.String() + `":1e400}`,
		} {
			t.Run(head, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte(`{"answers":{"claim":` + head + `}}`))
				}))
				defer server.Close()
				client := New("", "", time.Second)
				defer client.Close()
				client.Endpoint = server.URL
				claim := &Claim{Type: kind, Context: "Evaluate current evidence"}
				var err error
				if kind == ClaimScore {
					claim.Options = []string{"low", "high"}
					_, err = client.Score(t.Context(), claim)
				} else {
					_, err = client.Noul(t.Context(), claim)
				}
				if err == nil {
					t.Fatal("invalid numeric answer accepted")
				}
			})
		}
	}
}

func TestRetriesAccountForMissingUsageAndPreserveBatch(t *testing.T) {
	var calls atomic.Int64
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jevwire.Request
		if json.NewDecoder(r.Body).Decode(&req) != nil || len(req.Questions) != 2 {
			t.Error("batch not preserved")
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(429)
			return
		}
		_, _ = w.Write([]byte(`{"answers":{"entry":{"type":"choice","choice":"run"},"run":{"type":"choice","choice":"go"}},"usage":{"input_tokens":20,"output_tokens":0}}`))
	}))
	defer s.Close()
	c := New("key", DefaultModel, time.Second)
	defer c.Close()
	c.Endpoint = s.URL
	q := map[string]jevwire.Question{"entry": {Type: "choice", Criteria: json.RawMessage(`{"run":"run"}`)}, "run": {Type: "choice", Criteria: json.RawMessage(`{"go":"go"}`)}}
	out, err := c.exchange(t.Context(), jevwire.Request{State: json.RawMessage(`{}`), Questions: q})
	if err != nil {
		t.Fatal(err)
	}
	if out.Attempts != 2 || out.TokenUsage().Detail["usage_missing"] != 1 || c.Usage().Detail["usage_missing"] != 1 || c.Usage().InputTokens != 20 {
		t.Fatalf("incomplete retry accounting: %v %v", out, c.Usage())
	}
}
func TestClientRejectsRedirectsAndInvalidSelectedAnswers(t *testing.T) {
	var targetCalls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer s.Close()
	c := New("key", DefaultModel, time.Second)
	defer c.Close()
	c.Endpoint = s.URL
	_, err := c.exchange(t.Context(), jevwire.Request{State: json.RawMessage(`{}`), Questions: map[string]jevwire.Question{"q": {Type: "choice"}}})
	if err == nil || targetCalls.Load() != 0 {
		t.Fatal("redirect followed")
	}
	invalid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"answers":{"claim":{"type":"choice","choice":"unknown"}}}`))
	}))
	defer invalid.Close()
	c.Endpoint = invalid.URL
	if _, err = c.Choice(t.Context(), &Claim{Type: ClaimChoice, Context: "Choose a known result", Options: []string{"known", "defer"}}); err == nil {
		t.Fatal("unbound answer accepted")
	}
}

func TestDroppedConnectionsRetryTheSameJudgmentAndAccountForUsage(t *testing.T) {
	for _, stage := range []string{"before_headers", "partial_body"} {
		t.Run(stage, func(t *testing.T) {
			var calls atomic.Int64
			body := `{"answers":{"q":{"type":"choice","choice":"yes"}},"usage":{"input_tokens":20,"output_tokens":1}}`
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req jevwire.Request
				if json.NewDecoder(r.Body).Decode(&req) != nil || len(req.Questions) != 1 || string(req.State) != `{"ready":true}` {
					t.Error("retry changed the judgment")
				}
				if calls.Add(1) == 1 {
					if stage == "before_headers" {
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						_ = conn.Close()
					} else {
						w.Header().Set("Content-Length", fmt.Sprint(len(body)))
						_, _ = w.Write([]byte(body[:len(body)/2]))
					}
					return
				}
				_, _ = w.Write([]byte(body))
			}))
			defer s.Close()
			c := New("key", DefaultModel, 2*time.Second)
			defer c.Close()
			c.Endpoint = s.URL
			q := Claim{Type: ClaimChoice, Context: "Is it ready?" + jevwire.EvidenceMarker + `{"ready":true}`, Options: []string{"yes", "no"}}
			out, err := c.Evaluate(t.Context(), map[string]Claim{"q": q})
			if err != nil {
				t.Fatal(err)
			}
			if choice, err := out.Choice("q", q); err != nil || choice != "yes" || calls.Load() != 2 || out.TokenUsage().Detail["requests"] != 2 || out.TokenUsage().Detail["usage_missing"] != 1 || c.Usage().Detail["usage_missing"] != 1 || c.Usage().InputTokens != 20 {
				t.Fatalf("choice=%s error=%v calls=%d usage=%v", choice, err, calls.Load(), c.Usage())
			}
		})
	}
}

func TestDroppedConnectionRetriesRespectAttemptAndTimeBudgets(t *testing.T) {
	for _, budget := range []time.Duration{3 * time.Second, 100 * time.Millisecond} {
		t.Run(budget.String(), func(t *testing.T) {
			var calls atomic.Int64
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = conn.Close()
			}))
			defer s.Close()
			c := New("key", DefaultModel, budget)
			defer c.Close()
			c.Endpoint = s.URL
			out, err := c.exchange(t.Context(), jevwire.Request{State: json.RawMessage(`{}`), Questions: map[string]jevwire.Question{"q": {Type: "choice"}}})
			want := int64(3)
			if budget < 250*time.Millisecond {
				want = 1
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("lost request deadline: %v", err)
				}
			}
			if err == nil || calls.Load() != want || out.Attempts != uint64(want) || c.Usage().Detail["usage_missing"] != uint64(want) {
				t.Fatalf("calls=%d attempts=%d usage=%v error=%v", calls.Load(), out.Attempts, c.Usage(), err)
			}
		})
	}
}
