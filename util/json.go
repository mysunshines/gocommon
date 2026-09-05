package util

import (
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"
)

var jsonTitleCaser = cases.Title(language.English)

// JSONFormat 将 JSON 字符串美化（带缩进）输出。
func JSONFormat(raw string) (string, error) {
	var data any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return "", fmt.Errorf("JSON 解析失败: %v", err)
	}
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// JSONToYAML 将 JSON 字符串转换为 YAML。
func JSONToYAML(raw string) (string, error) {
	var data any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return "", fmt.Errorf("JSON 解析失败: %v", err)
	}
	out, err := yaml.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("YAML 生成失败: %v", err)
	}
	return string(out), nil
}

// YAMLToJSON 将 YAML 字符串转换为格式化后的 JSON。
// yaml.Unmarshal 在某些场景下会产生 map[interface{}]interface{}，
// 直接交给 json.Marshal 会报错，故先 NormalizeForJSON 规整。
func YAMLToJSON(raw string) (string, error) {
	var data any
	if err := yaml.Unmarshal([]byte(raw), &data); err != nil {
		return "", fmt.Errorf("YAML 解析失败: %v", err)
	}
	data = NormalizeForJSON(data)
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", fmt.Errorf("JSON 格式化失败: %v", err)
	}
	return string(out), nil
}

// NormalizeForJSON 将 yaml.Unmarshal 产生的 map[interface{}]interface{} /
// []interface{} 递归规整为 map[string]interface{} / []any，使其可被 json.Marshal。
func NormalizeForJSON(v any) any {
	switch val := v.(type) {
	case map[string]any:
		for k, vv := range val {
			val[k] = NormalizeForJSON(vv)
		}
		return val
	case map[any]any:
		m := make(map[string]any, len(val))
		for k, vv := range val {
			m[fmt.Sprint(k)] = NormalizeForJSON(vv)
		}
		return m
	case []any:
		for i, vv := range val {
			val[i] = NormalizeForJSON(vv)
		}
		return val
	default:
		return val
	}
}

// ToPascalCase 将 snake_case / kebab-case / 空格分隔 / camelCase 转为 PascalCase。
func ToPascalCase(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, ".", " ")

	words := strings.Fields(s)
	for i, w := range words {
		if len(w) == 0 {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, "")
}
