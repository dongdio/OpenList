package common

import (
	"testing"
)

// TestIsApply 测试IsApply函数
// 验证元数据路径是否应用于请求路径的判断
func TestIsApply(t *testing.T) {
	// 测试用例
	testCases := []struct {
		name     string // 测试名称
		metaPath string // 元数据路径
		reqPath  string // 请求路径
		applySub bool   // 是否应用于子路径
		expected bool   // 期望结果
	}{
		{
			name:     "相同路径",
			metaPath: "/",
			reqPath:  "/",
			applySub: false,
			expected: true,
		},
		{
			name:     "子路径且应用于子路径",
			metaPath: "/",
			reqPath:  "/test",
			applySub: true,
			expected: true,
		},
		{
			name:     "子路径但不应用于子路径",
			metaPath: "/",
			reqPath:  "/test",
			applySub: false,
			expected: false,
		},
		{
			name:     "非子路径",
			metaPath: "/test",
			reqPath:  "/",
			applySub: true,
			expected: false,
		},
		{
			name:     "多级子路径",
			metaPath: "/parent",
			reqPath:  "/parent/child/file",
			applySub: true,
			expected: true,
		},
	}

	// 执行测试
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := IsApply(tc.metaPath, tc.reqPath, tc.applySub)
			if result != tc.expected {
				t.Errorf("IsApply(%q, %q, %v) = %v, 期望 %v",
					tc.metaPath, tc.reqPath, tc.applySub, result, tc.expected)
			}
		})
	}
}
