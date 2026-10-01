// Package jev implements the native TypeSafe choice, score and noul API.
// Reflexes, thresholds, policy and execution belong to consuming extensions.
package jev

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"time"

	aop "github.com/chainreactors/cyber/aop"
)

const Endpoint = "https://api.typesafe.ai/v1/systemone"
const DefaultModel = "jev-1.13.0"

// Question is the vendor's native question, not a second application protocol.
type Question struct {
	Type         string `json:"type"`
	Instructions any    `json:"instructions"`
	// Criteria is an option map for choice, an ordered array for score, or
	// optional true/false descriptions for noul. Values may be structured JSON.
	Criteria any `json:"criteria,omitempty"`
}
type Request struct {
	State     json.RawMessage     `json:"state"`
	Questions map[string]Question `json:"questions"`
}
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Noul          *float64           `json:"noul,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
}
type Response struct {
	Attempts uint64            `json:"-"`
	Model    string            `json:"model"`
	Answers  map[string]Answer `json:"answers"`
	Usage    *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (r *Response) TokenUsage() *aop.TokenUsage {
	if r == nil || r.Usage == nil {
		return nil
	}
	missing := uint64(0)
	if r.Attempts > 1 {
		missing = r.Attempts - 1
	}
	return &aop.TokenUsage{InputTokens: uint64(max(0, r.Usage.InputTokens)), OutputTokens: uint64(max(0, r.Usage.OutputTokens)), TotalTokens: uint64(max(0, r.Usage.InputTokens) + max(0, r.Usage.OutputTokens)), Detail: map[string]uint64{"requests": r.Attempts, "usage_missing": missing}}
}

type Client struct {
	APIKey   string
	Model    string
	Endpoint string
	Timeout  time.Duration
	http     *http.Client
	attempts atomic.Uint64
	input    atomic.Uint64
	output   atomic.Uint64
	missing  atomic.Uint64
}

func New(key, model string, timeout time.Duration) *Client {
	if model == "" {
		model = DefaultModel
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	// The deployed inference endpoint can stall HTTP/2 streams after accepting
	// TLS. Keep this provider on HTTP/1.1 with connection reuse; changing the
	// process-wide transport or increasing every request deadline hides the
	// failure and would affect unrelated model/tool traffic.
	transport := http.DefaultTransport
	if native, ok := transport.(*http.Transport); ok {
		native = native.Clone()
		native.Protocols = new(http.Protocols)
		native.Protocols.SetHTTP1(true)
		if native.TLSClientConfig == nil {
			native.TLSClientConfig = &tls.Config{}
		}
		// Clone can carry the default transport's already initialized h2 ALPN.
		// The TLS negotiation must agree with the selected HTTP protocol.
		native.TLSClientConfig.NextProtos = []string{"http/1.1"}
		transport = native
	}
	return &Client{APIKey: key, Model: model, Endpoint: Endpoint, Timeout: timeout, http: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *Client) Close() { c.http.CloseIdleConnections() }

// Usage includes all consumers and failed attempts. It is suitable for paired
// run accounting; missing vendor usage is explicit rather than assumed free.
func (c *Client) Usage() *aop.TokenUsage {
	i, o := c.input.Load(), c.output.Load()
	return &aop.TokenUsage{InputTokens: i, OutputTokens: o, TotalTokens: i + o, Detail: map[string]uint64{"requests": c.attempts.Load(), "usage_missing": c.missing.Load()}}
}

// Exchange preserves batched speculative questions. Callers validate only the
// selected answer heads; an unused speculative head cannot authorize effects.
func (c *Client) Exchange(ctx context.Context, input Request) (response *Response, resultErr error) {
	var attempts uint64
	defer func() {
		if response == nil {
			response = &Response{}
		}
		response.Attempts = attempts
	}()
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	if len(input.State) > 64<<10 || !json.Valid(input.State) || len(input.Questions) == 0 || len(input.Questions) > 40 {
		return nil, errors.New("invalid JEV request limits")
	}
	body, err := json.Marshal(map[string]any{"model": c.Model, "state": input.State, "questions": input.Questions})
	if err != nil {
		return nil, err
	}
	if len(body) > 128<<10 {
		return nil, errors.New("JEV request too large")
	}
	if c.APIKey != "" {
		body = bytes.ReplaceAll(body, []byte(c.APIKey), []byte("[REDACTED]"))
	}
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(1<<(attempt-1)) * 250 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", "application/json")
		c.attempts.Add(1)
		attempts++
		res, err := c.http.Do(req)
		if err != nil {
			c.missing.Add(1)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// A dropped connection does not produce a judgment. Retry this
			// inference only, within the same request budget; no tool is replayed.
			if attempt < 2 && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) {
				continue
			}
			message := err.Error()
			if c.APIKey != "" {
				message = strings.ReplaceAll(message, c.APIKey, "[REDACTED]")
			}
			return nil, fmt.Errorf("JEV request failed: %s", message)
		}
		raw, readErr := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
		res.Body.Close()
		if readErr != nil || len(raw) > 1<<20 {
			c.missing.Add(1)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt < 2 && (errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF)) {
				continue
			}
			return nil, errors.New("invalid JEV response body")
		}
		if (res.StatusCode == 429 || res.StatusCode == 529) && attempt < 2 {
			c.missing.Add(1)
			continue
		}
		if res.StatusCode != 200 {
			c.missing.Add(1)
			return nil, fmt.Errorf("JEV HTTP status %d", res.StatusCode)
		}
		var out Response
		if json.Unmarshal(raw, &out) != nil {
			c.missing.Add(1)
			return nil, errors.New("invalid JEV response")
		}
		if out.Usage != nil && (out.Usage.InputTokens < 0 || out.Usage.OutputTokens < 0) {
			c.missing.Add(1)
			return nil, errors.New("invalid JEV usage")
		}
		if usage := out.TokenUsage(); usage != nil {
			c.input.Add(usage.InputTokens)
			c.output.Add(usage.OutputTokens)
		} else {
			c.missing.Add(1)
		}
		return &out, nil
	}
	return nil, errors.New("JEV retry limit reached")
}

func (r *Response) Choice(name string, question Question) (string, error) {
	if r == nil {
		return "", errors.New("missing JEV response")
	}
	a, ok := r.Answers[name]
	if question.Type != "choice" || !ok || a.Type != "choice" {
		return "", errors.New("invalid JEV choice response")
	}
	options := reflect.ValueOf(question.Criteria)
	if !options.IsValid() || options.Kind() != reflect.Map || options.Type().Key().Kind() != reflect.String {
		return "", errors.New("invalid JEV choice criteria")
	}
	key := reflect.ValueOf(a.Choice).Convert(options.Type().Key())
	if !options.MapIndex(key).IsValid() {
		return "", errors.New("invalid JEV choice binding")
	}
	return a.Choice, nil
}

// Score returns the native weighted level index, not an application verdict.
func (r *Response) Score(name string, question Question) (float64, error) {
	levels := reflect.ValueOf(question.Criteria)
	if question.Type != "score" || !levels.IsValid() || (levels.Kind() != reflect.Slice && levels.Kind() != reflect.Array) || (levels.Kind() == reflect.Slice && levels.Type().Elem().Kind() == reflect.Uint8) || levels.Len() < 2 || levels.Len() > 10 {
		return 0, errors.New("invalid JEV score criteria")
	}
	return r.number(name, "score", float64(levels.Len()-1))
}

// Noul returns a probability. The caller owns any threshold or consequence.
func (r *Response) Noul(name string) (float64, error) {
	return r.number(name, "noul", 1)
}

func (r *Response) number(name, kind string, upper float64) (float64, error) {
	if r == nil {
		return 0, errors.New("missing JEV response")
	}
	a, ok := r.Answers[name]
	value := a.Noul
	if kind == "score" {
		value = a.Score
	}
	if !ok || a.Type != kind || value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > upper {
		return 0, fmt.Errorf("invalid JEV %s response", kind)
	}
	return *value, nil
}
