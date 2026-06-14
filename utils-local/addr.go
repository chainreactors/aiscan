package utils

import (
	"net"
)

func NewAddrWithPort(ip, port string) *Addr {
	return &Addr{ParseIP(ip), port}
}

func NewAddr(s string) *Addr {
	if ip, port, err := net.SplitHostPort(s); err == nil {
		return &Addr{ParseIP(ip), port}
	}
	return nil
}

type Addr struct {
	IP   *IP
	Port string
}

func (a Addr) String() string {
	return net.JoinHostPort(a.IP.String(), a.Port)
}

func NewAddrs(ss []string) Addrs {
	var addrs Addrs
	for _, s := range ss {
		if addr := NewAddr(s); addr != nil {
			addrs = append(addrs, addr)
		}
	}
	return addrs
}

func NewAddrsWithDefaultPort(ss []string, port string) Addrs {
	var addrs Addrs
	for _, s := range ss {
		if addr := NewAddr(s); addr != nil {
			addrs = append(addrs, addr)
		} else if ip := ParseIP(s); ip != nil {
			addrs = append(addrs, &Addr{ip, port})
		}
	}
	return addrs
}

type Addrs []*Addr

func NewAddrsWithPorts(ips []string, ports interface{}) *AddrsGenerator {
	switch ports.(type) {
	case string:
		return &AddrsGenerator{ParseIPs(ips), ParsePortsString(ports.(string))}
	default:
		return &AddrsGenerator{ParseIPs(ips), ports.([]string)}
	}
}

type AddrsGenerator struct {
	IPs   IPs
	Ports []string
}

func (as AddrsGenerator) Count() int {
	return len(as.IPs) * len(as.Ports)
}

func (as AddrsGenerator) GenerateWithIP() chan *Addr {
	gen := make(chan *Addr)
	go func() {
		for _, ip := range as.IPs {
			for _, port := range as.Ports {
				gen <- &Addr{ip, port}
			}
		}
		close(gen)
	}()
	return gen
}

func (as AddrsGenerator) GenerateWithPort() chan *Addr {
	gen := make(chan *Addr)
	go func() {
		for _, port := range as.Ports {
			for _, ip := range as.IPs {
				gen <- &Addr{ip, port}
			}
		}
		close(gen)
	}()
	return gen
}
