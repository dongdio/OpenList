// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webdav

// The If header is covered by Section 10.4.
// http://www.webdav.org/specs/rfc4918.html#HEADER_If

import (
	"strings"
)

// ifHeader 表示WebDAV的If头部
// 它是一组ifList的逻辑或(OR)组合
type ifHeader struct {
	lists []ifList
}

// ifList 表示条件的逻辑与(AND)组合，带有可选的资源标签
type ifList struct {
	resourceTag string      // 资源标签，如果有的话
	conditions  []Condition // 条件列表，需要全部满足
}

// Condition 表示WebDAV If头部中的单个条件
// 未在此文件中定义，应该在其他地方定义
// 参见 http://www.webdav.org/specs/rfc4918.html#HEADER_If

// parseIfHeader 解析WebDAV的If头部
// 例如: "If: (<locktoken:a> <locktoken:b>) (Not <locktoken:c>)"
//
// 参数:
//   - httpHeader: HTTP头部值，不包含"If:"前缀，并且所有"\r\n"已替换为空格
//
// 返回:
//   - ifHeader: 解析后的If头部结构
//   - bool: 是否解析成功
func parseIfHeader(httpHeader string) (h ifHeader, ok bool) {
	s := strings.TrimSpace(httpHeader)
	if s == "" {
		return ifHeader{}, false
	}

	// 检查第一个标记的类型
	tokenType, _, _ := lex(s)
	switch tokenType {
	case '(':
		// 无标签列表：(condition1 condition2)
		return parseNoTagLists(s)
	case angleTokenType:
		// 带标签列表：<resource-tag> (condition1 condition2)
		return parseTaggedLists(s)
	default:
		return ifHeader{}, false
	}
}

// parseNoTagLists 解析没有资源标签的列表
//
// 参数:
//   - s: 要解析的字符串
//
// 返回:
//   - ifHeader: 解析后的头部
//   - bool: 是否解析成功
func parseNoTagLists(s string) (h ifHeader, ok bool) {
	// 不断解析列表，直到字符串结束
	for {
		// 解析单个列表
		list, remaining, ok := parseList(s)
		if !ok {
			return ifHeader{}, false
		}

		// 添加到结果中
		h.lists = append(h.lists, list)

		// 如果没有剩余内容，解析完成
		if remaining == "" {
			return h, true
		}

		// 否则继续解析剩余部分
		s = remaining
	}
}

// parseTaggedLists 解析带有资源标签的列表
//
// 参数:
//   - s: 要解析的字符串
//
// 返回:
//   - ifHeader: 解析后的头部
//   - bool: 是否解析成功
func parseTaggedLists(s string) (h ifHeader, ok bool) {
	resourceTag, listCount := "", 0

	for first := true; ; first = false {
		tokenType, tokenStr, remaining := lex(s)

		switch tokenType {
		case angleTokenType:
			// 找到一个资源标签：<resource-tag>
			// 除了第一个标记外，如果前一个标签没有列表，则格式错误
			if !first && listCount == 0 {
				return ifHeader{}, false
			}

			// 更新当前资源标签，重置列表计数
			resourceTag, listCount = tokenStr, 0
			s = remaining

		case '(':
			// 找到一个列表的开始：(
			listCount++

			// 解析列表
			list, remaining, ok := parseList(s)
			if !ok {
				return ifHeader{}, false
			}

			// 设置资源标签并添加到结果
			list.resourceTag = resourceTag
			h.lists = append(h.lists, list)

			// 如果没有剩余内容，解析完成
			if remaining == "" {
				return h, true
			}

			// 否则继续解析剩余部分
			s = remaining

		default:
			return ifHeader{}, false
		}
	}
}

// parseList 解析单个条件列表
//
// 参数:
//   - s: 要解析的字符串，应以'('开头
//
// 返回:
//   - ifList: 解析后的列表
//   - string: 剩余的未解析字符串
//   - bool: 是否解析成功
func parseList(s string) (l ifList, remaining string, ok bool) {
	// 确认列表以'('开头
	tokenType, _, s := lex(s)
	if tokenType != '(' {
		return ifList{}, "", false
	}

	// 解析列表中的所有条件，直到遇到')'
	for {
		tokenType, _, remaining = lex(s)

		// 列表结束
		if tokenType == ')' {
			// 列表不能为空
			if len(l.conditions) == 0 {
				return ifList{}, "", false
			}
			return l, remaining, true
		}

		// 解析单个条件
		c, remaining, ok := parseCondition(s)
		if !ok {
			return ifList{}, "", false
		}

		// 添加条件并继续
		l.conditions = append(l.conditions, c)
		s = remaining
	}
}

// parseCondition 解析单个条件
//
// 参数:
//   - s: 要解析的字符串
//
// 返回:
//   - Condition: 解析后的条件
//   - string: 剩余的未解析字符串
//   - bool: 是否解析成功
func parseCondition(s string) (c Condition, remaining string, ok bool) {
	// 解析第一个标记
	tokenType, tokenStr, s := lex(s)

	// 检查是否是Not条件
	if tokenType == notTokenType {
		c.Not = true
		tokenType, tokenStr, s = lex(s)
	}

	// 根据标记类型填充条件
	switch tokenType {
	case strTokenType, angleTokenType:
		// 字符串或尖括号标记：foo 或 <bar>
		c.Token = tokenStr
	case squareTokenType:
		// 方括号标记：[etag]
		c.ETag = tokenStr
	default:
		return Condition{}, "", false
	}

	return c, s, true
}

// 标记类型常量
// 单字符标记如'('或')'的类型等于它们的rune值
// 其他所有标记都有负值的类型
const (
	errTokenType    = rune(-1) // 错误标记类型
	eofTokenType    = rune(-2) // 输入结束标记类型
	strTokenType    = rune(-3) // 字符串标记类型
	notTokenType    = rune(-4) // Not标记类型
	angleTokenType  = rune(-5) // 尖括号标记类型，如<foo>
	squareTokenType = rune(-6) // 方括号标记类型，如[bar]
)

// lex 词法分析函数，将输入字符串分解为标记
//
// 参数:
//   - s: 要分析的字符串
//
// 返回:
//   - tokenType: 标记类型
//   - tokenStr: 标记内容
//   - remaining: 剩余的未解析字符串
func lex(s string) (tokenType rune, tokenStr string, remaining string) {
	// 解析HTTP头部的net/textproto Reader会将跨越多个"\r\n"行的
	// 线性空白折叠为单个" "，所以我们不需要查找'\r'或'\n'

	// 跳过前导空白
	for len(s) > 0 && (s[0] == '\t' || s[0] == ' ') {
		s = s[1:]
	}

	// 处理空字符串
	if len(s) == 0 {
		return eofTokenType, "", ""
	}

	// 查找第一个特殊字符位置
	i := 0
loop:
	for ; i < len(s); i++ {
		switch s[i] {
		case '\t', ' ', '(', ')', '<', '>', '[', ']':
			break loop
		}
	}

	// 处理普通字符串标记
	if i != 0 {
		tokenStr, remaining = s[:i], s[i:]
		// 特殊处理"Not"关键字
		if tokenStr == "Not" {
			return notTokenType, "", remaining
		}
		return strTokenType, tokenStr, remaining
	}

	// 处理特殊字符标记
	switch s[0] {
	case '<':
		// 查找匹配的'>'
		j := strings.IndexByte(s, '>')
		if j < 0 {
			return errTokenType, "", ""
		}
		return angleTokenType, s[1:j], s[j+1:]
	case '[':
		// 查找匹配的']'
		j := strings.IndexByte(s, ']')
		if j < 0 {
			return errTokenType, "", ""
		}
		return squareTokenType, s[1:j], s[j+1:]
	default:
		// 单字符标记
		return rune(s[0]), "", s[1:]
	}
}
