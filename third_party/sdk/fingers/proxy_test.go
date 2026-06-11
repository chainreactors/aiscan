package fingers

import (
	"io"
	"net"
	"sync/atomic"
	"testing"
	"net/http/httptest"
	"net/http"
)

// TestContextGetClientHonorsProxy 回归 buildDefaultClient 旧 TODO：
// WithProxy 后 GetClient() 返回的客户端（主动指纹探测所用）应经过代理。
func TestContextGetClientHonorsProxy(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer target.Close()

	var hits int32
	proxy := miniSocks5ForFingers(t, &hits)

	ctx := NewContext().WithProxy("socks5://" + proxy)
	client := ctx.GetClient()
	resp, err := client.Get(target.URL)
	if err != nil {
		t.Fatalf("get via proxied client: %v", err)
	}
	resp.Body.Close()
	if atomic.LoadInt32(&hits) == 0 {
		t.Fatal("GetClient() did not route through the proxy (buildDefaultClient TODO regression)")
	}
}

func miniSocks5ForFingers(t *testing.T, hits *int32) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 262)
				if _, err := io.ReadFull(c, buf[:2]); err != nil {
					return
				}
				n := int(buf[1])
				if _, err := io.ReadFull(c, buf[:n]); err != nil {
					return
				}
				c.Write([]byte{0x05, 0x00})
				if _, err := io.ReadFull(c, buf[:4]); err != nil {
					return
				}
				var host string
				switch buf[3] {
				case 0x01:
					io.ReadFull(c, buf[:4])
					host = net.IP(append([]byte(nil), buf[:4]...)).String()
				case 0x03:
					io.ReadFull(c, buf[:1])
					l := int(buf[0])
					io.ReadFull(c, buf[:l])
					host = string(buf[:l])
				case 0x04:
					io.ReadFull(c, buf[:16])
					host = net.IP(append([]byte(nil), buf[:16]...)).String()
				default:
					c.Write([]byte{0x05, 0x08, 0, 1, 0, 0, 0, 0, 0, 0})
					return
				}
				io.ReadFull(c, buf[:2])
				port := int(buf[0])<<8 | int(buf[1])
				remote, err := net.Dial("tcp", net.JoinHostPort(host, itoaFingers(port)))
				if err != nil {
					c.Write([]byte{0x05, 0x01, 0, 1, 0, 0, 0, 0, 0, 0})
					return
				}
				defer remote.Close()
				atomic.AddInt32(hits, 1)
				c.Write([]byte{0x05, 0x00, 0, 1, 0, 0, 0, 0, 0, 0})
				done := make(chan struct{}, 2)
				go func() { io.Copy(remote, c); done <- struct{}{} }()
				go func() { io.Copy(c, remote); done <- struct{}{} }()
				<-done
			}(c)
		}
	}()
	return ln.Addr().String()
}

func itoaFingers(n int) string {
	if n == 0 {
		return "0"
	}
	var b [6]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
