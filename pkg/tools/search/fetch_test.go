package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchToolRequiresURL(t *testing.T) {
	tool := NewFetchTool()
	_, err := tool.Execute(context.Background(), `{}`)
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("expected url error, got %v", err)
	}

	_, err = tool.Execute(context.Background(), `{"url":"   "}`)
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("expected blank url error, got %v", err)
	}
}

func TestFetchToolRejectsUnknownArguments(t *testing.T) {
	tool := NewFetchTool()
	_, err := tool.Execute(context.Background(), `{"url":"https://example.com","method":"POST"}`)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestFetchToolPreservesExplicitHTTPURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><h1>HTTP target</h1><p>plain service</p></body></html>"))
	}))
	defer server.Close()

	tool := NewFetchTool()
	args, _ := json.Marshal(fetchToolArgs{URL: server.URL})
	result, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	out := result.Text()
	if !strings.Contains(out, "Fetched: "+server.URL) {
		t.Fatalf("output = %q, want explicit http URL", out)
	}
	if !strings.Contains(out, "HTTP target") {
		t.Fatalf("output = %q, want page content", out)
	}
}

func TestFetchToolCacheHit(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello world"))
	}))
	defer server.Close()

	tool := NewFetchTool()
	args, _ := json.Marshal(fetchToolArgs{URL: server.URL})

	result1, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	result2, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}

	if callCount != 1 {
		t.Fatalf("expected 1 HTTP call, got %d (cache miss)", callCount)
	}
	if result1.Text() != result2.Text() {
		t.Fatal("cached response differs from original")
	}
}

func TestFetchToolClearCache(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("data"))
	}))
	defer server.Close()

	tool := NewFetchTool()
	args, _ := json.Marshal(fetchToolArgs{URL: server.URL})

	if _, err := tool.Execute(context.Background(), string(args)); err != nil {
		t.Fatal(err)
	}
	tool.ClearCache()
	if _, err := tool.Execute(context.Background(), string(args)); err != nil {
		t.Fatal(err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 HTTP calls after cache clear, got %d", callCount)
	}
}

func TestFetchCacheMarksRecentGetsAsMostRecent(t *testing.T) {
	cache := newURLCache()
	cache.Set("a", &cacheEntry{content: "a", size: 1, fetchedAt: time.Now()})
	cache.Set("b", &cacheEntry{content: "b", size: 1, fetchedAt: time.Now()})

	if _, ok := cache.Get("a"); !ok {
		t.Fatal("expected cache hit for a")
	}
	cache.Set("a", &cacheEntry{content: "new-a", size: 2, fetchedAt: time.Now()})

	if got, want := strings.Join(cache.order, ","), "b,a"; got != want {
		t.Fatalf("cache order = %q, want %q", got, want)
	}
	if cache.totalSize != 3 {
		t.Fatalf("totalSize = %d, want 3", cache.totalSize)
	}
}

func TestFetchToolSameHostRedirect(t *testing.T) {
	mux := http.NewServeMux()
	var serverURL string
	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, serverURL+"/new", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/new", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("landed"))
	})
	server := httptest.NewServer(mux)
	serverURL = server.URL
	defer server.Close()

	tool := NewFetchTool()
	args, _ := json.Marshal(fetchToolArgs{URL: server.URL + "/old"})
	result, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result.Text(), "landed") {
		t.Fatalf("output = %q, expected content from /new", result.Text())
	}
}

func TestFetchToolSeeOtherRedirect(t *testing.T) {
	mux := http.NewServeMux()
	var serverURL string
	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, serverURL+"/new", http.StatusSeeOther)
	})
	mux.HandleFunc("/new", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("see other landed"))
	})
	server := httptest.NewServer(mux)
	serverURL = server.URL
	defer server.Close()

	tool := NewFetchTool()
	args, _ := json.Marshal(fetchToolArgs{URL: server.URL + "/old"})
	result, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result.Text(), "see other landed") {
		t.Fatalf("output = %q, expected content from /new", result.Text())
	}
}

func TestFetchToolCrossHostRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://other.example.com/page", http.StatusFound)
	}))
	defer server.Close()

	tool := NewFetchTool()
	args, _ := json.Marshal(fetchToolArgs{URL: server.URL})
	result, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	out := result.Text()
	if !strings.Contains(out, "REDIRECT DETECTED") {
		t.Fatalf("output = %q, want redirect message", out)
	}
	if !strings.Contains(out, "other.example.com") {
		t.Fatalf("output = %q, want redirect URL", out)
	}
}

func TestFetchValidateURLRejectsLongURL(t *testing.T) {
	long := "https://example.com/" + strings.Repeat("a", maxURLLength)
	err := validateURL(long)
	if err == nil {
		t.Fatal("expected error for long URL")
	}
	if !strings.Contains(err.Error(), "too long") {
		t.Fatalf("error = %q, want 'too long'", err)
	}
}

func TestFetchValidateURLRejectsUserInfo(t *testing.T) {
	err := validateURL("https://user:pass@example.com/")
	if err == nil {
		t.Fatal("expected error for URL with userinfo")
	}
	if !strings.Contains(err.Error(), "username") {
		t.Fatalf("error = %q, want 'username'", err)
	}
}

func TestFetchValidateURLRejectsSinglePartHostname(t *testing.T) {
	err := validateURL("https://localhost/path")
	if err == nil {
		t.Fatal("expected error for single-part hostname")
	}
}

func TestFetchBinaryContentDetection(t *testing.T) {
	for _, ct := range []string{
		"application/pdf",
		"image/png",
		"image/jpeg",
		"audio/mpeg",
		"video/mp4",
		"application/zip",
		"application/octet-stream",
	} {
		if !isBinaryContentType(ct) {
			t.Errorf("isBinaryContentType(%q) = false, want true", ct)
		}
	}
	for _, ct := range []string{
		"text/html",
		"text/plain",
		"application/json",
		"text/xml",
	} {
		if isBinaryContentType(ct) {
			t.Errorf("isBinaryContentType(%q) = true, want false", ct)
		}
	}
}

func TestFetchToolBinaryContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.4 fake pdf content"))
	}))
	defer server.Close()

	tool := NewFetchTool()
	args, _ := json.Marshal(fetchToolArgs{URL: server.URL})
	result, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	out := result.Text()
	if !strings.Contains(out, "Binary content") {
		t.Fatalf("output = %q, want binary content notice", out)
	}
	if !strings.Contains(out, "application/pdf") {
		t.Fatalf("output = %q, want content-type", out)
	}
}

func TestFetchToolBinaryContentCached(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.4 fake pdf content"))
	}))
	defer server.Close()

	tool := NewFetchTool()
	args, _ := json.Marshal(fetchToolArgs{URL: server.URL})

	result1, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	result2, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 HTTP call, got %d", callCount)
	}
	if result1.Text() != result2.Text() {
		t.Fatal("cached binary response differs from original")
	}
}

func TestFetchIsPermittedRedirect(t *testing.T) {
	tests := []struct {
		original string
		redirect string
		want     bool
	}{
		{"https://example.com/a", "https://example.com/b", true},
		{"https://example.com/a", "https://www.example.com/b", true},
		{"https://www.example.com/a", "https://example.com/b", true},
		{"https://example.com/a", "https://other.com/b", false},
		{"https://example.com/a", "http://example.com/b", false},
		{"https://example.com:443/a", "https://example.com:8080/b", false},
	}
	for _, tt := range tests {
		got := isPermittedRedirect(tt.original, tt.redirect)
		if got != tt.want {
			t.Errorf("isPermittedRedirect(%q, %q) = %v, want %v", tt.original, tt.redirect, got, tt.want)
		}
	}
}

func TestFetchToolContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":"value"}`))
	}))
	defer server.Close()

	tool := NewFetchTool()
	args, _ := json.Marshal(fetchToolArgs{URL: server.URL})
	result, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result.Text(), "Content-Type: application/json") {
		t.Fatalf("output = %q, want Content-Type header", result.Text())
	}
}

func TestFetchToolExtractHint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("Section A: irrelevant\n\nSection B: CVSS 9.8 critical\n\nSection C: also irrelevant"))
	}))
	defer server.Close()

	tool := NewFetchTool()
	args, _ := json.Marshal(fetchToolArgs{URL: server.URL, Extract: "CVSS"})
	result, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result.Text(), "CVSS 9.8") {
		t.Fatalf("output = %q, want CVSS section", result.Text())
	}
}
