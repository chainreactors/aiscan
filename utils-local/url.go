package utils

import (
	"net"
	"net/url"
	"strings"
)

func ParseHost(target string) string {
	target = strings.TrimSpace(target)
	if strings.Contains(target, "http") {
		u, err := url.Parse(target)
		if err != nil {
			return ""
		}
		return u.Hostname()
	} else {
		return strings.TrimSpace(strings.Trim(target, "/"))
	}
}

func NormalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return strings.ToLower(raw)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	if parsed.Path == "/" {
		parsed.Path = ""
	}
	return parsed.String()
}

func IsWebScheme(scheme string) bool {
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	return scheme == "http" || scheme == "https"
}

func IsWebPort(port string) bool {
	switch strings.TrimSpace(port) {
	case "80", "81", "443", "8000", "8008", "8080", "8081", "8443", "8888", "9000":
		return true
	default:
		return false
	}
}

func IsDomainHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	return host != "" && net.ParseIP(host) == nil && !strings.Contains(host, "/")
}

func SplitHostPort(raw string) (host, port string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	host, port, err := net.SplitHostPort(raw)
	if err == nil && host != "" && port != "" {
		return strings.Trim(host, "[]"), port, true
	}
	if strings.Count(raw, ":") == 1 {
		parts := strings.SplitN(raw, ":", 2)
		host = strings.TrimSpace(parts[0])
		port = strings.TrimSpace(parts[1])
		if host != "" && port != "" {
			return host, port, true
		}
	}
	return "", "", false
}

func URLFromHostPort(scheme, host, port string) string {
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	host = strings.Trim(strings.TrimSpace(host), "[]")
	port = strings.TrimSpace(port)
	if scheme == "" || host == "" || port == "" {
		return ""
	}
	return scheme + "://" + net.JoinHostPort(host, port)
}
