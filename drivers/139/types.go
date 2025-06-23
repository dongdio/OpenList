package _139

import (
	"encoding/xml"
)

const (
	MetaPersonal    string = "personal"
	MetaFamily      string = "family"
	MetaGroup       string = "group"
	MetaPersonalNew string = "personal_new"
)

type BaseResp struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Catalog struct {
	CatalogID   string `json:"catalogID"`
	CatalogName string `json:"catalogName"`
	// CatalogType     int         `json:"catalogType"`
	CreateTime string `json:"createTime"`
	UpdateTime string `json:"updateTime"`
	// IsShared        bool        `json:"isShared"`
	// CatalogLevel    int         `json:"catalogLevel"`
	// ShareDoneeCount int         `json:"shareDoneeCount"`
	// OpenType        int         `json:"openType"`
	// ParentCatalogID string      `json:"parentCatalogId"`
	// DirEtag         int         `json:"dirEtag"`
	// Tombstoned      int         `json:"tombstoned"`
	// ProxyID         any `json:"proxyID"`
	// Moved           int         `json:"moved"`
	// IsFixedDir      int         `json:"isFixedDir"`
	// IsSynced        any `json:"isSynced"`
	// Owner           string      `json:"owner"`
	// Modifier        any `json:"modifier"`
	// Path            string      `json:"path"`
	// ShareType       int         `json:"shareType"`
	// SoftLink        any `json:"softLink"`
	// ExtProp1        any `json:"extProp1"`
	// ExtProp2        any `json:"extProp2"`
	// ExtProp3        any `json:"extProp3"`
	// ExtProp4        any `json:"extProp4"`
	// ExtProp5        any `json:"extProp5"`
	// ETagOprType     int         `json:"ETagOprType"`
}

type Content struct {
	ContentID   string `json:"contentID"`
	ContentName string `json:"contentName"`
	// ContentSuffix   string      `json:"contentSuffix"`
	ContentSize int64 `json:"contentSize"`
	// ContentDesc     string      `json:"contentDesc"`
	// ContentType     int         `json:"contentType"`
	// ContentOrigin   int         `json:"contentOrigin"`
	CreateTime string `json:"createTime"`
	UpdateTime string `json:"updateTime"`
	// CommentCount    int         `json:"commentCount"`
	ThumbnailURL string `json:"thumbnailURL"`
	// BigthumbnailURL string      `json:"bigthumbnailURL"`
	// PresentURL      string      `json:"presentURL"`
	// PresentLURL     string      `json:"presentLURL"`
	// PresentHURL     string      `json:"presentHURL"`
	// ContentTAGList  any `json:"contentTAGList"`
	// ShareDoneeCount int         `json:"shareDoneeCount"`
	// Safestate       int         `json:"safestate"`
	// Transferstate   int         `json:"transferstate"`
	// IsFocusContent  int         `json:"isFocusContent"`
	// UpdateShareTime any `json:"updateShareTime"`
	// UploadTime      string      `json:"uploadTime"`
	// OpenType        int         `json:"openType"`
	// AuditResult     int         `json:"auditResult"`
	// ParentCatalogID string      `json:"parentCatalogId"`
	// Channel         string      `json:"channel"`
	// GeoLocFlag      string      `json:"geoLocFlag"`
	Digest string `json:"digest"`
	// Version         string      `json:"version"`
	// FileEtag        string      `json:"fileEtag"`
	// FileVersion     string      `json:"fileVersion"`
	// Tombstoned      int         `json:"tombstoned"`
	// ProxyID         string      `json:"proxyID"`
	// Moved           int         `json:"moved"`
	// MidthumbnailURL string      `json:"midthumbnailURL"`
	// Owner           string      `json:"owner"`
	// Modifier        string      `json:"modifier"`
	// ShareType       int         `json:"shareType"`
	// ExtInfo         struct {
	//	Uploader string `json:"uploader"`
	//	Address  string `json:"address"`
	// } `json:"extInfo"`
	// Exif struct {
	//	CreateTime    string      `json:"createTime"`
	//	Longitude     any `json:"longitude"`
	//	Latitude      any `json:"latitude"`
	//	LocalSaveTime any `json:"localSaveTime"`
	// } `json:"exif"`
	// CollectionFlag any `json:"collectionFlag"`
	// TreeInfo       any `json:"treeInfo"`
	// IsShared       bool        `json:"isShared"`
	// ETagOprType    int         `json:"ETagOprType"`
}

type GetDiskResp struct {
	BaseResp
	Data struct {
		Result struct {
			ResultCode string `json:"resultCode"`
			ResultDesc any    `json:"resultDesc"`
		} `json:"result"`
		GetDiskResult struct {
			ParentCatalogID string    `json:"parentCatalogID"`
			NodeCount       int       `json:"nodeCount"`
			CatalogList     []Catalog `json:"catalogList"`
			ContentList     []Content `json:"contentList"`
			IsCompleted     int       `json:"isCompleted"`
		} `json:"getDiskResult"`
	} `json:"data"`
}

type UploadResp struct {
	BaseResp
	Data struct {
		Result struct {
			ResultCode string `json:"resultCode"`
			ResultDesc any    `json:"resultDesc"`
		} `json:"result"`
		UploadResult struct {
			UploadTaskID     string `json:"uploadTaskID"`
			RedirectionURL   string `json:"redirectionUrl"`
			NewContentIDList []struct {
				ContentID     string `json:"contentID"`
				ContentName   string `json:"contentName"`
				IsNeedUpload  string `json:"isNeedUpload"`
				FileEtag      int64  `json:"fileEtag"`
				FileVersion   int64  `json:"fileVersion"`
				OverridenFlag int    `json:"overridenFlag"`
			} `json:"newContentIDList"`
			CatalogIDList any `json:"catalogIDList"`
			IsSlice       any `json:"isSlice"`
		} `json:"uploadResult"`
	} `json:"data"`
}

type InterLayerUploadResult struct {
	XMLName    xml.Name `xml:"result"`
	Text       string   `xml:",chardata"`
	ResultCode int      `xml:"resultCode"`
	Msg        string   `xml:"msg"`
}

type CloudContent struct {
	ContentID string `json:"contentID"`
	// Modifier         string      `json:"modifier"`
	// Nickname         string      `json:"nickname"`
	// CloudNickName    string      `json:"cloudNickName"`
	ContentName string `json:"contentName"`
	// ContentType      int         `json:"contentType"`
	// ContentSuffix    string      `json:"contentSuffix"`
	ContentSize int64 `json:"contentSize"`
	// ContentDesc      string      `json:"contentDesc"`
	CreateTime string `json:"createTime"`
	// Shottime         any `json:"shottime"`
	LastUpdateTime string `json:"lastUpdateTime"`
	ThumbnailURL   string `json:"thumbnailURL"`
	// MidthumbnailURL  string      `json:"midthumbnailURL"`
	// BigthumbnailURL  string      `json:"bigthumbnailURL"`
	// PresentURL       string      `json:"presentURL"`
	// PresentLURL      string      `json:"presentLURL"`
	// PresentHURL      string      `json:"presentHURL"`
	// ParentCatalogID  string      `json:"parentCatalogID"`
	// Uploader         string      `json:"uploader"`
	// UploaderNickName string      `json:"uploaderNickName"`
	// TreeInfo         any `json:"treeInfo"`
	// UpdateTime       any `json:"updateTime"`
	// ExtInfo          struct {
	//	Uploader string `json:"uploader"`
	// } `json:"extInfo"`
	// EtagOprType any `json:"etagOprType"`
}

type CloudCatalog struct {
	CatalogID   string `json:"catalogID"`
	CatalogName string `json:"catalogName"`
	// CloudID         string `json:"cloudID"`
	CreateTime     string `json:"createTime"`
	LastUpdateTime string `json:"lastUpdateTime"`
	// Creator         string `json:"creator"`
	// CreatorNickname string `json:"creatorNickname"`
}

type QueryContentListResp struct {
	BaseResp
	Data struct {
		Result struct {
			ResultCode string `json:"resultCode"`
			ResultDesc string `json:"resultDesc"`
		} `json:"result"`
		Path             string         `json:"path"`
		CloudContentList []CloudContent `json:"cloudContentList"`
		CloudCatalogList []CloudCatalog `json:"cloudCatalogList"`
		TotalCount       int            `json:"totalCount"`
		RecallContent    any            `json:"recallContent"`
	} `json:"data"`
}

type QueryGroupContentListResp struct {
	BaseResp
	Data struct {
		Result struct {
			ResultCode string `json:"resultCode"`
			ResultDesc string `json:"resultDesc"`
		} `json:"result"`
		GetGroupContentResult struct {
			ParentCatalogID string `json:"parentCatalogID"` // 根目录是"0"
			CatalogList     []struct {
				Catalog
				Path string `json:"path"`
			} `json:"catalogList"`
			ContentList []Content `json:"contentList"`
			NodeCount   int       `json:"nodeCount"` // 文件+文件夹数量
			CtlgCnt     int       `json:"ctlgCnt"`   // 文件夹数量
			ContCnt     int       `json:"contCnt"`   // 文件数量
		} `json:"getGroupContentResult"`
	} `json:"data"`
}

type ParallelHashCtx struct {
	PartOffset int64 `json:"partOffset"`
}

type PartInfo struct {
	PartNumber      int64           `json:"partNumber"`
	PartSize        int64           `json:"partSize"`
	ParallelHashCtx ParallelHashCtx `json:"parallelHashCtx"`
}

type PersonalThumbnail struct {
	Style string `json:"style"`
	Url   string `json:"url"`
}

type PersonalFileItem struct {
	FileId     string              `json:"fileId"`
	Name       string              `json:"name"`
	Size       int64               `json:"size"`
	Type       string              `json:"type"`
	CreatedAt  string              `json:"createdAt"`
	UpdatedAt  string              `json:"updatedAt"`
	Thumbnails []PersonalThumbnail `json:"thumbnailUrls"`
}

type PersonalListResp struct {
	BaseResp
	Data struct {
		Items          []PersonalFileItem `json:"items"`
		NextPageCursor string             `json:"nextPageCursor"`
	}
}

type PersonalPartInfo struct {
	PartNumber int    `json:"partNumber"`
	UploadUrl  string `json:"uploadUrl"`
}

type PersonalUploadResp struct {
	BaseResp
	Data struct {
		FileId      string             `json:"fileId"`
		FileName    string             `json:"fileName"`
		PartInfos   []PersonalPartInfo `json:"partInfos"`
		Exist       bool               `json:"exist"`
		RapidUpload bool               `json:"rapidUpload"`
		UploadId    string             `json:"uploadId"`
	}
}

type PersonalUploadUrlResp struct {
	BaseResp
	Data struct {
		FileId    string             `json:"fileId"`
		UploadId  string             `json:"uploadId"`
		PartInfos []PersonalPartInfo `json:"partInfos"`
	}
}

type QueryRoutePolicyResp struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    struct {
		RoutePolicyList []struct {
			SiteID      string `json:"siteID"`
			SiteCode    string `json:"siteCode"`
			ModName     string `json:"modName"`
			HttpUrl     string `json:"httpUrl"`
			HttpsUrl    string `json:"httpsUrl"`
			EnvID       string `json:"envID"`
			ExtInfo     string `json:"extInfo"`
			HashName    string `json:"hashName"`
			ModAddrType int    `json:"modAddrType"`
		} `json:"routePolicyList"`
	} `json:"data"`
}

type RefreshTokenResp struct {
	XMLName     xml.Name `xml:"root"`
	Return      string   `xml:"return"`
	Token       string   `xml:"token"`
	Expiretime  int32    `xml:"expiretime"`
	AccessToken string   `xml:"accessToken"`
	Desc        string   `xml:"desc"`
}