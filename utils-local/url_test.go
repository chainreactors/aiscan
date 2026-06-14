package utils

import "testing"

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: " ", want: ""},
		{name: "canonical web", raw: "HTTP://Example.COM/", want: "http://example.com"},
		{name: "path preserved", raw: "HTTPS://Example.COM/Admin", want: "https://example.com/Admin"},
		{name: "fallback", raw: " Example.COM ", want: "example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeURL(tt.raw); got != tt.want {
				t.Fatalf("NormalizeURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestSplitHostPort(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantHost string
		wantPort string
		wantOK   bool
	}{
		{name: "bare", raw: "127.0.0.1:8080", wantHost: "127.0.0.1", wantPort: "8080", wantOK: true},
		{name: "ipv6", raw: "[::1]:443", wantHost: "::1", wantPort: "443", wantOK: true},
		{name: "domain", raw: "example.com:80", wantHost: "example.com", wantPort: "80", wantOK: true},
		{name: "missing port", raw: "example.com", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port, ok := SplitHostPort(tt.raw)
			if host != tt.wantHost || port != tt.wantPort || ok != tt.wantOK {
				t.Fatalf("SplitHostPort(%q) = %q, %q, %v; want %q, %q, %v", tt.raw, host, port, ok, tt.wantHost, tt.wantPort, tt.wantOK)
			}
		})
	}
}

func TestHostAndWebHelpers(t *testing.T) {
	if !IsWebScheme("HTTPS") || IsWebScheme("ssh") {
		t.Fatal("IsWebScheme returned unexpected result")
	}
	if !IsWebPort("8080") || IsWebPort("22") {
		t.Fatal("IsWebPort returned unexpected result")
	}
	if !IsDomainHost("example.com") || IsDomainHost("127.0.0.1") || IsDomainHost("192.168.1.0/24") {
		t.Fatal("IsDomainHost returned unexpected result")
	}
	if got := URLFromHostPort("https", "127.0.0.1", "443"); got != "https://127.0.0.1:443" {
		t.Fatalf("URLFromHostPort() = %q", got)
	}
	if got := URLFromHostPort("http", "::1", "8080"); got != "http://[::1]:8080" {
		t.Fatalf("URLFromHostPort() = %q", got)
	}
}
