//go:build tinygo
// +build tinygo

package httpx

import (
	"crypto/tls"
	"errors"
	"net/http"
)

var errTinyGoUseLastResponse = errors.New("use last response")

func newTransport(cfg ClientConfig, tlsConfig *tls.Config) *http.Transport {
	return &http.Transport{}
}

func errUseLastResponse() error {
	return errTinyGoUseLastResponse
}
