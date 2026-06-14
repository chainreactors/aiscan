package utils

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
)

var (
	IPV4 = 4
	IPV6 = 6
)

func IsIp(ip string) bool {
	if net.ParseIP(ip) != nil {
		return true
	}
	return false
}

func MaskToIPv4(mask int) *IP {
	subnetMask := make([]byte, net.IPv4len) // 创建长度为4的字节数组
	for i := 0; i < mask; i++ {
		subnetMask[i/8] |= 1 << uint(7-i%8) // 根据子网掩码长度设置相应位为1
	}
	return &IP{IP: subnetMask, Ver: 4}
}

func MaskToIPv6(mask int) *IP {
	subnetMask := make([]byte, net.IPv6len) // 创建长度为4的字节数组
	for i := 0; i < mask; i++ {
		subnetMask[i/8] |= 1 << uint(7-i%8) // 根据子网掩码长度设置相应位为1
	}
	return &IP{IP: subnetMask, Ver: 6}
}

func MaskToIP(mask, ver int) *IP {
	if ver == 4 {
		return MaskToIPv4(mask)
	} else if ver == 6 {
		return MaskToIPv6(mask)
	}
	return nil
}

func IPMaskToPrefixLength(ipMask net.IPMask) (int, error) {
	mask := ipMask
	if len(mask) != net.IPv4len && len(mask) != net.IPv6len {
		return 0, fmt.Errorf("invalid IP mask length")
	}

	prefixLength := 0
	for _, octet := range mask {
		for i := 7; i >= 0; i-- {
			if (octet>>uint(i))&1 == 0 {
				return prefixLength, nil
			}
			prefixLength++
		}
	}

	return prefixLength, nil
}

func Ip2Intv4(ip string) uint {
	s2ip := net.ParseIP(ip).To4()
	return uint(binary.BigEndian.Uint32(s2ip))
}

func Int2Ipv4(ipint uint) string {
	ip := net.IP{byte(ipint >> 24), byte(ipint >> 16), byte(ipint >> 8), byte(ipint)}
	return ip.String()
}

// Is p all zeros?
func isZeros(p net.IP) bool {
	for i := 0; i < len(p); i++ {
		if p[i] != 0 {
			return false
		}
	}
	return true
}

func DistinguishIPVersion(ip net.IP) int {
	switch len(ip) {
	case net.IPv4len:
		return 4
	case net.IPv6len:
		if isZeros(ip[0:10]) && ip[10] == 0xff && ip[11] == 0xff {
			return 4
		} else {
			return 6
		}
	}
	return 0
}

func ParseIP(s string) *IP {
	ip := net.ParseIP(s)
	if ip == nil {
		i, err := ParseHostToIP(s)
		if err != nil {
			return nil
		} else {
			return i
		}
	}

	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '.':
			return &IP{IP: ip.To4(), Ver: IPV4}
		case ':':
			return &IP{IP: ip.To16(), Ver: IPV6}
		}
	}
	return nil
}

//func NewIP(ipint uint) *IP {
//	return &IP{IP: net.IP{byte(ipint >> 24), byte(ipint >> 16), byte(ipint >> 8), byte(ipint)}, Ver: 4}
//}
func NewIP(ip net.IP) *IP {
	if len(ip) == net.IPv4len {
		return &IP{IP: ip, Ver: IPV4}
	} else if ip[0] == 0 {
		return &IP{IP: ip.To4(), Ver: IPV4}
	} else {
		return &IP{IP: ip, Ver: IPV6}
	}
}

// ParseHostToIP parse host to ip and validate ip format
func ParseHostToIP(target string) (*IP, error) {
	iprecords, err := resolveHostIP(ParseHost(target))
	if err != nil {
		return nil, fmt.Errorf("Unable to resolve domain name:" + target + ". SKIPPED!")
	}

	for _, ip := range iprecords {
		if ip != nil {
			//Log.Important("parse domain SUCCESS, map " + target + " to " + ip.String())
			switch DistinguishIPVersion(ip) {
			case 4:
				return &IP{ip.To4(), IPV4, target}, nil
			case 6:
				return &IP{ip.To16(), IPV6, target}, nil
			}
		}
	}
	return nil, fmt.Errorf("not found Ip address")
}

type IP struct {
	IP   net.IP
	Ver  int
	Host string
}

func (ip *IP) Len() int {
	if ip.Ver == IPV4 {
		return net.IPv4len
	} else if ip.Ver == IPV6 {
		return net.IPv6len
	} else {
		return 0
	}
}

func (ip *IP) Int() uint {
	if ip.Ver == IPV4 {
		return uint(binary.BigEndian.Uint32(ip.IP.To4()))
	}
	return 0
}

func (ip *IP) String() string {
	return ip.IP.String()
}

func (ip *IP) Mask(mask int) *IP {
	maskip := MaskToIP(mask, ip.Ver)
	return ip.MaskNet(maskip)
}

func (ip *IP) MaskNet(mask *IP) *IP {
	newip := make(net.IP, ip.Len())
	for i := 0; i < ip.Len(); i++ {
		newip[i] = ip.IP[i] & mask.IP[i]
	}
	return &IP{IP: newip, Ver: ip.Ver}
}

func (ip *IP) CIDR(mask int) *CIDR {
	c := &CIDR{
		IP:     ip.Copy(),
		Mask:   mask,
		maskIP: MaskToIP(mask, ip.Ver),
	}
	c.Reset()
	return c
}

func (ip *IP) Mask24() *IP {
	i := ip.Copy()
	i.IP[3] = 0
	return i
}

func (ip *IP) Mask16() *IP {
	i := ip.Copy()
	i.IP[2] = 0
	i.IP[3] = 0
	return i
}

func (ip *IP) Equal(other *IP) bool {
	return bytes.Equal(ip.IP, other.IP)
}

func (ip *IP) Compare(other *IP) int {
	if ip.Ver != other.Ver {
		return 1
	}
	return bytes.Compare(ip.IP, other.IP)
}

func (ip *IP) Copy() *IP {
	newip := make(net.IP, ip.Len())
	copy(newip, ip.IP)
	return &IP{IP: newip, Ver: ip.Ver, Host: ip.Host}
}

func (ip *IP) Next() *IP {
	ip.IP[ip.Len()-1]++
	for i := ip.Len() - 1; i > 0; i-- {
		if ip.IP[i] == 0 {
			ip.IP[i-1]++
			if ip.IP[i-1] != 0 {
				break
			} else {
				continue
			}
		} else {
			break
		}
	}
	return ip
}

// ParseIPs parse string to ip , auto skip wrong ip
func ParseIPs(input []string) IPs {
	var ips IPs
	for _, ip := range input {
		i := ParseIP(ip)
		if i == nil {
			continue
		}
		ips = append(ips, i)
	}
	return ips
}

type IPs []*IP

func (is IPs) CIDRs() CIDRs {
	cs := make(CIDRs, len(is))
	for i, c := range is {
		cs[i] = c.CIDR(32)
	}
	return cs
}

func (is IPs) Less(i, j int) bool {
	if is[i].Compare(is[j]) < 0 {
		return true
	} else {
		return false
	}
}

func (is IPs) Swap(i, j int) {
	is[i], is[j] = is[j], is[i]
}

func (is IPs) Len() int {
	return len(is)
}

func (is IPs) Strings() []string {
	s := make([]string, len(is))
	for i, cidr := range is {
		s[i] = cidr.String()
	}
	return s
}

func (is IPs) Approx() CIDRs {
	cidrMap := make(map[string]*CIDR)

	for _, ip := range is {
		if n, ok := cidrMap[ip.Mask(24).String()]; ok {
			var baseNet byte
			var nowN, newN byte
			for i := 8; i > 0; i-- {
				nowN = n.IP.IP[3] & (1 << uint(i-1)) >> uint(i-1)
				newN = ip.IP[3] & (1 << uint(i-1)) >> uint(i-1)
				if nowN&newN == 1 {
					baseNet += 1 << uint(i-1)
				}
				if nowN^newN == 1 {
					n.Mask = 32 - i
					n.IP.IP[3] = baseNet
					break
				}
			}
		} else {
			cidrMap[ip.Mask(24).String()] = NewCIDR(ip.String(), 32)
		}
	}

	approxed := make(CIDRs, len(cidrMap))
	var index int
	for _, cidr := range cidrMap {
		approxed[index] = cidr
		index++
	}

	return approxed
}
