//go:build !windows

package local

import (
	"io/fs"
	"strings"
)

// isHidden 判断文件是否为隐藏文件（Unix系统）
// 在Unix/Linux系统中，以点(.)开头的文件被视为隐藏文件
// 参数:
//   - f: 文件信息
//   - _: 完整路径（在Unix系统中未使用，保持与Windows版本接口一致）
//
// 返回:
//   - bool: 如果文件是隐藏文件则返回true，否则返回false
func isHidden(f fs.FileInfo, _ string) bool {
	return strings.HasPrefix(f.Name(), ".")
}
