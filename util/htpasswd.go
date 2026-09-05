package util

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const htpasswdItoa64 = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// GenerateHtpasswd 生成 htpasswd 条目，支持 sha / apr1 / bcrypt / plain。
// 返回 (条目, 算法描述, error)。
func GenerateHtpasswd(password, algo string) (string, string, error) {
	switch strings.ToLower(algo) {
	case "sha":
		return generateHtpasswdSHA(password)
	case "apr1":
		return generateHtpasswdAPR1(password)
	case "bcrypt":
		return generateHtpasswdBcrypt(password)
	case "plain":
		return generateHtpasswdPlain(password)
	default:
		return generateHtpasswdBcrypt(password)
	}
}

// VerifyHtpasswd 校验密码与 htpasswd 条目是否匹配。
func VerifyHtpasswd(password, hash string) bool {
	if strings.HasPrefix(hash, "{SHA}") {
		got, _, _ := generateHtpasswdSHA(password)
		return hash == got
	}
	if strings.HasPrefix(hash, "$apr1$") {
		parts := strings.SplitN(hash, "$", 4)
		if len(parts) < 4 {
			return false
		}
		salt := parts[2]
		return hash == apr1Hash(password, salt)
	}
	if strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") || strings.HasPrefix(hash, "$2y$") {
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	}
	return password == hash
}

// RandomPassword 生成指定长度的随机密码（长度被夹在 [4,64]）。
func RandomPassword(length int) string {
	if length < 4 {
		length = 12
	}
	if length > 64 {
		length = 64
	}
	charset := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*"
	result := make([]byte, length)
	for i := range result {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		result[i] = charset[n.Int64()]
	}
	return string(result)
}

// ========== 内部实现 ==========

func generateHtpasswdSHA(password string) (string, string, error) {
	h := sha1.Sum([]byte(password))
	encoded := base64.StdEncoding.EncodeToString(h[:])
	return fmt.Sprintf("{SHA}%s", encoded), "SHA-1", nil
}

func generateHtpasswdAPR1(password string) (string, string, error) {
	saltBytes := make([]byte, 8)
	if _, err := rand.Read(saltBytes); err != nil {
		return "", "", err
	}
	salt := ""
	for _, b := range saltBytes {
		salt += string(htpasswdItoa64[int(b)%64])
	}
	salt = salt[:8]
	hash := apr1Hash(password, salt)
	return hash, "APR1 (MD5)", nil
}

// apr1Hash 实现 Apache 的 $apr1$ MD5 口令哈希。
func apr1Hash(password, salt string) string {
	salt = salt[:8]

	// Start digest B
	b := md5.New()
	b.Write([]byte(password + "$apr1$" + salt))

	// Digest C: password + salt + password (length-aware)
	c := md5.New()
	c.Write([]byte(password + "$apr1$" + salt))
	cSum := c.Sum(nil)

	// Add for each group of 16 in password length
	pwLen := len(password)
	for n := pwLen; n > 0; n -= 16 {
		if n > 16 {
			b.Write(cSum)
		} else {
			b.Write(cSum[:n])
		}
	}

	// Pad with zeros
	for n := pwLen; n > 0; n >>= 1 {
		if n&1 != 0 {
			b.Write([]byte{0})
		} else {
			b.Write([]byte(password[0:1]))
		}
	}

	intermediate := b.Sum(nil)

	// 1000 rounds of MD5
	for i := 0; i < 1000; i++ {
		round := md5.New()
		if i&1 != 0 {
			round.Write([]byte(password))
		} else {
			round.Write(intermediate)
		}
		if i%3 != 0 {
			round.Write([]byte(salt))
		}
		if i%7 != 0 {
			round.Write([]byte(password))
		}
		if i&1 != 0 {
			round.Write(intermediate)
		} else {
			round.Write([]byte(password))
		}
		intermediate = round.Sum(nil)
	}

	return fmt.Sprintf("$apr1$%s$%s", salt, apr1To64(intermediate))
}

func apr1To64(v []byte) string {
	out := make([]byte, 0, 22)
	out = append(out, apr1To64Chars(to24b(v[0], v[1], v[2]), 4)...)
	out = append(out, apr1To64Chars(to24b(v[3], v[4], v[5]), 4)...)
	out = append(out, apr1To64Chars(to24b(v[6], v[7], v[8]), 4)...)
	out = append(out, apr1To64Chars(to24b(v[9], v[10], v[11]), 4)...)
	out = append(out, apr1To64Chars(to24b(v[12], v[13], v[14]), 4)...)
	out = append(out, apr1To64Chars(uint(v[15]), 2)...)
	return string(out)
}

// to24b 将 3 个字节打包为 24 位整数（小端：a 为低 8 位，c 为高 8 位）。
func to24b(a, b, c byte) uint {
	return uint(a) | uint(b)<<8 | uint(c)<<16
}

// apr1To64Chars 将 v 的低 n*6 位按 6 位一组映射为 itoa64 字符（Apache 自定义 base64 字母表）。
// n 为目标字符数（前 5 组为 4，末组为 2）。
func apr1To64Chars(v uint, n int) []byte {
	result := make([]byte, n)
	for i := 0; i < n; i++ {
		result[i] = htpasswdItoa64[v&0x3f]
		v >>= 6
	}
	return result
}

func generateHtpasswdBcrypt(password string) (string, string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", "", fmt.Errorf("bcrypt 生成失败: %v", err)
	}
	return string(hash), "bcrypt", nil
}

func generateHtpasswdPlain(password string) (string, string, error) {
	return password, "plain", nil
}
