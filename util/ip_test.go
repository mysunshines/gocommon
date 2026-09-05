package util

import (
	"fmt"
	"testing"
)

func TestIPToInt(t *testing.T) {
	cases := []struct {
		in   string
		want uint32
		err  bool
	}{
		{"192.168.1.1", 3232235777, false},
		{"0.0.0.0", 0, false},
		{"255.255.255.255", 4294967295, false},
		{"::1", 0, true},    // 仅支持 IPv4
		{"not-ip", 0, true}, // 非法
	}
	for _, c := range cases {
		got, err := IPToInt(c.in)
		if c.err {
			if err == nil {
				t.Errorf("IPToInt(%q): 期望错误", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("IPToInt(%q): 意外错误 %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("IPToInt(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestIntToIP(t *testing.T) {
	cases := []struct {
		in   uint32
		want string
	}{
		{3232235777, "192.168.1.1"},
		{0, "0.0.0.0"},
		{4294967295, "255.255.255.255"},
	}
	for _, c := range cases {
		if got := IntToIP(c.in); got != c.want {
			t.Errorf("IntToIP(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidateIP(t *testing.T) {
	if v, ok := ValidateIP("192.168.1.1"); !ok || v != "IPv4" {
		t.Errorf("ValidateIP(IPv4) = %q,%v", v, ok)
	}
	if v, ok := ValidateIP("::1"); !ok || v != "IPv6" {
		t.Errorf("ValidateIP(IPv6) = %q,%v", v, ok)
	}
	if _, ok := ValidateIP("garbage"); ok {
		t.Error("ValidateIP(garbage) 应返回 false")
	}
}

func TestCalculateSubnet(t *testing.T) {
	m, err := CalculateSubnet("192.168.1.10", "255.255.255.0")
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	want := map[string]string{
		"network":      "192.168.1.0",
		"broadcast":    "192.168.1.255",
		"netmask":      "255.255.255.0",
		"first_usable": "192.168.1.1",
		"last_usable":  "192.168.1.254",
		"cidr":         "192.168.1.0/24",
		"total_hosts":  "256",
		"usable_hosts": "254",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("CalculateSubnet[%q] = %q, want %q", k, m[k], v)
		}
	}

	// CIDR 形式也应可用
	if _, err := CalculateSubnet("10.0.0.5", "10.0.0.0/8"); err != nil {
		t.Errorf("CIDR 形式应可用: %v", err)
	}
	// IPv6 应报错
	if _, err := CalculateSubnet("::1", "255.255.255.0"); err == nil {
		t.Error("IPv6 应返回错误")
	}
}

func TestIsPrivate(t *testing.T) {
	if ok, block := IsPrivate("192.168.1.1"); !ok || block != "192.168.0.0/16" {
		t.Errorf("IsPrivate(192.168.1.1) = %v,%q", ok, block)
	}
	if ok, _ := IsPrivate("8.8.8.8"); ok {
		t.Error("8.8.8.8 不是私有地址")
	}
	if ok, block := IsPrivate("not-ip"); ok || block != "无效 IP" {
		t.Errorf("IsPrivate(not-ip) = %v,%q", ok, block)
	}
}

func ExampleIPToInt() {
	v, err := IPToInt("192.168.1.1")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(v)
	// Output: 3232235777
}

func ExampleIntToIP() {
	fmt.Println(IntToIP(3232235777))
	fmt.Println(IntToIP(0))
	fmt.Println(IntToIP(4294967295))
	// Output:
	// 192.168.1.1
	// 0.0.0.0
	// 255.255.255.255
}

func ExampleValidateIP() {
	fmt.Println(ValidateIP("192.168.1.1"))
	fmt.Println(ValidateIP("::1"))
	fmt.Println(ValidateIP("not-ip"))
	// Output:
	// IPv4 true
	// IPv6 true
	//  false
}

func ExampleLookupIP() {
	// 实际解析需要网络，这里仅演示用法（不产生 Output 断言）
	ips, err := LookupIP("localhost")
	if err == nil {
		_ = ips
	}
}

func ExampleCalculateSubnet() {
	m, _ := CalculateSubnet("192.168.1.10", "255.255.255.0")
	for _, k := range []string{"network", "broadcast", "netmask", "first_usable", "last_usable", "cidr", "total_hosts", "usable_hosts"} {
		fmt.Printf("%s=%s\n", k, m[k])
	}
	// Output:
	// network=192.168.1.0
	// broadcast=192.168.1.255
	// netmask=255.255.255.0
	// first_usable=192.168.1.1
	// last_usable=192.168.1.254
	// cidr=192.168.1.0/24
	// total_hosts=256
	// usable_hosts=254
}

func ExampleIsPrivate() {
	b, block := IsPrivate("192.168.1.1")
	fmt.Printf("%v %q\n", b, block)
	b, block = IsPrivate("8.8.8.8")
	fmt.Printf("%v %q\n", b, block)
	b, block = IsPrivate("not-ip")
	fmt.Printf("%v %q\n", b, block)
	// Output:
	// true "192.168.0.0/16"
	// false ""
	// false "无效 IP"
}
