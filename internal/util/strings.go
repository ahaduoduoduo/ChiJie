// Package util 提供跨模块复用的轻量工具函数。
package util

import (
	"strconv"
	"strings"
)

// FirstNonEmpty 返回第一个 TrimSpace 后非空的字符串。
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ContainsString 报告 values 是否包含 value（精确匹配）。
func ContainsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

// RemoveString 返回去除全部等于 value 的元素后的切片。
// 复用底层数组以减少分配。
func RemoveString(values []string, value string) []string {
	result := values[:0]
	for _, item := range values {
		if item != value {
			result = append(result, item)
		}
	}
	return result
}

// ParseInt 解析十进制字符串为 int，失败返回 0。
func ParseInt(value string) int {
	if value == "" {
		return 0
	}
	result, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return result
}

// SplitList 用 ',' / '\n' / '|' 切分字符串并去除空项。
func SplitList(value string) []string {
	var result []string
	for _, item := range strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == '|'
	}) {
		item = strings.TrimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}
