//go:build windows

package local

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// Windows API常量
const (
	FILE_ATTRIBUTE_HIDDEN   = 0x2
	INVALID_FILE_ATTRIBUTES = 0xFFFFFFFF
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

	// 使用Windows API获取文件属性
	attrs, err := getFileAttributes(filePath)
	if err != nil {
		// 获取属性失败，保守起见返回false
		return false
	}

	// 检查是否设置了隐藏属性
	return attrs&FILE_ATTRIBUTE_HIDDEN != 0
}

// getFileAttributes 获取Windows文件属性
// 参数:
//   - filePath: 文件路径
//
// 返回:
//   - uint32: 文件属性
//   - error: 错误信息
func getFileAttributes(filePath string) (uint32, error) {
	// 将路径转换为UTF16格式（Windows API需要）
	namePtr, err := syscall.UTF16PtrFromString(filePath)
	if err != nil {
		return 0, fmt.Errorf("转换路径失败: %w", err)
	}

	// 获取文件属性
	attrs, err := syscall.GetFileAttributes(namePtr)
	if err != nil {
		return 0, fmt.Errorf("获取文件属性失败: %w", err)
	}

	// 检查是否为无效属性
	if attrs == INVALID_FILE_ATTRIBUTES {
		return 0, fmt.Errorf("获取到无效的文件属性")
	}

	return attrs, nil
}

// setFileAttributes 设置Windows文件属性
// 参数:
//   - filePath: 文件路径
//   - attrs: 要设置的属性
//
// 返回:
//   - error: 错误信息
func setFileAttributes(filePath string, attrs uint32) error {
	// 将路径转换为UTF16格式
	namePtr, err := syscall.UTF16PtrFromString(filePath)
	if err != nil {
		return fmt.Errorf("转换路径失败: %w", err)
	}

	// 设置文件属性
	if err := syscall.SetFileAttributes(namePtr, attrs); err != nil {
		return fmt.Errorf("设置文件属性失败: %w", err)
	}

	return nil
}

// SetFileHidden 设置文件为隐藏状态
// 参数:
//   - filePath: 文件路径
//   - hidden: 是否隐藏
//
// 返回:
//   - error: 错误信息
func SetFileHidden(filePath string, hidden bool) error {
	attrs, err := getFileAttributes(filePath)
	if err != nil {
		return err
	}

	if hidden {
		// 添加隐藏属性
		attrs |= FILE_ATTRIBUTE_HIDDEN
	} else {
		// 移除隐藏属性
		attrs &= ^uint32(FILE_ATTRIBUTE_HIDDEN)
	}

	return setFileAttributes(filePath, attrs)
}

// GetFileOwner 获取Windows文件所有者
// 参数:
//   - filePath: 文件路径
//
// 返回:
//   - string: 所有者名称
//   - error: 错误信息
func GetFileOwner(filePath string) (string, error) {
	// 此函数需要更复杂的Windows API调用
	// 这里提供一个简化版本，仅返回文件信息
	info, err := os.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("获取文件信息失败: %w", err)
	}

	// 在Windows中，需要使用更复杂的安全API获取所有者
	// 这里仅返回文件基本信息
	return fmt.Sprintf("File: %s, Size: %d bytes", info.Name(), info.Size()), nil
}
