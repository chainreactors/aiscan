package neutron

import (
	"testing"
	"time"
)

// TestCompileOptionsInjectsProxyDialer 验证：Config.Proxy 非空时，compileOptions
// 会把代理拨号器注入 Options.DialContext（编译期粒度），且互不共享。
func TestCompileOptionsInjectsProxyDialer(t *testing.T) {
	withProxy := &Engine{config: &Config{Timeout: 5 * time.Second, Proxy: []string{"socks5://127.0.0.1:1080"}}}
	opts := withProxy.compileOptions()
	if opts.Options.DialContext == nil {
		t.Fatal("expected Options.DialContext to be set when Config.Proxy is non-empty")
	}

	noProxy := &Engine{config: &Config{Timeout: 5 * time.Second}}
	if noProxy.compileOptions().Options.DialContext != nil {
		t.Fatal("expected Options.DialContext to be nil when Config.Proxy is empty")
	}

	// 两个不同代理的引擎，各自的 ExecuterOptions 相互独立（无共享全局）。
	other := &Engine{config: &Config{Timeout: 5 * time.Second, Proxy: []string{"socks5://127.0.0.1:1081"}}}
	if &opts.Options == &other.compileOptions().Options {
		t.Fatal("expected per-engine ExecuterOptions, not shared")
	}
}
