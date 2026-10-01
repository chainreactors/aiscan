package jev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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
		_, _ = w.Write([]byte(`{"answers":{"q":{"type":"choice","choice":"yes"}},"usage":{"input_tokens":20,"output_tokens":1}}`))
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
		out, err := client.Exchange(t.Context(), Request{State: json.RawMessage(`{}`), Questions: map[string]Question{"q": {Type: "choice", Criteria: map[string]string{"yes": "Ready"}}}})
		if err != nil || out.Answers["q"].Choice != "yes" {
			t.Fatalf("response=%v err=%v", out, err)
		}
	}
	if requests.Load() != 2 || connections.Load() != 1 {
		t.Fatalf("requests=%d connections=%d: inference connection was not reused", requests.Load(), connections.Load())
	}
}

func TestNativePrimitivesPreserveStructuredQuestionsAndAnswers(t *testing.T) {
	questions := map[string]Question{
		"route":   {Type: "choice", Instructions: map[string]any{"question": "Select a route", "context": []string{"current goal"}}, Criteria: map[string]any{"browser": map[string]string{"description": "Interactive page"}, "other": nil}},
		"impact":  {Type: "score", Instructions: "Rate impact", Criteria: []any{"none", map[string]string{"description": "bounded"}, "large"}},
		"present": {Type: "noul", Instructions: "Is the element present?"},
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
	out, err := client.Exchange(t.Context(), Request{State: json.RawMessage(`{"page":"current"}`), Questions: questions})
	if err != nil {
		t.Fatal(err)
	}
	if choice, err := out.Choice("route", questions["route"]); err != nil || choice != "browser" {
		t.Fatalf("choice: %q %v", choice, err)
	}
	if score, err := out.Score("impact", questions["impact"]); err != nil || score != 1.25 {
		t.Fatalf("score: %v %v", score, err)
	}
	if probability, err := out.Noul("present"); err != nil || probability != 0 {
		t.Fatalf("noul: %v %v", probability, err)
	}
	if out.Answers["impact"].Legend["1"] != "bounded" || out.Answers["route"].Probabilities["browser"] != 0.8 {
		t.Fatal("native evidence lost")
	}
}

func TestNumericAnswersRejectMissingWrongTypeAndOutOfRangeValues(t *testing.T) {
	for _, kind := range []string{"score", "noul"} {
		for _, test := range []struct {
			name       string
			value      *float64
			answerType string
		}{
			{"missing", nil, kind}, {"negative", new(-0.1), kind}, {"too large", new(1.1), kind},
			{"nan", new(math.NaN()), kind}, {"infinite", new(math.Inf(1)), kind}, {"wrong type", new(0.5), "choice"},
		} {
			t.Run(kind+"/"+test.name, func(t *testing.T) {
				answer := Answer{Type: test.answerType, Score: test.value, Noul: test.value}
				out := &Response{Answers: map[string]Answer{"q": answer}}
				var err error
				if kind == "score" {
					_, err = out.Score("q", Question{Type: "score", Criteria: []string{"low", "high"}})
				} else {
					_, err = out.Noul("q")
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
		var req Request
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
	q := map[string]Question{"entry": {Type: "choice", Criteria: map[string]string{"run": "run"}}, "run": {Type: "choice", Criteria: map[string]string{"go": "go"}}}
	out, err := c.Exchange(t.Context(), Request{State: json.RawMessage(`{}`), Questions: q})
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
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer s.Close()
	c := New("key", DefaultModel, time.Second)
	defer c.Close()
	c.Endpoint = s.URL
	_, err := c.Exchange(t.Context(), Request{State: json.RawMessage(`{}`), Questions: map[string]Question{"q": {Type: "choice"}}})
	if err == nil || targetCalls.Load() != 0 {
		t.Fatal("redirect followed")
	}
	out := &Response{Answers: map[string]Answer{"q": {Type: "choice", Choice: "unknown"}}}
	if _, err = out.Choice("q", Question{Type: "choice", Criteria: map[string]string{"known": "known"}}); err == nil {
		t.Fatal("unbound answer accepted")
	}
}

func TestDroppedConnectionsRetryTheSameJudgmentAndAccountForUsage(t *testing.T) {
	for _, stage := range []string{"before_headers", "partial_body"} {
		t.Run(stage, func(t *testing.T) {
			var calls atomic.Int64
			body := `{"answers":{"q":{"type":"choice","choice":"yes"}},"usage":{"input_tokens":20,"output_tokens":1}}`
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req Request
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
			q := Question{Type: "choice", Criteria: map[string]string{"yes": "ready", "no": "not ready"}}
			out, err := c.Exchange(t.Context(), Request{State: json.RawMessage(`{"ready":true}`), Questions: map[string]Question{"q": q}})
			if err != nil {
				t.Fatal(err)
			}
			if choice, err := out.Choice("q", q); err != nil || choice != "yes" || calls.Load() != 2 || out.Attempts != 2 || out.TokenUsage().Detail["usage_missing"] != 1 || c.Usage().Detail["usage_missing"] != 1 || c.Usage().InputTokens != 20 {
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
			out, err := c.Exchange(t.Context(), Request{State: json.RawMessage(`{}`), Questions: map[string]Question{"q": {Type: "choice"}}})
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
