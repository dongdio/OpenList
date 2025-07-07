//go:build windows

package local

import (
	"io/fs"
	"path/filepath"
	"syscall"
)

// isHidden 判断文件是否为隐藏文件（Windows系统）
// 在Windows系统中，通过文件属性的FILE_ATTRIBUTE_HIDDEN标志判断
// 参数:
//   - f: 文件信息
//   - fullPath: 文件所在目录的完整路径
//
// 返回:
//   - bool: 如果文件是隐藏文件则返回true，否则返回false
func isHidden(f fs.FileInfo, fullPath string) bool {
	// 构建完整文件路径
	filePath := filepath.Join(fullPath, f.Name())

	// 将路径转换为UTF16格式（Windows API需要）
	namePtr, err := syscall.UTF16PtrFromString(filePath)
	if err != nil {
		// 转换失败，保守起见返回false
		return false
	}

	// 获取文件属性
	attrs, err := syscall.GetFileAttributes(namePtr)
	if err != nil {
		// 获取属性失败，保守起见返回false
		return false
	}

	// 检查是否设置了隐藏属性
	return attrs&syscall.FILE_ATTRIBUTE_HIDDEN != 0
}
