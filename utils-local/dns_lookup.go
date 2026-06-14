//go:build !tinygo
// +build !tinygo

package utils

import "net"

func resolveHostIP(host string) ([]net.IP, error) {
	return net.LookupIP(host)
}
