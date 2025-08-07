//go:build !windows

package local

import (
	"io/fs"
	"path/filepath"
	"strings"
	"syscall"
)

// isHidden 判断文件是否为隐藏文件（Unix系统）
// 在Unix/Linux系统中，以点(.)开头的文件被视为隐藏文件
// 参数:
//   - f: 文件信息
//   - dirPath: 完整路径（在Unix系统中主要用于额外检查）
//
// 返回:
//   - bool: 如果文件是隐藏文件则返回true，否则返回false
func isHidden(f fs.FileInfo, dirPath string) bool {
	// 主要判断标准：以点(.)开头的文件
	if strings.HasPrefix(f.Name(), ".") {
		return true
	}

	// 额外检查：某些系统可能通过扩展属性标记隐藏文件
	fullPath := filepath.Join(dirPath, f.Name())

	// 尝试获取文件状态
	var stat syscall.Stat_t
	if err := syscall.Stat(fullPath, &stat); err != nil {
		return false // 获取失败，保守返回非隐藏
	}

	// 检查隐藏标志（某些系统可能支持）
	// 这里仅作为扩展，大多数Unix系统仅依赖文件名判断
	return false
}