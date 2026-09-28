package curl

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// ConnectToEntry changes only the connection destination. Empty source fields
// match any host/port; empty destination fields keep the original value.
type ConnectToEntry struct {
	Host, Port, ConnectHost, ConnectPort string
}

func parseConnectTo(raw string) (ConnectToEntry, error) {
	var fields []string
	start, bracket := 0, false
	invalid := func() (ConnectToEntry, error) {
		return ConnectToEntry{}, fmt.Errorf("curl: --connect-to expects HOST1:PORT1:HOST2:PORT2 with valid hosts and ports")
	}
	for i, ch := range raw {
		switch ch {
		case '[':
			if bracket || i != start {
				return invalid()
			}
			bracket = true
		case ']':
			if !bracket || (i+1 < len(raw) && raw[i+1] != ':') {
				return invalid()
			}
			bracket = false
		case ':':
			if !bracket {
				fields = append(fields, raw[start:i])
				start = i + 1
			}
		}
	}
	fields = append(fields, raw[start:])
	if bracket || len(fields) != 4 {
		return invalid()
	}
	for _, i := range []int{0, 2} {
		host := fields[i]
		if strings.HasPrefix(host, "[") {
			host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
			if net.ParseIP(host) == nil || !strings.Contains(host, ":") {
				return invalid()
			}
		} else if strings.ContainsAny(host, "[]:/?#@\\") {
			return invalid()
		}
		if strings.ContainsFunc(host, func(r rune) bool { return r <= ' ' || r == 127 }) {
			return invalid()
		}
		fields[i] = host
	}
	for _, i := range []int{1, 3} {
		if fields[i] == "" {
			continue
		}
		if strings.ContainsFunc(fields[i], func(r rune) bool { return r < '0' || r > '9' }) {
			return invalid()
		}
		port, err := strconv.Atoi(fields[i])
		if err != nil || port < 1 || port > 65535 {
			return invalid()
		}
		fields[i] = strconv.Itoa(port)
	}
	return ConnectToEntry{fields[0], fields[1], fields[2], fields[3]}, nil
}

type contextDialer func(context.Context, string, string) (net.Conn, error)

// mappedDialer keeps URL, Host and TLS verification untouched. With a proxy,
// CONNECT (or SOCKS) carries the mapped destination through that proxy. The
// proxy endpoint itself is never subject to destination mappings, and failure
// never falls back to a direct connection. Redirects use the same local rules.
func mappedDialer(req *Request, proxyURL *url.URL, tlsConfig *tls.Config, timeout time.Duration) (contextDialer, error) {
	dialer := &net.Dialer{Timeout: timeout}
	dial := contextDialer(dialer.DialContext)
	if proxyURL != nil {
		switch proxyURL.Scheme {
		case "http", "https":
			dial = func(ctx context.Context, network, address string) (net.Conn, error) {
				return dialProxyTunnel(ctx, dialer, proxyURL, tlsConfig, network, address)
			}
		case "socks5", "socks5h":
			socks, err := proxy.FromURL(proxyURL, dialer)
			if err != nil {
				return nil, fmt.Errorf("curl: cannot configure SOCKS proxy")
			}
			contextual, ok := socks.(proxy.ContextDialer)
			if !ok {
				return nil, fmt.Errorf("curl: proxy cannot dial with a context")
			}
			dial = contextual.DialContext
		default:
			return nil, fmt.Errorf("curl: connection mappings require an HTTP, HTTPS or SOCKS5 proxy")
		}
	}
	resolve := makeResolveMap(req.Resolve)
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		for _, entry := range req.ConnectTo {
			if (entry.Host == "" || strings.EqualFold(entry.Host, host)) && (entry.Port == "" || entry.Port == port) {
				if entry.ConnectHost == "" && entry.ConnectPort == "" {
					continue
				}
				if entry.ConnectHost != "" {
					host = entry.ConnectHost
				}
				if entry.ConnectPort != "" {
					port = entry.ConnectPort
				}
				break // curl uses the first matching connection override.
			}
		}
		addresses := []string{host}
		if entry, ok := lookupResolve(resolve, host, port); ok {
			addresses = entry.Addresses
		}
		var lastErr error
		for _, mapped := range addresses {
			conn, err := dial(ctx, network, net.JoinHostPort(mapped, port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		return nil, lastErr
	}, nil
}

type tunnelConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *tunnelConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

func dialProxyTunnel(ctx context.Context, dialer *net.Dialer, proxyURL *url.URL, tlsConfig *tls.Config, network, address string) (tunnel net.Conn, err error) {
	port := proxyURL.Port()
	if port == "" {
		port = "80"
		if proxyURL.Scheme == "https" {
			port = "443"
		}
	}
	conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(proxyURL.Hostname(), port))
	if err != nil {
		return nil, err
	}
	raw := conn
	canceled := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = raw.Close(); close(canceled) })
	defer func() {
		if !stop() {
			<-canceled
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil {
			_ = raw.Close()
			tunnel = nil
		}
	}()
	if proxyURL.Scheme == "https" {
		config := tlsConfig.Clone()
		config.ServerName = proxyURL.Hostname()
		config.NextProtos = []string{"http/1.1"}
		secured := tls.Client(conn, config)
		if err := secured.HandshakeContext(ctx); err != nil {
			return nil, err
		}
		conn = secured
	}
	request := &http.Request{
		Method: http.MethodConnect, URL: &url.URL{Opaque: address}, Host: address, Header: make(http.Header),
	}
	if proxyURL.User != nil {
		password, _ := proxyURL.User.Password()
		credential := proxyURL.User.Username() + ":" + password
		request.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(credential)))
	}
	if err := request.Write(conn); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	// CONNECT transfers the socket to the tunnel. Draining this body could
	// consume tunnel data; failed handshakes close raw in the deferred cleanup.
	response, err := http.ReadResponse(reader, request) //nolint:bodyclose
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		// Do not echo proxy-supplied status text, body or credentials.
		return nil, fmt.Errorf("curl: proxy CONNECT failed (HTTP %d)", response.StatusCode)
	}
	return &tunnelConn{Conn: conn, reader: reader}, nil
}
