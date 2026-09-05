package util

import "strings"

// FormatSQL 简单格式化 SQL（关键字大写，主要子句前换行）。
// 注意：不处理字符串字面量内的关键字，仅做基础美化。
func FormatSQL(sqlStr string) string {
	sqlStr = strings.TrimSpace(sqlStr)
	if sqlStr == "" {
		return sqlStr
	}

	keywords := []string{
		"SELECT", "FROM", "WHERE", "AND", "OR", "ORDER BY", "GROUP BY",
		"HAVING", "LIMIT", "OFFSET", "INSERT INTO", "VALUES", "UPDATE",
		"SET", "DELETE FROM", "CREATE TABLE", "ALTER TABLE", "DROP TABLE",
		"JOIN", "LEFT JOIN", "RIGHT JOIN", "INNER JOIN", "OUTER JOIN",
		"ON", "AS", "IN", "NOT", "NULL", "IS", "LIKE", "BETWEEN",
		"UNION", "ALL", "DISTINCT", "CASE", "WHEN", "THEN", "ELSE", "END",
		"COUNT", "SUM", "AVG", "MAX", "MIN", "EXISTS",
	}

	result := sqlStr

	// 关键字大写
	for _, kw := range keywords {
		upperKW := strings.ToUpper(kw)
		idx := 0
		for {
			i := strings.Index(strings.ToUpper(result[idx:]), upperKW)
			if i < 0 {
				break
			}
			actualIdx := idx + i
			// 检查是否为完整单词
			before := actualIdx == 0 || !isAlpha(result[actualIdx-1])
			after := actualIdx+len(kw) >= len(result) || !isAlpha(result[actualIdx+len(kw)])
			if before && after {
				result = result[:actualIdx] + upperKW + result[actualIdx+len(kw):]
			}
			idx = actualIdx + len(kw)
		}
	}

	// 主要子句前换行
	breakBefore := []string{"SELECT", "FROM", "WHERE", "ORDER BY", "GROUP BY",
		"HAVING", "LIMIT", "LEFT JOIN", "RIGHT JOIN", "INNER JOIN", "JOIN",
		"INSERT INTO", "VALUES", "UPDATE", "SET", "DELETE FROM", "UNION",
		"CREATE TABLE", "ALTER TABLE", "DROP TABLE"}
	for _, clause := range breakBefore {
		upper := strings.ToUpper(clause)
		result = strings.ReplaceAll(result, " "+upper, "\n"+upper)
		result = strings.ReplaceAll(result, "\n\t"+upper, "\n"+upper)
	}

	// 逗号后换行（在 SELECT 和 CREATE TABLE 中）
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		if strings.Contains(line, ",") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "SELECT") || strings.HasPrefix(trimmed, "CREATE TABLE") {
				continue // 第一行不拆分
			}
		}
		_ = i
	}

	return strings.TrimSpace(result)
}

// isAlpha 判断字节是否为字母或下划线（用于关键字边界判断）。
func isAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}
