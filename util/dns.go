package util

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
)

// DNSRecord 表示一条 DNS 记录。
type DNSRecord struct {
	Type  string   `json:"type"`
	Name  string   `json:"name"`
	Value []string `json:"value"`
	TTL   uint32   `json:"ttl,omitempty"`
}

// DNSResult 域名 DNS 查询结果。
type DNSResult struct {
	Domain  string      `json:"domain"`
	Records []DNSRecord `json:"records"`
	Errors  []string    `json:"errors,omitempty"`
}

// resolverOrDefault 返回指定的解析器，为 nil 时使用系统默认解析器。
func resolverOrDefault(r *net.Resolver) *net.Resolver {
	if r != nil {
		return r
	}
	return net.DefaultResolver
}

// LookupDNS 执行常见 DNS 查询（A/AAAA/CNAME/MX/NS/TXT），可传入自定义解析器。
func LookupDNS(domain string, r *net.Resolver) *DNSResult {
	domain = strings.TrimSuffix(domain, ".")
	resolver := resolverOrDefault(r)
	result := &DNSResult{
		Domain:  domain,
		Records: []DNSRecord{},
		Errors:  []string{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// A / AAAA records
	if ips, err := resolver.LookupHost(ctx, domain); err == nil {
		var aRecords []string
		var aaaaRecords []string
		for _, ip := range ips {
			if strings.Contains(ip, ":") {
				aaaaRecords = append(aaaaRecords, ip)
			} else {
				aRecords = append(aRecords, ip)
			}
		}
		if len(aRecords) > 0 {
			sort.Strings(aRecords)
			result.Records = append(result.Records, DNSRecord{Type: "A", Name: domain, Value: aRecords})
		}
		if len(aaaaRecords) > 0 {
			sort.Strings(aaaaRecords)
			result.Records = append(result.Records, DNSRecord{Type: "AAAA", Name: domain, Value: aaaaRecords})
		}
	} else {
		result.Errors = append(result.Errors, fmt.Sprintf("A/AAAA: %v", err))
	}

	// CNAME
	if cname, err := resolver.LookupCNAME(ctx, domain); err == nil && cname != domain+"." {
		result.Records = append(result.Records, DNSRecord{Type: "CNAME", Name: domain, Value: []string{strings.TrimSuffix(cname, ".")}})
	}

	// MX
	if mxs, err := resolver.LookupMX(ctx, domain); err == nil {
		values := make([]string, 0, len(mxs))
		for _, mx := range mxs {
			values = append(values, fmt.Sprintf("%d %s", mx.Pref, strings.TrimSuffix(mx.Host, ".")))
		}
		sort.Strings(values)
		result.Records = append(result.Records, DNSRecord{Type: "MX", Name: domain, Value: values})
	} else {
		result.Errors = append(result.Errors, fmt.Sprintf("MX: %v", err))
	}

	// NS
	if ns, err := resolver.LookupNS(ctx, domain); err == nil {
		values := make([]string, 0, len(ns))
		for _, n := range ns {
			values = append(values, strings.TrimSuffix(n.Host, "."))
		}
		sort.Strings(values)
		result.Records = append(result.Records, DNSRecord{Type: "NS", Name: domain, Value: values})
	} else {
		result.Errors = append(result.Errors, fmt.Sprintf("NS: %v", err))
	}

	// TXT
	if txts, err := resolver.LookupTXT(ctx, domain); err == nil {
		result.Records = append(result.Records, DNSRecord{Type: "TXT", Name: domain, Value: txts})
	} else {
		result.Errors = append(result.Errors, fmt.Sprintf("TXT: %v", err))
	}

	// SOA: Go 标准库不支持 SOA 查询，跳过
	result.Errors = append(result.Errors, "SOA: Go 标准库不支持 SOA 记录查询（需使用第三方库）")

	return result
}

// LookupDNSByType 查询指定类型的 DNS 记录，可传入自定义解析器。
func LookupDNSByType(domain, recordType string, r *net.Resolver) *DNSResult {
	domain = strings.TrimSuffix(domain, ".")
	resolver := resolverOrDefault(r)
	result := &DNSResult{
		Domain:  domain,
		Records: []DNSRecord{},
		Errors:  []string{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch strings.ToUpper(recordType) {
	case "A":
		ips, err := resolver.LookupHost(ctx, domain)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			return result
		}
		values := make([]string, 0)
		for _, ip := range ips {
			if !strings.Contains(ip, ":") {
				values = append(values, ip)
			}
		}
		sort.Strings(values)
		result.Records = append(result.Records, DNSRecord{Type: "A", Name: domain, Value: values})

	case "AAAA":
		ips, err := resolver.LookupHost(ctx, domain)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			return result
		}
		values := make([]string, 0)
		for _, ip := range ips {
			if strings.Contains(ip, ":") {
				values = append(values, ip)
			}
		}
		sort.Strings(values)
		result.Records = append(result.Records, DNSRecord{Type: "AAAA", Name: domain, Value: values})

	case "CNAME":
		cname, err := resolver.LookupCNAME(ctx, domain)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			return result
		}
		result.Records = append(result.Records, DNSRecord{Type: "CNAME", Name: domain, Value: []string{strings.TrimSuffix(cname, ".")}})

	case "MX":
		mxs, err := resolver.LookupMX(ctx, domain)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			return result
		}
		values := make([]string, 0, len(mxs))
		for _, mx := range mxs {
			values = append(values, fmt.Sprintf("%d %s", mx.Pref, strings.TrimSuffix(mx.Host, ".")))
		}
		sort.Strings(values)
		result.Records = append(result.Records, DNSRecord{Type: "MX", Name: domain, Value: values})

	case "NS":
		ns, err := resolver.LookupNS(ctx, domain)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			return result
		}
		values := make([]string, 0, len(ns))
		for _, n := range ns {
			values = append(values, strings.TrimSuffix(n.Host, "."))
		}
		sort.Strings(values)
		result.Records = append(result.Records, DNSRecord{Type: "NS", Name: domain, Value: values})

	case "TXT":
		txts, err := resolver.LookupTXT(ctx, domain)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			return result
		}
		result.Records = append(result.Records, DNSRecord{Type: "TXT", Name: domain, Value: txts})

	case "SOA":
		result.Errors = append(result.Errors, "Go 标准库不支持 SOA 记录查询（需使用第三方库）")
		return result

	case "PTR":
		names, err := resolver.LookupAddr(ctx, domain)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			return result
		}
		for i := range names {
			names[i] = strings.TrimSuffix(names[i], ".")
		}
		result.Records = append(result.Records, DNSRecord{Type: "PTR", Name: domain, Value: names})

	default:
		result.Errors = append(result.Errors, fmt.Sprintf("不支持的记录类型: %s", recordType))
	}

	return result
}

// LookupPTR 执行反向 DNS（PTR）查询，返回主机名列表。
func LookupPTR(ip string, r *net.Resolver) ([]string, error) {
	resolver := resolverOrDefault(r)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	names, err := resolver.LookupAddr(ctx, ip)
	if err != nil {
		return nil, err
	}
	for i := range names {
		names[i] = strings.TrimSuffix(names[i], ".")
	}
	return names, nil
}
