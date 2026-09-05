package util

import (
	"fmt"
	"net"
)

// IPToInt 将 IPv4 字符串转换为 uint32。
func IPToInt(ipStr string) (uint32, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return 0, fmt.Errorf("无效的 IPv4 地址")
	}
	ipv4 := ip.To4()
	if ipv4 == nil {
		return 0, fmt.Errorf("仅支持 IPv4 地址")
	}
	return uint32(ipv4[0])<<24 | uint32(ipv4[1])<<16 | uint32(ipv4[2])<<8 | uint32(ipv4[3]), nil
}

// IntToIP 将 uint32 转换为 IPv4 字符串。
func IntToIP(n uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		(n>>24)&0xFF,
		(n>>16)&0xFF,
		(n>>8)&0xFF,
		n&0xFF)
}

// ValidateIP 校验 IP 地址，返回类型（"IPv4"/"IPv6"）与是否合法。
func ValidateIP(ipStr string) (string, bool) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return "", false
	}
	if ip.To4() != nil {
		return "IPv4", true
	}
	return "IPv6", true
}

// LookupIP 解析主机名为 IP 地址列表。
func LookupIP(hostname string) ([]string, error) {
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(ips))
	for _, ip := range ips {
		result = append(result, ip.String())
	}
	return result, nil
}

// CalculateSubnet 计算子网信息（network/broadcast/netmask/wildcard/可用范围/主机数）。
func CalculateSubnet(ipStr, maskStr string) (map[string]string, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, fmt.Errorf("无效的 IP 地址")
	}
	_, network, err := net.ParseCIDR(maskStr)
	if err != nil {
		// Try as netmask
		maskIP := net.ParseIP(maskStr)
		if maskIP == nil {
			return nil, fmt.Errorf("无效的网络掩码或 CIDR")
		}
		mask := net.IPMask(maskIP.To4())
		network = &net.IPNet{IP: ip.Mask(mask), Mask: mask}
	}

	ipv4 := ip.To4()
	if ipv4 == nil {
		return nil, fmt.Errorf("仅支持 IPv4")
	}

	mask := network.Mask
	networkAddr := ipv4.Mask(mask)
	// 广播地址：网络地址的主机位全部置 1（对掩码取反后按位或）。
	broadcast := make(net.IP, len(networkAddr))
	for i := range networkAddr {
		broadcast[i] = networkAddr[i] | ^mask[i]
	}

	// Calculate total hosts: 循环累加得到的是"主机位全 1"的位模式（= 地址数 - 1），
	// 故末尾 +1 得到子网内真实地址总数（如 /24 为 256）。
	var totalHosts uint64
	for _, b := range mask {
		totalHosts = (totalHosts << 8) | uint64(^byte(b))
	}
	totalHosts++

	firstIP := make(net.IP, len(networkAddr))
	copy(firstIP, networkAddr)
	// First usable (network + 1)
	for i := len(firstIP) - 1; i >= 0; i-- {
		firstIP[i]++
		if firstIP[i] != 0 {
			break
		}
	}

	lastIP := make(net.IP, len(broadcast))
	copy(lastIP, broadcast)
	// Last usable (broadcast - 1)
	for i := len(lastIP) - 1; i >= 0; i-- {
		lastIP[i]--
		if lastIP[i] != 255 {
			break
		}
	}

	usableHosts := uint64(0)
	if totalHosts > 2 {
		usableHosts = totalHosts - 2
	}

	return map[string]string{
		"network":      networkAddr.String(),
		"broadcast":    broadcast.String(),
		"netmask":      net.IP(mask).String(),
		"wildcard":     net.IP(mask).String(),
		"first_usable": firstIP.String(),
		"last_usable":  lastIP.String(),
		"cidr":         fmt.Sprintf("%s/%d", networkAddr.String(), maskSize(mask)),
		"total_hosts":  fmt.Sprintf("%d", totalHosts),
		"usable_hosts": fmt.Sprintf("%d", usableHosts),
	}, nil
}

// IsPrivate 判断是否为私有/保留地址，返回 (是否私有, 命中的网段)。
func IsPrivate(ipStr string) (bool, string) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false, "无效 IP"
	}

	privateBlocks := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
	}

	for _, block := range privateBlocks {
		_, network, _ := net.ParseCIDR(block)
		if network.Contains(ip) {
			return true, block
		}
	}
	return false, ""
}

// maskSize 返回掩码前缀长度（bits），如 /24 返回 24。
func maskSize(mask net.IPMask) int {
	ones, _ := mask.Size()
	return ones
}
