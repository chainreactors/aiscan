package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

const defaultTimeout = 120 * time.Second

func timeoutFromConfig(seconds int) time.Duration {
	if seconds <= 0 {
		return defaultTimeout
	}
	return time.Duration(seconds) * time.Second
}

type apiRequest struct {
	client  *http.Client
	timeout time.Duration
}

func (r *apiRequest) do(ctx context.Context, method, endpoint string, body []byte, setHeaders func(*http.Request)) ([]byte, error) {
	parentCtx := ctx
	var callTimedOut atomic.Bool
	if r.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		timer := time.AfterFunc(r.timeout, func() { callTimedOut.Store(true); cancel() })
		defer func() { timer.Stop(); cancel() }()
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if setHeaders != nil {
		setHeaders(req)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, wrapReadError(parentCtx, callTimedOut.Load(), r.timeout, "http request", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, wrapReadError(parentCtx, callTimedOut.Load(), r.timeout, "read response", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Message: truncateStr(string(data), 512)}
	}
	return data, nil
}

func doJSON(ctx context.Context, client *http.Client, timeout time.Duration, method, endpoint string, payload any, setHeaders func(*http.Request)) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	return (&apiRequest{client: client, timeout: timeout}).do(ctx, method, endpoint, body, setHeaders)
}

func wrapReadError(parentCtx context.Context, timedOut bool, timeout time.Duration, op string, err error) error {
	if timedOut && parentCtx.Err() == nil {
		return fmt.Errorf("%s: %w after %s: %v", op, ErrCallTimeout, timeout, err)
	}
	if errors.Is(err, context.Canceled) && parentCtx.Err() == nil {
		return fmt.Errorf("%s: %w", op, ErrCallTimeout)
	}
	return fmt.Errorf("%s: %w", op, err)
}

func readAllWithCancelTimeout(r io.Reader, cancel context.CancelFunc, timeout time.Duration) ([]byte, bool, error) {
	var timedOut atomic.Bool
	timer := time.AfterFunc(timeout, func() {
		timedOut.Store(true)
		cancel()
	})
	defer timer.Stop()
	body, err := io.ReadAll(r)
	return body, timedOut.Load(), err
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
