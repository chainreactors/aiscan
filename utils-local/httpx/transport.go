//go:build !tinygo
// +build !tinygo

package httpx

import (
	"crypto/tls"
	"net/http"
)

func newTransport(cfg ClientConfig, tlsConfig *tls.Config) *http.Transport {
	tr := &http.Transport{
		TLSClientConfig:     tlsConfig,
		MaxIdleConns:        cfg.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:     cfg.IdleConnTimeout,
		DisableKeepAlives:   cfg.DisableKeepAlives,
	}
	if cfg.DialContext != nil {
		tr.DialContext = cfg.DialContext
	}
	return tr
}

func errUseLastResponse() error {
	return http.ErrUseLastResponse
}
