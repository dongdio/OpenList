package _189

// LoginResp 登录响应结构体
type LoginResp struct {
	Msg    string `json:"msg"`    // 响应消息
	Result int    `json:"result"` // 响应结果码，0表示成功
	ToURL  string `json:"toUrl"`  // 重定向URL
}

// Error API错误响应结构体
type Error struct {
	ErrorCode string `json:"errorCode"` // 错误代码
	ErrorMsg  string `json:"errorMsg"`  // 错误信息
}

// File 文件信息结构体
type File struct {
	ID         int64  `json:"id"`         // 文件ID
	LastOpTime string `json:"lastOpTime"` // 最后操作时间
	Name       string `json:"name"`       // 文件名
	Size       int64  `json:"size"`       // 文件大小
	Icon       struct {
		SmallURL string `json:"smallUrl"` // 缩略图URL
		// LargeUrl string `json:"largeUrl"` // 大图URL（未使用）
	} `json:"icon"`
	URL string `json:"url"` // 文件URL
}

// Folder 文件夹信息结构体
type Folder struct {
	ID         int64  `json:"id"`         // 文件夹ID
	LastOpTime string `json:"lastOpTime"` // 最后操作时间
	Name       string `json:"name"`       // 文件夹名
}

// Files 文件列表响应结构体
type Files struct {
	ResCode    int    `json:"res_code"`    // 响应代码，0表示成功
	ResMessage string `json:"res_message"` // 响应消息
	FileListAO struct {
		Count      int      `json:"count"`      // 总数
		FileList   []File   `json:"fileList"`   // 文件列表
		FolderList []Folder `json:"folderList"` // 文件夹列表
	} `json:"fileListAO"`
}

// UploadUrlsResp 上传URL响应结构体
type UploadUrlsResp struct {
	Code       string          `json:"code"`       // 响应代码
	UploadUrls map[string]Part `json:"uploadUrls"` // 上传URL映射
}

// Part 上传分片信息结构体
type Part struct {
	RequestURL    string `json:"requestURL"`    // 请求URL
	RequestHeader string `json:"requestHeader"` // 请求头
}

// Rsa RSA加密参数结构体
type Rsa struct {
	Expire int64  `json:"expire"` // 过期时间
	PkID   string `json:"pkId"`   // 公钥ID
	PubKey string `json:"pubKey"` // 公钥
}

// Down 下载信息响应结构体
type Down struct {
	ResCode         int    `json:"res_code"`        // 响应代码
	ResMessage      string `json:"res_message"`     // 响应消息
	FileDownloadURL string `json:"fileDownloadUrl"` // 文件下载URL
}

// DownResp 下载响应结构体
type DownResp struct {
	ResCode         int    `json:"res_code"`    // 响应代码
	ResMessage      string `json:"res_message"` // 响应消息
	FileDownloadURL string `json:"downloadUrl"` // 文件下载URL
}
