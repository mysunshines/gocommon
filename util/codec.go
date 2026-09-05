package util

import (
	"crypto/sha1"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// ============================================================================
// Base64 / URL / Hex 编解码
// ============================================================================

// Base64EncodeEx 支持 URL-safe 与去填充选项的 Base64 编码。
func Base64EncodeEx(input string, urlSafe, noPadding bool) string {
	encoding := base64.StdEncoding
	if !noPadding {
		encoding = base64.RawStdEncoding
	}
	if urlSafe {
		encoding = base64.URLEncoding
		if !noPadding {
			encoding = base64.RawURLEncoding
		}
	}
	return encoding.EncodeToString([]byte(input))
}

// Base64DecodeEx 支持 URL-safe 与去填充选项的 Base64 解码。
func Base64DecodeEx(input string, urlSafe, noPadding bool) (string, error) {
	encoding := base64.StdEncoding
	if !noPadding {
		encoding = base64.RawStdEncoding
	}
	if urlSafe {
		encoding = base64.URLEncoding
		if !noPadding {
			encoding = base64.RawURLEncoding
		}
	}
	data, err := encoding.DecodeString(input)
	if err != nil {
		return "", fmt.Errorf("Base64 解码失败: %v", err)
	}
	return string(data), nil
}

// URLEncode 对字符串进行 URL 编码（application/x-www-form-urlencoded）。
func URLEncode(input string) string {
	return url.QueryEscape(input)
}

// URLDecode 对 URL 编码字符串进行解码。
func URLDecode(input string) (string, error) {
	result, err := url.QueryUnescape(input)
	if err != nil {
		return "", fmt.Errorf("URL 解码失败: %v", err)
	}
	return result, nil
}

// HexEncode 将字符串编码为十六进制。
func HexEncode(input string) string {
	return hex.EncodeToString([]byte(input))
}

// HexDecode 将十六进制字符串解码为原字符串。
func HexDecode(input string) (string, error) {
	data, err := hex.DecodeString(strings.TrimSpace(input))
	if err != nil {
		return "", fmt.Errorf("Hex 解码失败: %v", err)
	}
	return string(data), nil
}

// SHA1 返回字符串的 SHA-1 十六进制摘要。
func SHA1(input string) string {
	h := sha1.New()
	h.Write([]byte(input))
	return hex.EncodeToString(h.Sum(nil))
}

// SHA512 返回字符串的 SHA-512 十六进制摘要。
func SHA512(input string) string {
	h := sha512.New()
	h.Write([]byte(input))
	return hex.EncodeToString(h.Sum(nil))
}

// ============================================================================
// 字符串处理
// ============================================================================

// ReverseString 按 rune 反转字符串。
func ReverseString(input string) string {
	runes := []rune(input)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

// UnicodeEscape 将非 ASCII 字符转义为 \uXXXX 形式。
func UnicodeEscape(input string) string {
	var buf strings.Builder
	for _, r := range input {
		if r < 128 {
			buf.WriteRune(r)
		} else {
			fmt.Fprintf(&buf, "\\u%04x", r)
		}
	}
	return buf.String()
}

// UnicodeUnescape 将 \uXXXX 转义还原为原字符。
func UnicodeUnescape(input string) (string, error) {
	result := ""
	i := 0
	for i < len(input) {
		if i+6 <= len(input) && input[i] == '\\' && input[i+1] == 'u' {
			hexStr := input[i+2 : i+6]
			code, err := hex.DecodeString(hexStr)
			if err != nil {
				return "", err
			}
			if len(code) == 2 {
				r := rune(code[0])<<8 | rune(code[1])
				result += string(r)
			} else {
				result += string(code)
			}
			i += 6
		} else {
			result += string(input[i])
			i++
		}
	}
	return result, nil
}

// MD5Sum 返回字符串的 MD5 十六进制摘要（与 util.MD5 等价，语义更明确）。
func MD5Sum(input string) string {
	return MD5(input)
}
