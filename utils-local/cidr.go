package utils

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/chainreactors/utils/iputils"
)

func SplitCIDR(cidr string) (string, int) {
	tmp := strings.Split(cidr, "/")
	if len(tmp) == 2 {
		return tmp[0], toInt(tmp[1])
	} else {
		return tmp[0], 32
	}
}

func DifferenceCIDR(target, exclude *CIDR) CIDRs {
	var cidrs CIDRs
	cidrNets := iputils.DifferenceCIDR(target.Net(), exclude.Net())
	for _, c := range cidrNets {
		cidrs = append(cidrs, NewCIDRFromNet(c))
	}
	return cidrs
}

func NewCIDR(ip string, mask int) *CIDR {
	c := &CIDR{IP: ParseIP(ip), Mask: mask}
	if c.IP == nil {
		return nil
	}
	if c.Mask == 0 {
		if c.Ver == 4 {
			c.Mask = 32
		} else {
			c.Mask = 128
		}
	}
	c.maskIP = MaskToIP(c.Mask, c.Ver)
	c.Reset()
	return c
}

func ParseCIDRs(ips []string) CIDRs {
	var cs CIDRs
	for _, ip := range ips {
		c := ParseCIDR(ip)
		if c != nil {
			cs = append(cs, c)
		}
	}
	return cs
}

func NewCIDRFromNet(ip *net.IPNet) *CIDR {
	mask, err := IPMaskToPrefixLength(ip.Mask)
	if err != nil {
		return nil
	}

	i := NewIP(ip.IP)
	cidr := &CIDR{
		IP:     i,
		Mask:   mask,
		maskIP: MaskToIP(mask, i.Ver),
	}
	cidr.Reset()
	return cidr

}

func ParseCIDR(target string) *CIDR {
	// return ip, hosts
	var ip string
	var mask int
	target = ParseHost(target)
	if strings.Contains(target, "/") {
		ip, mask = SplitCIDR(target)
	} else {
		ip = target
	}

	return NewCIDR(ip, mask)
}

type CIDR struct {
	*IP
	Mask   int
	maskIP *IP
	curIP  *IP
	cur    int
	max    int
}

func (c *CIDR) Len() int {
	if c == nil || c.IP == nil {
		return 0
	}
	var totalBits uint
	if c.Ver == 4 {
		totalBits = 32
	} else if c.Ver == 6 {
		totalBits = 128
	} else {
		return 0 // 无效的 IP 版本
	}
	return 1 << (totalBits - uint(c.Mask))
}

func (c *CIDR) String() string {
	return fmt.Sprintf("%s/%d", c.IP.String(), c.Mask)
}

func (c *CIDR) IPString() string {
	return c.IP.String()
}

func (c *CIDR) FirstIP() *IP {
	return c.IP.MaskNet(c.maskIP)
}

func (c *CIDR) LastIP() *IP {
	ip := make(net.IP, c.IP.Len())
	for i := 0; i < c.IP.Len(); i++ {
		ip[i] = c.IP.IP[i] | ^c.maskIP.IP[i]
	}
	return &IP{IP: ip, Ver: c.Ver}
}

func (c *CIDR) Split(mask int) (CIDRs, error) {
	if c.Mask > mask {
		return nil, fmt.Errorf("mask error, %d > %d", mask, c.Mask)
	}
	block := 1 << uint(mask-c.Mask)
	var cs CIDRs
	for i := 0; i < block; i++ {
		if i == 0 {
			cs = append(cs, c.FirstIP().CIDR(mask))
		} else {
			cs = append(cs, cs[i-1].LastIP().Next().CIDR(mask))
		}
	}
	return cs, nil
}

func (c *CIDR) Net() *net.IPNet {
	return &net.IPNet{c.IP.IP, net.IPMask(MaskToIP(c.Mask, c.Ver).IP)}
}

func (c *CIDR) NetWithMask(mask int) *net.IPNet {
	return &net.IPNet{c.IP.IP, net.IPMask(MaskToIP(mask, c.Ver).IP)}
}

func (c *CIDR) IPMask() net.IPMask {
	if c.Ver == 4 {
		return net.CIDRMask(c.Mask, 32)
	} else {
		return net.CIDRMask(c.Mask, 128)
	}
}

func (c *CIDR) Count() int {
	if c.Ver == 4 {
		return 1 << uint(32-c.Mask)
	} else {
		return 1 << uint(128-c.Mask)
	}
}

func (c *CIDR) Compare(other *CIDR) int {
	if i := c.FirstIP().Compare(other.FirstIP()); i < 0 {
		return -1
	} else if i > 0 {
		return 1
	} else {
		if c.Mask < other.Mask {
			return -1
		} else {
			return 1
		}
	}
}

//func (c *CIDR) Range() (first, final uint) {
//	if c.Ver == 6 {
//		return 0, 0
//	}
//	first = c.FirstIP().Int()
//	final = first | uint(math.Pow(2, float64(32-c.Mask))-1)
//	return first, final
//}

func (c *CIDR) Range() chan *IP {
	ch := make(chan *IP)
	go func() {
		for i := 0; i < c.max; i++ {
			ch <- c.Next()
		}
		close(ch)
	}()

	return ch
}

func (c *CIDR) ContainsCIDR(cidr *CIDR) bool {
	return c.Net().Contains(cidr.IP.IP)
}

func (c *CIDR) ContainsString(s string) bool {
	cidr := ParseCIDR(s)
	return c.ContainsCIDR(cidr)
}

func (c *CIDR) ContainsIP(ip *IP) bool {
	return c.Net().Contains(ip.IP)
}

func (c *CIDR) Next() *IP {
	if c.cur == 0 {
		c.cur++
		return c.curIP.Copy()
	}

	if c.cur >= c.max {
		c.Reset()
		return c.Next()
	}
	c.cur++
	c.curIP.Next()
	return c.curIP.Copy()
}

func (c *CIDR) Reset() {
	c.max = c.Count()
	c.cur = 0
	c.curIP = c.FirstIP()
}

type CIDRs []*CIDR

func (cs CIDRs) Less(i, j int) bool {
	if cs[i].Compare(cs[j]) < 0 {
		return true
	} else {
		return false
	}
}

func (cs CIDRs) Swap(i, j int) {
	cs[i], cs[j] = cs[j], cs[i]
}

func (cs CIDRs) Len() int {
	return len(cs)
}

func (cs CIDRs) Strings() []string {
	s := make([]string, len(cs))
	for i, cidr := range cs {
		s[i] = cidr.String()
	}
	return s
}

func (cs CIDRs) ContainsCIDR(cidr *CIDR) bool {
	for _, c := range cs {
		if c.ContainsCIDR(cidr) {
			return true
		}
	}
	return false
}

func (cs CIDRs) ContainsCIDRs(cidrs CIDRs) bool {
	for _, c := range cidrs {
		if !cs.ContainsCIDR(c) {
			return false
		}
	}
	return true
}

func (cs CIDRs) ContainsIP(ip *IP) bool {
	for _, c := range cs {
		if c.ContainsIP(ip) {
			return true
		}
	}
	return false
}

func (cs *CIDRs) ContainsString(s string) bool {
	cidr := ParseCIDR(s)
	for _, c := range *cs {
		if c.ContainsCIDR(cidr) {
			return true
		}
	}
	return false
}

func (cs CIDRs) Coalesce() CIDRs {
	sort.Sort(cs)
	var newCIDRs CIDRs
	for i := 0; i < len(cs); i++ {
		j := i
		for j < len(cs)-1 {
			if !cs[j].ContainsCIDR(cs[j+1]) {
				break
			} else {
				j++
			}
		}
		cs[i].IP = cs[i].FirstIP()
		newCIDRs = append(newCIDRs, cs[i])
		i = j
	}
	return newCIDRs
}

func (cs CIDRs) Range() chan *IP {
	ch := make(chan *IP)
	go func() {
		for _, c := range cs {
			for ip := range c.Range() {
				ch <- ip
			}
		}
		close(ch)
	}()
	return ch
}

func (cs CIDRs) SprayRange() chan *IP {
	ch := make(chan *IP)
	length := cs.Len()
	count := cs.Count()
	go func() {
		var i, vaild int
		for {
			if vaild == count {
				break
			}
			if cs[i%length].cur < cs[i%length].max {
				ch <- cs[i%length].Next()
				vaild++
			}
			i++
		}
		close(ch)
	}()
	return ch
}

func (cs CIDRs) Count() int {
	var sum int
	for _, c := range cs {
		sum += c.Count()
	}
	return sum
}
