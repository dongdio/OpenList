package qbittorrent

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/utility/utils"
)

// Client 定义了与qBittorrent交互的接口
type Client interface {
	// AddFromLink 添加磁力链接或者种子链接到qBittorrent
	AddFromLink(link string, savePath string, id string) error
	// GetInfo 获取特定任务的信息
	GetInfo(id string) (TorrentInfo, error)
	// GetFiles 获取特定任务的文件列表
	GetFiles(id string) ([]FileInfo, error)
	// Delete 删除特定任务，可选择是否同时删除文件
	Delete(id string, deleteFiles bool) error
}

// client 实现了Client接口
type client struct {
	url    *url.URL
	client http.Client
}

// New 创建一个新的qBittorrent客户端
// webuiUrl应该包含WebUI的完整URL，包括用户名和密码
func New(link string) (Client, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse URL")
	}

	// 需要用户信息
	if u.User == nil || u.User.Username() == "" {
		return nil, errors.New("URL必须包含用户名和密码")
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create cookie jar")
	}
	transport := &http.Transport{
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     30 * time.Second,
		DisableKeepAlives:   false, // Enable connection reuse
	}

	var c = &client{
		url: u,
		client: http.Client{
			Jar:       jar,
			Transport: transport,
			Timeout:   30 * time.Second, // Set overall timeout
		},
	}

	err = c.checkAuthorization()
	if err != nil {
		return nil, err
	}
	return c, nil
}

// checkAuthorization 检查客户端是否已授权，如果未授权，则尝试登录
func (c *client) checkAuthorization() error {
	// 检查是否已授权
	if c.authorized() {
		return nil
	}

	// 尝试登录后再次检查授权
	err := c.login()
	if err != nil {
		return errors.Wrapf(err, "failed to login")
	}
	if c.authorized() {
		return nil
	}
	return errors.New("failed to access qBittorrent WebUI, authentication failed")
}

// authorized 判断当前客户端是否有权限访问qBittorrent
func (c *client) authorized() bool {
	code, _, err := c.post("/api/v2/app/version", false, nil)
	if err != nil {
		return false
	}
	return code == 200 // 状态码为403表示未授权
}

// login 使用URL中的用户名和密码登录qBittorrent WebUI
func (c *client) login() error {
	// 准备HTTP请求
	v := url.Values{}
	v.Set("username", c.url.User.Username())

	passwd, _ := c.url.User.Password()
	v.Set("password", passwd)

	_, body, err := c.post("/api/v2/auth/login", true, v)
	if err != nil {
		return err
	}
	// 检查结果
	if !strings.EqualFold(string(body), "ok") {
		return errors.Errorf("failed to login qBittorrent WebUI, URL: %s", c.url.String())
	}
	return nil
}

// post 发送POST请求到qBittorrent WebUI
func (c *client) post(path string, body bool, data url.Values) (int, []byte, error) {
	u := c.url.JoinPath(path)
	u.User = nil // 移除请求中的用户信息

	var reqBody io.Reader
	if data != nil {
		reqBody = bytes.NewReader([]byte(data.Encode()))
	}

	req, err := http.NewRequest("POST", u.String(), reqBody)
	if err != nil {
		return 0, nil, errors.Wrapf(err, "failed to create request")
	}
	if data != nil {
		req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, nil, errors.Wrapf(err, "failed to send request")
	}
	defer resp.Body.Close()
	if resp.Cookies() != nil {
		c.client.Jar.SetCookies(u, resp.Cookies())
	}
	var respBody []byte
	if body {
		// 检查结果
		respBody, err = io.ReadAll(resp.Body)
		if err != nil {
			return 0, nil, errors.Wrapf(err, "failed to read response")
		}
	}
	return resp.StatusCode, respBody, nil
}

// AddFromLink 通过链接添加种子任务
func (c *client) AddFromLink(link string, savePath string, id string) error {
	err := c.checkAuthorization()
	if err != nil {
		return err
	}

	buf := new(bytes.Buffer)
	writer := multipart.NewWriter(buf)

	// 辅助函数用于添加字段并处理错误
	var fieldErr error
	addField := func(name string, value string) {
		if fieldErr != nil {
			return
		}
		fieldErr = writer.WriteField(name, value)
	}

	addField("urls", link)
	addField("savepath", savePath)
	addField("tags", "openlist-"+id)
	addField("autoTMM", "false")
	if fieldErr != nil {
		return errors.Wrapf(fieldErr, "failed to create form field")
	}

	err = writer.Close()
	if err != nil {
		return errors.Wrapf(err, "failed to close form writer")
	}

	u := c.url.JoinPath("/api/v2/torrents/add")
	u.User = nil // 移除请求中的用户信息
	req, err := http.NewRequest("POST", u.String(), buf)
	if err != nil {
		return errors.Wrapf(err, "failed to create request")
	}
	req.Header.Add("Content-Type", writer.FormDataContentType())

	resp, err := c.client.Do(req)
	if err != nil {
		return errors.Wrapf(err, "failed to send request")
	}
	defer func() { _ = resp.Body.Close() }()

	// 检查结果
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrapf(err, "failed to read response")
	}
	if resp.StatusCode != 200 || string(body) != "Ok" {
		return errors.Errorf("failed to add qBittorrent task: %s", link)
	}
	return nil
}

// TorrentStatus 表示种子任务的状态
type TorrentStatus string

// 种子任务的各种状态
const (
	ERROR              TorrentStatus = "error"              // 出错
	MISSINGFILES       TorrentStatus = "missingFiles"       // 文件丢失
	UPLOADING          TorrentStatus = "uploading"          // 上传中
	PAUSEDUP           TorrentStatus = "pausedUP"           // 上传已暂停
	QUEUEDUP           TorrentStatus = "queuedUP"           // 上传已排队
	STALLEDUP          TorrentStatus = "stalledUP"          // 上传已停滞
	CHECKINGUP         TorrentStatus = "checkingUP"         // 上传检查中
	FORCEDUP           TorrentStatus = "forcedUP"           // 强制上传
	ALLOCATING         TorrentStatus = "allocating"         // 分配中
	DOWNLOADING        TorrentStatus = "downloading"        // 下载中
	METADL             TorrentStatus = "metaDL"             // 元数据下载中
	PAUSEDDL           TorrentStatus = "pausedDL"           // 下载已暂停
	QUEUEDDL           TorrentStatus = "queuedDL"           // 下载已排队
	STALLEDDL          TorrentStatus = "stalledDL"          // 下载已停滞
	CHECKINGDL         TorrentStatus = "checkingDL"         // 下载检查中
	FORCEDDL           TorrentStatus = "forcedDL"           // 强制下载
	CHECKINGRESUMEDATA TorrentStatus = "checkingResumeData" // 检查恢复数据
	MOVING             TorrentStatus = "moving"             // 移动中
	UNKNOWN            TorrentStatus = "unknown"            // 未知状态
)

// TorrentInfo 包含种子任务的详细信息
// 参考: https://github.com/qbittorrent/qBittorrent/wiki/WebUI-API-(qBittorrent-4.1)#get-torrent-list
type TorrentInfo struct {
	AddedOn           int           `json:"added_on"`           // 将 torrent 添加到客户端的时间（Unix Epoch）
	AmountLeft        int64         `json:"amount_left"`        // 剩余大小（字节）
	AutoTmm           bool          `json:"auto_tmm"`           // 此 torrent 是否由 Automatic Torrent Management 管理
	Availability      float64       `json:"availability"`       // 当前可用性百分比
	Category          string        `json:"category"`           // 分类
	Completed         int64         `json:"completed"`          // 完成的传输数据量（字节）
	CompletionOn      int           `json:"completion_on"`      // Torrent 完成的时间（Unix Epoch）
	ContentPath       string        `json:"content_path"`       // torrent 内容的绝对路径（多文件 torrent 的根路径，单文件 torrent 的绝对文件路径）
	DlLimit           int           `json:"dl_limit"`           // Torrent 下载速度限制（字节/秒）
	Dlspeed           int           `json:"dlspeed"`            // Torrent 下载速度（字节/秒）
	Downloaded        int64         `json:"downloaded"`         // 已经下载大小
	DownloadedSession int64         `json:"downloaded_session"` // 此会话下载的数据量
	Eta               int           `json:"eta"`                // 预计完成时间（秒）
	FLPiecePrio       bool          `json:"f_l_piece_prio"`     // 如果第一个最后一块被优先考虑，则为true
	ForceStart        bool          `json:"force_start"`        // 如果为此 torrent 启用了强制启动，则为true
	Hash              string        `json:"hash"`               // 种子哈希
	LastActivity      int           `json:"last_activity"`      // 上次活跃的时间（Unix Epoch）
	MagnetURI         string        `json:"magnet_uri"`         // 与此 torrent 对应的 Magnet URI
	MaxRatio          float64       `json:"max_ratio"`          // 种子/上传停止种子前的最大共享比率
	MaxSeedingTime    int           `json:"max_seeding_time"`   // 停止种子种子前的最长种子时间（秒）
	Name              string        `json:"name"`               // 名称
	NumComplete       int           `json:"num_complete"`       // 完成下载的节点数
	NumIncomplete     int           `json:"num_incomplete"`     // 未完成下载的节点数
	NumLeechs         int           `json:"num_leechs"`         // 连接到的 leechers 的数量
	NumSeeds          int           `json:"num_seeds"`          // 连接到的种子数
	Priority          int           `json:"priority"`           // 速度优先。如果队列被禁用或 torrent 处于种子模式，则返回 -1
	Progress          float64       `json:"progress"`           // 进度
	Ratio             float64       `json:"ratio"`              // Torrent 共享比率
	RatioLimit        int           `json:"ratio_limit"`        // 比率限制
	SavePath          string        `json:"save_path"`          // 保存路径
	SeedingTime       int           `json:"seeding_time"`       // Torrent 完成用时（秒）
	SeedingTimeLimit  int           `json:"seeding_time_limit"` // max_seeding_time
	SeenComplete      int           `json:"seen_complete"`      // 上次 torrent 完成的时间
	SeqDl             bool          `json:"seq_dl"`             // 如果启用顺序下载，则为true
	Size              int64         `json:"size"`               // 大小
	State             TorrentStatus `json:"state"`              // 状态
	SuperSeeding      bool          `json:"super_seeding"`      // 如果启用超级播种，则为true
	Tags              string        `json:"tags"`               // Torrent 的逗号连接标签列表
	TimeActive        int           `json:"time_active"`        // 总活动时间（秒）
	TotalSize         int64         `json:"total_size"`         // 此 torrent 中所有文件的总大小（字节）（包括未选择的文件）
	Tracker           string        `json:"tracker"`            // 第一个具有工作状态的tracker。如果没有tracker在工作，则返回空字符串。
	TrackersCount     int           `json:"trackers_count"`     // tracker数量
	UpLimit           int           `json:"up_limit"`           // 上传限制
	Uploaded          int64         `json:"uploaded"`           // 累计上传
	UploadedSession   int64         `json:"uploaded_session"`   // 当前session累计上传
	Upspeed           int           `json:"upspeed"`            // 上传速度（字节/秒）
}

// InfoNotFoundError 当根据ID找不到对应的任务时返回
type InfoNotFoundError struct {
	ID  string
	Err error
}

// Error 实现error接口
func (i InfoNotFoundError) Error() string {
	return fmt.Sprintf("找不到标签为\"openlist-%s\"的任务", i.ID)
}

// NewInfoNotFoundError 创建一个新的InfoNotFoundError
func NewInfoNotFoundError(id string) InfoNotFoundError {
	return InfoNotFoundError{ID: id}
}

// GetInfo 根据ID获取种子任务信息
func (c *client) GetInfo(id string) (TorrentInfo, error) {
	var infos []TorrentInfo

	err := c.checkAuthorization()
	if err != nil {
		return TorrentInfo{}, err
	}

	v := url.Values{}
	v.Set("tag", "openlist-"+id)
	_, body, err := c.post("/api/v2/torrents/info", true, v)
	if err != nil {
		return TorrentInfo{}, errors.Wrap(err, "failed to get torrent info")
	}

	err = utils.JSONTool.Unmarshal(body, &infos)
	if err != nil {
		return TorrentInfo{}, errors.Wrap(err, "failed to parse JSON")
	}
	if len(infos) != 1 {
		return TorrentInfo{}, NewInfoNotFoundError(id)
	}
	return infos[0], nil
}

// FileInfo 表示种子任务中的文件信息
type FileInfo struct {
	Index        int     `json:"index"`        // 文件索引
	Name         string  `json:"name"`         // 文件名
	Size         int64   `json:"size"`         // 文件大小
	Progress     float32 `json:"progress"`     // 下载进度
	Priority     int     `json:"priority"`     // 优先级
	IsSeed       bool    `json:"is_seed"`      // 是否做种
	PieceRange   []int   `json:"piece_range"`  // 分片范围
	Availability float32 `json:"availability"` // 可用性
}

// GetFiles 获取种子任务的文件列表
func (c *client) GetFiles(id string) ([]FileInfo, error) {
	var infos []FileInfo

	err := c.checkAuthorization()
	if err != nil {
		return []FileInfo{}, err
	}

	tInfo, err := c.GetInfo(id)
	if err != nil {
		return []FileInfo{}, err
	}

	v := url.Values{}
	v.Set("hash", tInfo.Hash)
	_, body, err := c.post("/api/v2/torrents/files", true, v)
	if err != nil {
		return []FileInfo{}, errors.Wrap(err, "failed to get file list")
	}

	err = utils.JSONTool.Unmarshal(body, &infos)
	return infos, errors.Wrap(err, "failed to parse JSON")
}

// Delete 删除种子任务
func (c *client) Delete(id string, deleteFiles bool) error {
	err := c.checkAuthorization()
	if err != nil {
		return err
	}

	info, err := c.GetInfo(id)
	if err != nil {
		return err
	}

	v := url.Values{}
	v.Set("hashes", info.Hash)
	v.Set("deleteFiles", fmt.Sprintf("%t", deleteFiles))

	deleteCode, _, err := c.post("/api/v2/torrents/delete", false, v)
	if err != nil {
		return errors.Wrapf(err, "failed to delete torrent")
	}
	if deleteCode != 200 {
		return errors.New("failed to delete qBittorrent task")
	}

	v = url.Values{}
	v.Set("tags", "openlist-"+id)
	deleteTagsCode, _, err := c.post("/api/v2/torrents/deleteTags", false, v)
	if err != nil {
		return errors.Wrapf(err, "failed to delete tag")
	}
	if deleteTagsCode != 200 {
		return errors.New("failed to delete qBittorrent tag")
	}
	return nil
}