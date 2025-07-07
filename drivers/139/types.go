package _139

import (
	"encoding/xml"
)

// 云盘类型常量
const (
	MetaPersonal    string = "personal"     // 旧版个人云盘，表示旧版个人云服务类型
	MetaFamily      string = "family"       // 家庭云，表示家庭共享云服务类型
	MetaGroup       string = "group"        // 群组云，表示群组共享云服务类型
	MetaPersonalNew string = "personal_new" // 新版个人云盘，表示新版个人云服务类型
)

// BaseResp 定义 API 响应的基础结构
// 用于所有接口的通用返回，包含请求是否成功、状态码和错误信息
// Success: 是否成功，Code: 状态码，Message: 错误信息
type BaseResp struct {
	Success bool   `json:"success"` // 请求是否成功，true 表示成功，false 表示失败
	Code    string `json:"code"`    // 状态码，用于标识具体的错误类型或成功状态
	Message string `json:"message"` // 错误消息，当请求失败时提供具体的错误描述
}

// Catalog 表示云盘目录结构
// 包含目录的基本信息，如 ID、名称、创建时间和更新时间
type Catalog struct {
	CatalogID   string `json:"catalogID"`   // 目录ID，唯一标识一个目录
	CatalogName string `json:"catalogName"` // 目录名称，用于显示和识别目录
	CreateTime  string `json:"createTime"`  // 创建时间，记录目录创建的时间戳
	UpdateTime  string `json:"updateTime"`  // 更新时间，记录目录最后更新的时间戳
}

// Content 表示云盘文件内容结构
// 包含文件的基本信息，如 ID、名称、大小、创建时间、更新时间和缩略图
type Content struct {
	ContentID    string `json:"contentID"`    // 文件ID，唯一标识一个文件
	ContentName  string `json:"contentName"`  // 文件名，用于显示和识别文件
	ContentSize  int64  `json:"contentSize"`  // 文件大小，单位为字节
	CreateTime   string `json:"createTime"`   // 创建时间，记录文件创建的时间戳
	UpdateTime   string `json:"updateTime"`   // 更新时间，记录文件最后更新的时间戳
	ThumbnailURL string `json:"thumbnailURL"` // 缩略图URL，用于预览文件的缩略图
	Digest       string `json:"digest"`       // 文件摘要（MD5），用于校验文件完整性
}

// GetDiskResp 表示获取磁盘内容的响应结构
// 包含目录和文件列表，以及是否完成的标志
type GetDiskResp struct {
	BaseResp
	Data struct {
		Result struct {
			ResultCode string `json:"resultCode"` // 结果码，标识请求的具体结果状态
			ResultDesc any    `json:"resultDesc"` // 结果描述，提供额外的结果信息
		} `json:"result"`
		GetDiskResult struct {
			ParentCatalogID string    `json:"parentCatalogID"` // 父目录ID，标识当前列表所在的父目录
			NodeCount       int       `json:"nodeCount"`       // 节点数量，记录目录和文件的总数
			CatalogList     []Catalog `json:"catalogList"`     // 目录列表，包含当前目录下的所有子目录
			ContentList     []Content `json:"contentList"`     // 文件列表，包含当前目录下的所有文件
			IsCompleted     int       `json:"isCompleted"`     // 是否完成，标识列表是否已全部加载
		} `json:"getDiskResult"`
	} `json:"data"`
}

// UploadResp 表示上传文件的响应结构
// 包含上传任务ID、重定向URL和文件ID列表等信息
type UploadResp struct {
	BaseResp
	Data struct {
		Result struct {
			ResultCode string `json:"resultCode"` // 结果码，标识上传请求的结果状态
			ResultDesc any    `json:"resultDesc"` // 结果描述，提供额外的上传结果信息
		} `json:"result"`
		UploadResult struct {
			UploadTaskID     string `json:"uploadTaskID"`   // 上传任务ID，唯一标识一个上传任务
			RedirectionURL   string `json:"redirectionUrl"` // 上传重定向URL，用于实际文件上传的地址
			NewContentIDList []struct {
				ContentID     string `json:"contentID"`     // 文件ID，上传后分配的唯一标识
				ContentName   string `json:"contentName"`   // 文件名，上传文件的名称
				IsNeedUpload  string `json:"isNeedUpload"`  // 是否需要上传，标识文件是否需要实际上传
				FileEtag      int64  `json:"fileEtag"`      // 文件Etag，用于文件版本控制
				FileVersion   int64  `json:"fileVersion"`   // 文件版本，记录文件的版本号
				OverridenFlag int    `json:"overridenFlag"` // 覆盖标志，标识是否覆盖已有文件
			} `json:"newContentIDList"`
			CatalogIDList any `json:"catalogIDList"` // 目录ID列表，与上传文件关联的目录信息
			IsSlice       any `json:"isSlice"`       // 是否分片，标识上传是否采用分片方式
		} `json:"uploadResult"`
	} `json:"data"`
}

// InterLayerUploadResult 表示分片上传的 XML 响应结构
// 包含分片上传的结果码和消息
type InterLayerUploadResult struct {
	XMLName    xml.Name `xml:"result"`     // XML 节点名称
	Text       string   `xml:",chardata"`  // 文本内容
	ResultCode int      `xml:"resultCode"` // 结果码，标识分片上传的结果状态
	Msg        string   `xml:"msg"`        // 消息，提供分片上传的额外信息或错误描述
}

// CloudContent 表示家庭云/群组云的文件结构
// 包含文件的基本信息，如 ID、名称、大小、创建时间和更新时间
type CloudContent struct {
	ContentID      string `json:"contentID"`      // 文件ID，唯一标识一个文件
	ContentName    string `json:"contentName"`    // 文件名，用于显示和识别文件
	ContentSize    int64  `json:"contentSize"`    // 文件大小，单位为字节
	CreateTime     string `json:"createTime"`     // 创建时间，记录文件创建的时间戳
	LastUpdateTime string `json:"lastUpdateTime"` // 最后更新时间，记录文件最后更新的时间戳
	ThumbnailURL   string `json:"thumbnailURL"`   // 缩略图URL，用于预览文件的缩略图
}

// CloudCatalog 表示家庭云/群组云的目录结构
// 包含目录的基本信息，如 ID、名称、创建时间和更新时间
type CloudCatalog struct {
	CatalogID      string `json:"catalogID"`      // 目录ID，唯一标识一个目录
	CatalogName    string `json:"catalogName"`    // 目录名称，用于显示和识别目录
	CreateTime     string `json:"createTime"`     // 创建时间，记录目录创建的时间戳
	LastUpdateTime string `json:"lastUpdateTime"` // 最后更新时间，记录目录最后更新的时间戳
}

// QueryContentListResp 表示家庭云内容列表响应
// 包含路径、文件列表、目录列表和总数等信息
type QueryContentListResp struct {
	BaseResp
	Data struct {
		Result struct {
			ResultCode string `json:"resultCode"` // 结果码，标识请求的结果状态
			ResultDesc string `json:"resultDesc"` // 结果描述，提供额外的结果信息
		} `json:"result"`
		Path             string         `json:"path"`             // 路径，当前列表所在的路径
		CloudContentList []CloudContent `json:"cloudContentList"` // 文件列表，包含当前路径下的所有文件
		CloudCatalogList []CloudCatalog `json:"cloudCatalogList"` // 目录列表，包含当前路径下的所有目录
		TotalCount       int            `json:"totalCount"`       // 总数，记录文件和目录的总数
		RecallContent    any            `json:"recallContent"`    // 回收内容，可能是回收站相关信息
	} `json:"data"`
}

// QueryGroupContentListResp 表示群组云内容列表响应
// 包含父目录ID、目录列表、文件列表和节点数量等信息
type QueryGroupContentListResp struct {
	BaseResp
	Data struct {
		Result struct {
			ResultCode string `json:"resultCode"` // 结果码，标识请求的结果状态
			ResultDesc string `json:"resultDesc"` // 结果描述，提供额外的结果信息
		} `json:"result"`
		GetGroupContentResult struct {
			ParentCatalogID string `json:"parentCatalogID"` // 父目录ID，标识当前列表所在的父目录
			CatalogList     []struct {
				Catalog
				Path string `json:"path"` // 路径，目录的具体路径
			} `json:"catalogList"`
			ContentList []Content `json:"contentList"` // 文件列表，包含当前目录下的所有文件
			NodeCount   int       `json:"nodeCount"`   // 节点数量，记录目录和文件的总数
			CtlgCnt     int       `json:"ctlgCnt"`     // 目录数量，记录目录的总数
			ContCnt     int       `json:"contCnt"`     // 文件数量，记录文件的总数
		} `json:"getGroupContentResult"`
	} `json:"data"`
}

// ParallelHashCtx 表示分片上传的哈希上下文
// 包含分片偏移量信息，用于分片上传的校验
type ParallelHashCtx struct {
	PartOffset int64 `json:"partOffset"` // 分片偏移量，记录分片在文件中的起始位置
}

// PartInfo 表示分片上传的分片信息
// 包含分片编号、大小和哈希上下文，用于分片上传管理
type PartInfo struct {
	PartNumber      int64           `json:"partNumber"`      // 分片编号，标识分片的顺序
	PartSize        int64           `json:"partSize"`        // 分片大小，记录分片的大小（字节）
	ParallelHashCtx ParallelHashCtx `json:"parallelHashCtx"` // 分片哈希上下文，包含分片的哈希信息
}

// PersonalThumbnail 表示新版个人云的缩略图信息
// 包含缩略图的样式和URL，用于文件预览
type PersonalThumbnail struct {
	Style string `json:"style"` // 缩略图样式，标识缩略图的类型或尺寸
	URL   string `json:"url"`   // 缩略图URL，用于获取缩略图的地址
}

// PersonalFileItem 表示新版个人云的文件项
// 包含文件的基本信息和缩略图列表
type PersonalFileItem struct {
	FileID     string              `json:"fileId"`        // 文件ID，唯一标识一个文件
	Name       string              `json:"name"`          // 文件名，用于显示和识别文件
	Size       int64               `json:"size"`          // 文件大小，单位为字节
	Type       string              `json:"type"`          // 文件类型，标识文件是文件夹还是文件
	CreatedAt  string              `json:"createdAt"`     // 创建时间，记录文件创建的时间戳
	UpdatedAt  string              `json:"updatedAt"`     // 更新时间，记录文件最后更新的时间戳
	Thumbnails []PersonalThumbnail `json:"thumbnailUrls"` // 缩略图列表，包含文件的缩略图信息
}

// PersonalListResp 表示新版个人云的文件列表响应
// 包含文件项列表和下一页游标，用于分页加载
type PersonalListResp struct {
	BaseResp
	Data struct {
		Items          []PersonalFileItem `json:"items"`          // 文件项列表，包含当前页的文件和目录
		NextPageCursor string             `json:"nextPageCursor"` // 下一页游标，用于获取下一页数据
	}
}

// PersonalPartInfo 表示新版个人云分片上传的分片信息
// 包含分片编号和上传URL，用于分片上传
type PersonalPartInfo struct {
	PartNumber int    `json:"partNumber"` // 分片编号，标识分片的顺序
	UploadURL  string `json:"uploadUrl"`  // 上传URL，用于上传该分片的地址
}

// PersonalUploadResp 表示新版个人云上传响应
// 包含文件ID、文件名、分片信息等上传结果
type PersonalUploadResp struct {
	BaseResp
	Data struct {
		FileID      string             `json:"fileId"`      // 文件ID，上传后分配的唯一标识
		FileName    string             `json:"fileName"`    // 文件名，上传文件的名称
		PartInfos   []PersonalPartInfo `json:"partInfos"`   // 分片信息列表，包含各分片的上传信息
		Exist       bool               `json:"exist"`       // 是否存在，标识文件是否已存在
		RapidUpload bool               `json:"rapidUpload"` // 是否秒传，标识文件是否通过秒传方式上传
		UploadID    string             `json:"uploadId"`    // 上传ID，唯一标识一个上传任务
	}
}

// PersonalUploadURLResp 表示新版个人云获取分片上传 URL 响应
// 包含文件ID、上传ID和分片信息列表
type PersonalUploadURLResp struct {
	BaseResp
	Data struct {
		FileID    string             `json:"fileId"`    // 文件ID，唯一标识一个文件
		UploadID  string             `json:"uploadId"`  // 上传ID，唯一标识一个上传任务
		PartInfos []PersonalPartInfo `json:"partInfos"` // 分片信息列表，包含各分片的上传URL
	}
}

// QueryRoutePolicyResp 表示路由策略查询响应
// 包含路由策略列表，用于确定云盘服务的访问地址
type QueryRoutePolicyResp struct {
	Success bool   `json:"success"` // 请求是否成功，true 表示成功，false 表示失败
	Code    string `json:"code"`    // 状态码，标识请求的具体结果状态
	Message string `json:"message"` // 消息，提供请求的额外信息或错误描述
	Data    struct {
		RoutePolicyList []struct {
			SiteID      string `json:"siteID"`      // 站点ID，唯一标识一个站点
			SiteCode    string `json:"siteCode"`    // 站点代码，标识站点的编码
			ModName     string `json:"modName"`     // 模块名称，标识具体的服务模块
			HTTPURL     string `json:"httpUrl"`     // HTTP URL，用于HTTP协议访问服务
			HTTPSURL    string `json:"httpsUrl"`    // HTTPS URL，用于HTTPS协议访问服务
			EnvID       string `json:"envID"`       // 环境ID，标识服务的环境
			ExtInfo     string `json:"extInfo"`     // 扩展信息，提供额外的服务信息
			HashName    string `json:"hashName"`    // 哈希名称，用于特定场景的哈希标识
			ModAddrType int    `json:"modAddrType"` // 模块地址类型，标识地址的类型或格式
		} `json:"routePolicyList"`
	} `json:"data"`
}

// RefreshTokenResp 表示刷新 token 的 XML 响应结构
// 包含刷新后的 token 信息和过期时间
type RefreshTokenResp struct {
	XMLName     xml.Name `xml:"root"`        // XML 节点名称
	Return      string   `xml:"return"`      // 返回值，标识刷新操作的结果状态
	Token       string   `xml:"token"`       // 新的 token，刷新后的授权令牌
	Expiretime  int32    `xml:"expiretime"`  // 过期时间，记录 token 的有效期
	AccessToken string   `xml:"accessToken"` // 访问 token，可能用于特定场景的访问授权
	Desc        string   `xml:"desc"`        // 描述，提供刷新操作的额外信息或错误描述
}
