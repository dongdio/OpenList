package _189pc

import (
	"encoding/xml"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dongdio/OpenList/v4/utility/utils"
)

// RespErr 表示API响应中的错误信息，兼容189云盘API的多种错误返回格式。
type RespErr struct {
	ResponseCode    any    `json:"res_code"`    // 响应代码，可能是整数或字符串
	ResponseMessage string `json:"res_message"` // 响应消息，描述错误详情

	ErrorDesc string `json:"error"` // 错误描述，另一种格式的错误字段

	XMLName xml.Name `xml:"error"`                  // XML根元素名称
	Code    string   `json:"code" xml:"code"`       // 错误代码
	Message string   `json:"message" xml:"message"` // 错误消息
	Msg     string   `json:"msg"`                   // 错误消息，另一种格式

	ErrorCode string `json:"errorCode"` // 错误代码，另一种格式
	ErrorMsg  string `json:"errorMsg"`  // 错误消息，另一种格式
}

// HasError 检查响应是否包含错误，根据不同的错误格式进行判断。
func (e *RespErr) HasError() bool {
	switch v := e.ResponseCode.(type) {
	case int, int64, int32:
		return v != 0
	case string:
		return e.ResponseCode != ""
	}
	return (e.Code != "" && e.Code != "SUCCESS") || e.ErrorCode != "" || e.ErrorDesc != ""
}

// Error 实现error接口，返回格式化的错误信息。
func (e *RespErr) Error() string {
	switch v := e.ResponseCode.(type) {
	case int, int64, int32:
		if v != 0 {
			return fmt.Sprintf("res_code: %d, res_msg: %s", v, e.ResponseMessage)
		}
	case string:
		if e.ResponseCode != "" {
			return fmt.Sprintf("res_code: %s, res_msg: %s", e.ResponseCode, e.ResponseMessage)
		}
	}

	if e.Code != "" && e.Code != "SUCCESS" {
		if e.Msg != "" {
			return fmt.Sprintf("code: %s, msg: %s", e.Code, e.Msg)
		}
		if e.Message != "" {
			return fmt.Sprintf("code: %s, msg: %s", e.Code, e.Message)
		}
		return "code: " + e.Code
	}

	if e.ErrorCode != "" {
		return fmt.Sprintf("err_code: %s, err_msg: %s", e.ErrorCode, e.ErrorMsg)
	}

	if e.ErrorDesc != "" {
		return fmt.Sprintf("error: %s, message: %s", e.ErrorDesc, e.Message)
	}
	return ""
}

// LoginParam 存储登录所需的参数信息，包括加密后的凭据和相关令牌。
type LoginParam struct {
	// 加密后的用户名和密码
	RsaUsername string // 经过RSA加密的用户名
	RsaPassword string // 经过RSA加密的密码

	// RSA密钥
	JRsaKey string `json:"jRsaKey"` // RSA公钥标识

	// 请求头参数
	Lt    string // 登录令牌
	ReqID string `json:"ReqId"` // 请求ID

	// 表单参数
	ParamID string `json:"ParamId"` // 参数ID

	// 验证码相关
	CaptchaToken string // 验证码令牌
	ValidateCode string // 验证码值
}

// EncryptConfResp 登录加密相关配置响应，包含加密公钥等信息。
type EncryptConfResp struct {
	Result int `json:"result"` // 请求结果代码，0表示成功
	Data   struct {
		UpSmsOn   string `json:"upSmsOn"`   // 是否需要短信验证
		Pre       string `json:"pre"`       // 前缀信息
		PreDomain string `json:"preDomain"` // 前缀域名
		PubKey    string `json:"pubKey"`    // RSA公钥，用于加密用户名和密码
	} `json:"data"` // 响应数据
}

// LoginResp 登录响应，包含登录结果和跳转URL。
type LoginResp struct {
	Msg    string `json:"msg"`    // 响应消息
	Result int    `json:"result"` // 结果代码，0表示成功
	ToUrl  string `json:"toUrl"`  // 登录成功后的跳转URL
}

// UserSessionResp 刷新会话时返回的响应，包含会话密钥等信息。
type UserSessionResp struct {
	ResCode    int    `json:"res_code"`    // 响应代码
	ResMessage string `json:"res_message"` // 响应消息

	LoginName string `json:"loginName"` // 登录用户名

	KeepAlive       int `json:"keepAlive"`       // 会话保持时间
	GetFileDiffSpan int `json:"getFileDiffSpan"` // 获取文件差异的时间间隔
	GetUserInfoSpan int `json:"getUserInfoSpan"` // 获取用户信息的时间间隔

	// 个人云会话信息
	SessionKey    string `json:"sessionKey"`    // 个人云会话密钥
	SessionSecret string `json:"sessionSecret"` // 个人云会话秘钥
	// 家庭云会话信息
	FamilySessionKey    string `json:"familySessionKey"`    // 家庭云会话密钥
	FamilySessionSecret string `json:"familySessionSecret"` // 家庭云会话秘钥
}

// AppSessionResp 登录返回的会话信息，包含访问令牌和刷新令牌。
type AppSessionResp struct {
	UserSessionResp

	IsSaveName string `json:"isSaveName"` // 是否保存用户名

	// 会话刷新令牌
	AccessToken string `json:"accessToken"` // 访问令牌，用于API调用
	// 令牌刷新
	RefreshToken string `json:"refreshToken"` // 刷新令牌，用于更新访问令牌
}

// FamilyInfoListResp 家庭云账户列表响应，包含多个家庭账户信息。
type FamilyInfoListResp struct {
	FamilyInfoResp []FamilyInfoResp `json:"familyInfoResp"` // 家庭账户信息列表
}

// FamilyInfoResp 家庭云账户信息，包含账户的基本信息和角色。
type FamilyInfoResp struct {
	Count      int    `json:"count"`      // 成员数量
	CreateTime string `json:"createTime"` // 创建时间
	FamilyID   int64  `json:"familyId"`   // 家庭ID
	RemarkName string `json:"remarkName"` // 备注名称
	Type       int    `json:"type"`       // 账户类型
	UseFlag    int    `json:"useFlag"`    // 使用标志
	UserRole   int    `json:"userRole"`   // 用户角色
}

/* 文件部分 */

// Cloud189File 表示189云盘中的文件，包含文件的基本信息和图标链接。
type Cloud189File struct {
	ID   String `json:"id"`   // 文件ID
	Name string `json:"name"` // 文件名称
	Size int64  `json:"size"` // 文件大小
	Md5  string `json:"md5"`  // 文件MD5值

	LastOpTime Time `json:"lastOpTime"` // 最后操作时间
	CreateDate Time `json:"createDate"` // 创建时间
	Icon       struct {
		// iconOption 5
		SmallURL string `json:"smallUrl"` // 小图标URL
		LargeURL string `json:"largeUrl"` // 大图标URL

		// iconOption 10
		Max600    string `json:"max600"`    // 最大600尺寸图标URL
		MediumURL string `json:"mediumUrl"` // 中等尺寸图标URL
	} `json:"icon"` // 图标信息

	// Orientation int64  `json:"orientation"` // 方向
	// FileCata   int64  `json:"fileCata"` // 文件分类
	// MediaType   int    `json:"mediaType"` // 媒体类型
	// Rev         string `json:"rev"` // 版本号
	// StarLabel   int64  `json:"starLabel"` // 星标标签
}

// CreateTime 获取文件的创建时间。
func (c *Cloud189File) CreateTime() time.Time {
	return time.Time(c.CreateDate)
}

// GetHash 获取文件的哈希信息。
func (c *Cloud189File) GetHash() utils.HashInfo {
	return utils.NewHashInfo(utils.MD5, c.Md5)
}

// GetSize 获取文件大小。
func (c *Cloud189File) GetSize() int64 { return c.Size }

// GetName 获取文件名称。
func (c *Cloud189File) GetName() string { return c.Name }

// ModTime 获取文件修改时间。
func (c *Cloud189File) ModTime() time.Time { return time.Time(c.LastOpTime) }

// IsDir 判断是否为目录。
func (c *Cloud189File) IsDir() bool { return false }

// GetID 获取文件ID。
func (c *Cloud189File) GetID() string { return string(c.ID) }

// GetPath 获取文件路径。
func (c *Cloud189File) GetPath() string { return "" }

// Thumb 获取文件缩略图URL。
func (c *Cloud189File) Thumb() string { return c.Icon.SmallURL }

// Cloud189Folder 表示189云盘中的文件夹，包含文件夹的基本信息。
type Cloud189Folder struct {
	ID       String `json:"id"`       // 文件夹ID
	ParentID int64  `json:"parentId"` // 父文件夹ID
	Name     string `json:"name"`     // 文件夹名称

	LastOpTime Time `json:"lastOpTime"` // 最后操作时间
	CreateDate Time `json:"createDate"` // 创建时间

	// FileListSize int64 `json:"fileListSize"` // 文件列表大小
	// FileCount int64 `json:"fileCount"` // 文件数量
	// FileCata  int64 `json:"fileCata"` // 文件分类
	// Rev          string `json:"rev"` // 版本号
	// StarLabel    int64  `json:"starLabel"` // 星标标签
}

// CreateTime 获取文件夹的创建时间。
func (c *Cloud189Folder) CreateTime() time.Time {
	return time.Time(c.CreateDate)
}

// GetHash 获取文件夹的哈希信息（文件夹没有哈希）。
func (c *Cloud189Folder) GetHash() utils.HashInfo {
	return utils.HashInfo{}
}

// GetSize 获取文件夹大小（文件夹大小为0）。
func (c *Cloud189Folder) GetSize() int64 { return 0 }

// GetName 获取文件夹名称。
func (c *Cloud189Folder) GetName() string { return c.Name }

// ModTime 获取文件夹修改时间
func (c *Cloud189Folder) ModTime() time.Time { return time.Time(c.LastOpTime) }

// IsDir 判断是否为目录
func (c *Cloud189Folder) IsDir() bool { return true }

// GetID 获取文件夹ID
func (c *Cloud189Folder) GetID() string { return string(c.ID) }

// GetPath 获取文件夹路径
func (c *Cloud189Folder) GetPath() string { return "" }

// Cloud189FilesResp 文件列表响应
type Cloud189FilesResp struct {
	// ResCode    int    `json:"res_code"`
	// ResMessage string `json:"res_message"`
	FileListAO struct {
		Count      int              `json:"count"`
		FileList   []Cloud189File   `json:"fileList"`
		FolderList []Cloud189Folder `json:"folderList"`
	} `json:"fileListAO"`
}

// BatchTaskInfo 批量任务信息
type BatchTaskInfo struct {
	// FileID 文件ID
	FileID string `json:"fileId"`
	// FileName 文件名
	FileName string `json:"fileName"`
	// IsFolder 是否是文件夹，0-否，1-是
	IsFolder int `json:"isFolder"`
	// SrcParentID 文件所在父目录ID
	SrcParentID string `json:"srcParentId,omitempty"`

	/* 冲突管理 */
	// 1 -> 跳过 2 -> 保留 3 -> 覆盖
	DealWay    int `json:"dealWay,omitempty"`
	IsConflict int `json:"isConflict,omitempty"`
}

/* 上传部分 */
// InitMultiUploadResp 初始化分片上传的响应
type InitMultiUploadResp struct {
	// Code string `json:"code"`
	Data struct {
		UploadType     int    `json:"uploadType"`
		UploadHost     string `json:"uploadHost"`
		UploadFileID   string `json:"uploadFileId"`
		FileDataExists int    `json:"fileDataExists"`
	} `json:"data"`
}

// UploadUrlsResp 上传URL响应
type UploadUrlsResp struct {
	Code string                    `json:"code"`
	Data map[string]UploadUrlsData `json:"uploadUrls"`
}

// UploadUrlsData 上传URL数据
type UploadUrlsData struct {
	RequestURL    string `json:"requestURL"`
	RequestHeader string `json:"requestHeader"`
}

// UploadUrlInfo 上传URL信息，包含分片序号和请求头
type UploadUrlInfo struct {
	PartNumber int
	Headers    map[string]string
	UploadUrlsData
}

// UploadProgress 上传进度信息
type UploadProgress struct {
	UploadInfo  InitMultiUploadResp
	UploadParts []string
}

// CreateUploadFileResp 创建上传文件的响应
type CreateUploadFileResp struct {
	// 上传文件请求ID
	UploadFileID int64 `json:"uploadFileId"`
	// 上传文件数据的URL路径
	FileUploadURL string `json:"fileUploadUrl"`
	// 上传文件完成后确认路径
	FileCommitURL string `json:"fileCommitUrl"`
	// 文件是否已存在云盘中，0-未存在，1-已存在
	FileDataExists int `json:"fileDataExists"`
}

// GetUploadFileStatusResp 获取上传文件状态的响应
type GetUploadFileStatusResp struct {
	CreateUploadFileResp

	// 已上传的大小
	DataSize int64 `json:"dataSize"`
	Size     int64 `json:"size"`
}

// GetSize 获取文件大小
func (r *GetUploadFileStatusResp) GetSize() int64 {
	return r.Size
}

// CommitMultiUploadFileResp 提交分片上传文件的响应
type CommitMultiUploadFileResp struct {
	File struct {
		UserFileID String `json:"userFileId"`
		FileName   string `json:"fileName"`
		FileSize   int64  `json:"fileSize"`
		FileMd5    string `json:"fileMd5"`
		CreateDate Time   `json:"createDate"`
	} `json:"file"`
}

// toFile 将提交响应转换为文件对象
func (f *CommitMultiUploadFileResp) toFile() *Cloud189File {
	return &Cloud189File{
		ID:         f.File.UserFileID,
		Name:       f.File.FileName,
		Size:       f.File.FileSize,
		Md5:        f.File.FileMd5,
		LastOpTime: f.File.CreateDate,
		CreateDate: f.File.CreateDate,
	}
}

// OldCommitUploadFileResp 旧版上传文件提交响应
type OldCommitUploadFileResp struct {
	XMLName    xml.Name `xml:"file"`
	ID         String   `xml:"id"`
	Name       string   `xml:"name"`
	Size       int64    `xml:"size"`
	Md5        string   `xml:"md5"`
	CreateDate Time     `xml:"createDate"`
}

// toFile 将旧版上传响应转换为文件对象
func (f *OldCommitUploadFileResp) toFile() *Cloud189File {
	return &Cloud189File{
		ID:         f.ID,
		Name:       f.Name,
		Size:       f.Size,
		Md5:        f.Md5,
		CreateDate: f.CreateDate,
		LastOpTime: f.CreateDate,
	}
}

// CreateBatchTaskResp 创建批量任务的响应
type CreateBatchTaskResp struct {
	TaskID string `json:"taskId"`
}

// BatchTaskStateResp 批量任务状态响应
type BatchTaskStateResp struct {
	FailedCount         int     `json:"failedCount"`
	Process             int     `json:"process"`
	SkipCount           int     `json:"skipCount"`
	SubTaskCount        int     `json:"subTaskCount"`
	SuccessedCount      int     `json:"successedCount"`
	SuccessedFileIDList []int64 `json:"successedFileIdList"`
	TaskID              string  `json:"taskId"`
	TaskStatus          int     `json:"taskStatus"` // 1 初始化 2 存在冲突 3 执行中，4 完成
}

// BatchTaskConflictTaskInfoResp 批量任务冲突信息响应，包含任务ID和冲突文件信息。
type BatchTaskConflictTaskInfoResp struct {
	SessionKey     string          `json:"sessionKey"`     // 会话密钥
	TargetFolderID int             `json:"targetFolderId"` // 目标文件夹ID
	TaskID         string          `json:"taskId"`         // 任务ID
	TaskInfos      []BatchTaskInfo // 任务信息列表
	TaskType       int             `json:"taskType"` // 任务类型
}

// Params 请求参数映射，用于构建URL查询字符串。
type Params map[string]string

// Set 设置参数键值对。
func (p Params) Set(k, v string) {
	p[k] = v
}

// Encode 将参数编码为URL查询字符串。
func (p Params) Encode() string {
	if p == nil {
		return ""
	}
	var buf strings.Builder
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if buf.Len() > 0 {
			buf.WriteByte('&')
		}
		buf.WriteString(k)
		buf.WriteByte('=')
		buf.WriteString(p[k])
	}
	return buf.String()
}
