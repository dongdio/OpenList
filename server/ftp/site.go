package ftp

import (
	"fmt"
	"strconv"

	ftpserver "github.com/fclairamb/ftpserverlib"
)

// HandleSIZE 处理FTP SIZE命令，用于设置下一个上传文件的大小
// param: SIZE命令的参数（文件大小）
// client: FTP客户端驱动
// 返回状态码和响应消息
func HandleSIZE(param string, client ftpserver.ClientDriver) (int, string) {
	// 尝试将客户端转换为AferoAdapter类型
	fs, ok := client.(*AferoAdapter)
	if !ok {
		return ftpserver.StatusNotLoggedIn, "Unexpected exception (driver is nil)"
	}

	// 解析文件大小参数
	size, err := strconv.ParseInt(param, 10, 64)
	if err != nil {
		return ftpserver.StatusSyntaxErrorParameters, fmt.Sprintf(
			"Couldn't parse file size, given: %s, err: %v", param, err)
	}

	// 设置下一个文件的大小
	fs.SetNextFileSize(size)
	return ftpserver.StatusOK, "Accepted next file size"
}
