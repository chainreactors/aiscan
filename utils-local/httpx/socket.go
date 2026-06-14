package httpx

import (
	"net"
	"time"
)

// SocketConfig 描述 Socket 的构造参数。
type SocketConfig struct {
	Timeout time.Duration
	// DialTimeout 非 nil 时用其建连（用于代理）；nil 则 net.DialTimeout 直连。
	DialTimeout DialTimeoutFunc
}

// Socket 是对 net.Conn 的轻量封装，提供带超时的请求/读取语义。
// 合并自 gogo/pkg 与 zombie/pkg 中原本各自重复的实现。
type Socket struct {
	Conn    net.Conn
	Count   int
	Timeout time.Duration
}

// NewSocket 建立到 target 的连接并返回 Socket。
// cfg.DialTimeout 非 nil 时经其（可为代理）建连，否则直连。
func NewSocket(network, target string, cfg SocketConfig) (*Socket, error) {
	timeout := cfg.Timeout
	s := &Socket{Timeout: timeout}
	var conn net.Conn
	var err error
	if cfg.DialTimeout != nil {
		conn, err = cfg.DialTimeout(network, target, timeout)
	} else {
		conn, err = net.DialTimeout(network, target, timeout)
	}
	if err != nil {
		return nil, err
	}
	s.Conn = conn
	return s, nil
}

func (s *Socket) Read(timeout int) ([]byte, error) {
	buf := make([]byte, 16384)
	s.Conn.SetReadDeadline(time.Now().Add(time.Duration(timeout) * time.Second))
	n, err := s.Conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func (s *Socket) Request(data []byte, max int) ([]byte, error) {
	_ = s.Conn.SetDeadline(time.Now().Add(s.Timeout))
	var err error
	_, err = s.Conn.Write(data)
	if err != nil {
		return []byte{}, err
	}
	s.Count++
	buf := make([]byte, max)
	time.Sleep(time.Duration(500) * time.Millisecond)
	n, err := s.Conn.Read(buf)
	if err != nil {
		return []byte{}, err
	}
	return buf[:n], err
}

func (s *Socket) QuickRequest(data []byte, max int) ([]byte, error) {
	// read small data, without wait for 500ms
	_ = s.Conn.SetDeadline(time.Now().Add(s.Timeout))
	var err error
	_, err = s.Conn.Write(data)
	if err != nil {
		return []byte{}, err
	}
	s.Count++
	buf := make([]byte, max)
	n, err := s.Conn.Read(buf)
	if err != nil {
		return []byte{}, err
	}
	return buf[:n], err
}

func (s *Socket) Close() error {
	return s.Conn.Close()
}
