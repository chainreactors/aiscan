package utils

import (
	"fmt"
	"sort"
	"testing"
)

func TestParseIP(t *testing.T) {
	println(ParseIP("127.0.0.1").String())
	println(ParseIP("::127.0.0.1").String())
	println(ParseIP("2001:0:53ab:0:0:0:0:0").String())
	println(ParseIP("2001:0:c38c:ffff:ffff:0000:0000:ffff").String())
	println(ParseIP("2001:0:c38c:ffff:ffff::").String())
	println(ParseIP("327.0.0.1"))
	println(ParseIP("2001:0:c38c:ffff:ffff:ffff:ffff:ffff1"))
	println(ParseIP("baidu.com").String())
	println(ParseIP("http://baidu.com/").String())
}

func BenchmarkParseIP(t *testing.B) {
	for i := 0; i < t.N; i++ {
		ParseIP("192.168.1.1")
	}
}

func TestIP_Next(t *testing.T) {
	ip := ParseIP("192.168.1.1")
	for i := 0; i < 10; i++ {
		println(ip.Next().String())
	}
}

func BenchmarkIP_Next(b *testing.B) {
	ip := ParseIP("192.168.1.1")
	for i := 0; i < b.N; i++ {
		ip.Next()
	}
}

func BenchmarkInt2Ipv4(t *testing.B) {
	for i := 0; i < t.N; i++ {
		Int2Ipv4(123123123111)
	}
}

func TestIP_Compare(t *testing.T) {
	i1 := ParseIP("192.168.0.0")
	i2 := ParseIP("192.168.0.1")
	i3 := ParseIP("192.168.0.2")
	ips := IPs{i2, i1, i3}
	println(i2.Compare(i1))
	println(i2.Compare(i3))
	sort.Sort(ips)
	fmt.Println(ips)
}

func TestIP_Mask(t *testing.T) {
	i := ParseIP("192.168.1.111")
	m := i.Mask(24)
	println(i.String(), m.String())
}
