//go:build tinygo
// +build tinygo

package utils

import (
	"fmt"
	"net"
)

func resolveHostIP(host string) ([]net.IP, error) {
	ip := net.ParseIP(host)
	if ip == nil {
		return nil, fmt.Errorf("dns lookup is unavailable in tinygo")
	}
	return []net.IP{ip}, nil
}
