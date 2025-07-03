package common

import (
	"regexp"
	"testing"

	"github.com/dongdio/OpenList/v4/internal/conf"
)

// TestHidePrivacy 测试hidePrivacy函数
// 验证隐私信息是否被正确隐藏
func TestHidePrivacy(t *testing.T) {
	// 设置测试用例
	testCases := []struct {
		name     string           // 测试用例名称
		regexps  []*regexp.Regexp // 隐私规则正则表达式
		input    string           // 输入字符串
		expected string           // 期望输出
	}{
		{
			name: "隐藏访问令牌",
			regexps: []*regexp.Regexp{
				regexp.MustCompile(`(?U)access_token=(.*)&`),
			},
			input:    `Get "https://pan.baidu.com/rest/2.0/xpan/file?access_token=121.d1f66e95acfa40274920079396a51c48.Y2aP2vQDq90hLBE3PAbVije59uTcn7GiWUfw8LCM_olw&dir=%2F&limit=200&method=list&order=name&start=0&web=web" : net/http: TLS handshake timeout`,
			expected: `Get "https://pan.baidu.com/rest/2.0/xpan/file?access_token=**********************************************************************************************&dir=%2F&limit=200&method=list&order=name&start=0&web=web" : net/http: TLS handshake timeout`,
		},
		{
			name: "隐藏多个隐私信息",
			regexps: []*regexp.Regexp{
				regexp.MustCompile(`(?U)access_token=(.*)&`),
				regexp.MustCompile(`(?U)password=([^&]*)`),
			},
			input:    `Get "https://api.example.com/login?username=test&password=secret123&access_token=abc.def.ghi&redirect=home"`,
			expected: `Get "https://api.example.com/login?username=test&password=*********&access_token=***********&redirect=home"`,
		},
		{
			name: "无隐私信息",
			regexps: []*regexp.Regexp{
				regexp.MustCompile(`(?U)access_token=(.*)&`),
			},
			input:    `Get "https://api.example.com/public/data?param=value"`,
			expected: `Get "https://api.example.com/public/data?param=value"`,
		},
	}

	// 运行测试用例
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 设置隐私规则
			conf.PrivacyReg = tc.regexps

			// 运行hidePrivacy函数
			result := hidePrivacy(tc.input)

			// 验证结果
			if result != tc.expected {
				t.Errorf("hidePrivacy() 结果不匹配\n期望: %s\n实际: %s", tc.expected, result)
			}
		})
	}
}