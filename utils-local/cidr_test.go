package utils

import (
	"fmt"
	"sort"
	"testing"
)

func TestCIDR_Next(t *testing.T) {
	c := NewCIDR("2001:0:53ab:0:0:0:0:0", 120)
	for i := 0; i < c.max; i++ {
		println(c.Next().String())
	}
}

func BenchmarkCIDR_Next100000(b *testing.B) {
	for i := 0; i < b.N; i++ {
		NewCIDR("2001:0:53ab:0:0:0:0:0", 120).Next()
	}
}

func TestParseCIDR(t *testing.T) {
	println(ParseCIDR("127.0.0.1").String())
	println(ParseCIDR("::127.0.0.1").String())
	println(ParseCIDR("2001:0:53ab:0:0:0:0:0/120").String())
	println(ParseCIDR("2001:0:c38c:ffff:ffff:0000:0000:ffff").String())
	println(ParseCIDR("2001:0:c38c:ffff:ffff::").String())
	println(ParseCIDR("327.0.0.1"))
	println(ParseCIDR("2001:0:c38c:ffff:ffff:ffff:ffff:ffff1"))
}

func TestCIDRs_Less(t *testing.T) {
	var cs CIDRs
	cs = append(cs, ParseCIDR("192.168.1.1/24"))
	cs = append(cs, ParseCIDR("192.168.1.55/16"))
	cs = append(cs, ParseCIDR("192.168.0.1/24"))
	cs = append(cs, ParseCIDR("192.10.1.1/24"))
	cs = append(cs, ParseCIDR("192.168.19.1/24"))
	cs = append(cs, ParseCIDR("2001:0:53ab:0:0:0:0:0/120"))
	sort.Sort(cs)
	for _, c := range cs.Strings() {
		println(c)
	}
}

func TestCIDR_Split(t *testing.T) {
	c := ParseCIDR("192.168.1.1/20")
	cs, _ := c.Split(24)
	fmt.Println(cs.Strings())
}

func TestCIDRs_SprayRange(t *testing.T) {
	var cs CIDRs
	cs = append(cs, ParseCIDR("192.168.1.1/24"))
	cs = append(cs, ParseCIDR("192.168.2.55/25"))
	for i := range cs.SprayRange() {
		println(i.String())
	}
}

func TestCIDR_Range(t *testing.T) {
	c := ParseCIDR("fe80::4e1d:bc74:cbd1:bd62")
	for i := range c.Range() {
		println(i.String())
	}
}

func TestCIDRDifference(t *testing.T) {
	cidr1 := ParseCIDR("192.168.0.0/24")
	cidr2 := ParseCIDR("192.168.0.0/28")
	for _, c := range DifferenceCIDR(cidr1, cidr2).Coalesce() {
		println(c.String())
	}
}

func TestCIDRs_Coalesce(t *testing.T) {
	cidrs := CIDRs{
		ParseCIDR("192.168.1.1/24"),
		ParseCIDR("192.168.1.1/25"),
		ParseCIDR("192.168.1.1/26"),
		ParseCIDR("192.168.1.1/27"),
		ParseCIDR("192.168.11.1/28"),
		ParseCIDR("192.168.88.1/28"),
	}
	for _, i := range cidrs.Coalesce() {
		println(i.String())
	}
}
