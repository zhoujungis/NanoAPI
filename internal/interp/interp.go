// Package interp 实现 {{变量}} 形式的字符串插值。
package interp

import (
	"regexp"
	"sort"
	"strings"
)

// maxDepth 限制递归替换的层数，防止变量互相引用形成死循环。
const maxDepth = 3

// pattern 匹配 {{name}}，name 允许包含字母、数字、下划线、点和连字符。
var pattern = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)

// Vars 是变量表，键为变量名。
type Vars map[string]string

// Resolve 把字符串中的 {{变量}} 替换为实际值。
// 未命中的变量保持原样，并出现在返回的未解析列表中。
func Resolve(s string, vars Vars) (string, []string) {
	cur := s
	for depth := 0; depth < maxDepth; depth++ {
		if !strings.Contains(cur, "{{") {
			break
		}
		next := pattern.ReplaceAllStringFunc(cur, func(m string) string {
			key := strings.TrimSpace(pattern.FindStringSubmatch(m)[1])
			if v, ok := vars[key]; ok {
				return v
			}
			return m
		})
		if next == cur {
			break
		}
		cur = next
	}
	return cur, Unresolved(cur)
}

// Unresolved 返回字符串中仍未被替换的变量名（已去重并排序）。
func Unresolved(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range pattern.FindAllStringSubmatch(s, -1) {
		key := strings.TrimSpace(m[1])
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// Names 列出字符串中引用到的所有变量名。
func Names(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range pattern.FindAllStringSubmatch(s, -1) {
		key := strings.TrimSpace(m[1])
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}
