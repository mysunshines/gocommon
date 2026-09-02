package configcenter

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strconv"

	"gopkg.in/yaml.v3"
)

// parseConsulIndex 解析 Consul 响应头中的 X-Consul-Index（uint64）。
// 解析失败（空或非法）时返回 0。
func parseConsulIndex(s string) uint64 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// decodeValue 将 Consul KV 返回的 base64 字符串解码为原始字节。
func decodeValue(b64 string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(b64)
}

// bytesReader 返回一个读取给定字节切片的 io.Reader（避免重复 import bytes）。
func bytesReader(b []byte) *bytes.Reader {
	return bytes.NewReader(b)
}

// unmarshalAuto 根据内容尝试 JSON，失败再尝试 YAML。
// Consul KV 中既可以直接存 YAML（推荐，和 config_xxx.yaml 格式一致），
// 也可以存 JSON；两者都支持，降低后台写入的实现成本。
func unmarshalAuto(raw []byte, out interface{}) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		if err := json.Unmarshal(trimmed, out); err == nil {
			return nil
		}
	}
	return yaml.Unmarshal(trimmed, out)
}
