package transmission

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/hekmon/transmissionrpc/v3"
	log "github.com/sirupsen/logrus"

	"github.com/dongdio/OpenList/v4/utility/errs"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/offline_download/tool"
	"github.com/dongdio/OpenList/v4/internal/setting"
)

// 错误类型定义
var (
	ErrClientNotInitialized = errs.New("transmission client not initialized")
	ErrInvalidURL           = errs.New("invalid URL format")
	ErrInvalidTorrentID     = errs.New("invalid torrent ID")
	ErrTorrentNotFound      = errs.New("torrent not found")
)

// 常量定义
const (
	defaultTimeout = 30 * time.Second
	bufferSize     = 32 * 1024 // 32KB 缓冲区
)

// 缓冲区池，用于减少内存分配
var bufferPool = sync.Pool{
	New: func() any {
		buffer := make([]byte, bufferSize)
		return &buffer
	},
}

// Transmission 实现了 BitTorrent 客户端接口
type Transmission struct {
	client *transmissionrpc.Client
	mutex  sync.RWMutex // 保护 client 字段的并发访问
}

// Run 实现了 tool.Tool 接口，但 Transmission 不支持此方法
func (t *Transmission) Run(task *tool.DownloadTask) error {
	return errs.NotSupport
}

// Name 返回下载工具的名称
func (t *Transmission) Name() string {
	return "Transmission"
}

// Items 返回 Transmission 客户端所需的配置项
func (t *Transmission) Items() []model.SettingItem {
	return []model.SettingItem{
		{
			Key:   consts.TransmissionUri,
			Value: "http://localhost:9091/transmission/rpc",
			Type:  consts.TypeString,
			Group: model.OFFLINE_DOWNLOAD,
			Flag:  model.PRIVATE,
		},
		{
			Key:   consts.TransmissionSeedtime,
			Value: "0",
			Type:  consts.TypeNumber,
			Group: model.OFFLINE_DOWNLOAD,
			Flag:  model.PRIVATE,
		},
	}
}

// Init 初始化 Transmission 客户端连接
// 返回版本信息和可能的错误
func (t *Transmission) Init() (string, error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	// 重置客户端
	t.client = nil

	// 获取配置的 URI
	uri := setting.GetStr(consts.TransmissionUri)
	if uri == "" {
		return "", errs.New("transmission URI is empty")
	}

	// 解析 URI
	endpoint, err := url.Parse(uri)
	if err != nil {
		return "", errs.Wrap(err, "failed to parse transmission URI")
	}

	// 创建 Transmission 客户端
	// transmissionrpc 库接受 nil 作为默认配置
	c, err := transmissionrpc.New(endpoint, nil)
	if err != nil {
		return "", errs.Wrap(err, "failed to create transmission client")
	}

	// 检查 RPC 版本兼容性
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	ok, serverVersion, serverMinimumVersion, err := c.RPCVersion(ctx)
	if err != nil {
		return "", errs.Wrap(err, "failed to get transmission RPC version")
	}

	if !ok {
		return "", errs.Errorf("transmission RPC version (v%d) is incompatible with library (v%d): requires at least v%d",
			serverVersion, transmissionrpc.RPCVersion, serverMinimumVersion)
	}

	// 设置客户端
	t.client = c

	log.WithFields(log.Fields{
		"server_version":  serverVersion,
		"library_version": transmissionrpc.RPCVersion,
	}).Info("transmission client initialized successfully")

	return fmt.Sprintf("transmission version: %d", serverVersion), nil
}

// IsReady 检查客户端是否已初始化
func (t *Transmission) IsReady() bool {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	return t.client != nil
}

// getClient 安全地获取客户端实例，如果未初始化则返回错误
func (t *Transmission) getClient() (*transmissionrpc.Client, error) {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	if t.client == nil {
		return nil, ErrClientNotInitialized
	}
	return t.client, nil
}

// AddURL 添加一个下载任务
// 支持磁力链接和 HTTP/HTTPS .torrent 文件
func (t *Transmission) AddURL(args *tool.AddURLLinkArgs) (string, error) {
	if args == nil {
		return "", errs.New("add URL arguments cannot be nil")
	}

	if args.URL == "" {
		return "", errs.New("download URL cannot be empty")
	}

	client, err := t.getClient()
	if err != nil {
		return "", err
	}

	// 解析 URL
	endpoint, err := url.Parse(args.URL)
	if err != nil {
		return "", errs.Wrap(err, "failed to parse download URL")
	}

	// 准备 RPC 请求参数
	rpcPayload := transmissionrpc.TorrentAddPayload{
		DownloadDir: &args.TempDir,
	}

	// 处理 HTTP/HTTPS .torrent 文件
	if endpoint.Scheme == "http" || endpoint.Scheme == "https" {
		// 创建带超时的 HTTP 请求
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, args.URL, nil)
		if err != nil {
			return "", errs.Wrap(err, "failed to create HTTP request")
		}

		// 发送请求
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", errs.Wrap(err, "failed to download .torrent file")
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return "", errs.Errorf("failed to download .torrent file: HTTP status %d", resp.StatusCode)
		}

		// 从缓冲区池获取缓冲区
		bufPtr := bufferPool.Get().(*[]byte)
		defer bufferPool.Put(bufPtr)

		// 创建 base64 编码缓冲区
		buffer := new(bytes.Buffer)
		encoder := base64.NewEncoder(base64.StdEncoding, buffer)

		// 流式复制文件内容到编码器
		if _, err = io.CopyBuffer(encoder, resp.Body, *bufPtr); err != nil {
			return "", errs.Wrap(err, "failed to copy file content to base64 encoder")
		}

		// 刷新最后的字节
		if err = encoder.Close(); err != nil {
			return "", errs.Wrap(err, "failed to flush base64 encoder")
		}

		// 获取 base64 编码的字符串
		b64 := buffer.String()
		rpcPayload.MetaInfo = &b64
	} else {
		// 处理磁力链接
		rpcPayload.Filename = &args.URL
	}

	// 添加种子
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	torrent, err := client.TorrentAdd(ctx, rpcPayload)
	if err != nil {
		return "", errs.Wrap(err, "failed to add torrent")
	}

	if torrent.ID == nil {
		return "", errs.New("failed to get torrent ID")
	}

	// 转换 ID 为字符串
	gid := strconv.FormatInt(*torrent.ID, 10)
	log.WithField("gid", gid).Info("torrent added successfully")

	return gid, nil
}

// Remove 删除一个下载任务
func (t *Transmission) Remove(task *tool.DownloadTask) error {
	if task == nil {
		return errs.New("download task cannot be nil")
	}

	client, err := t.getClient()
	if err != nil {
		return err
	}

	// 解析种子 ID
	gid, err := strconv.ParseInt(task.GID, 10, 64)
	if err != nil {
		return errs.Wrapf(err, "invalid torrent ID: %s", task.GID)
	}

	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	// 删除种子但保留下载的数据
	err = client.TorrentRemove(ctx, transmissionrpc.TorrentRemovePayload{
		IDs:             []int64{gid},
		DeleteLocalData: false,
	})

	if err != nil {
		return errs.Wrapf(err, "failed to remove torrent: %s", task.GID)
	}

	log.WithField("gid", task.GID).Info("torrent removed successfully")
	return nil
}

// Status 获取下载任务的状态
func (t *Transmission) Status(task *tool.DownloadTask) (*tool.Status, error) {
	if task == nil {
		return nil, errs.New("download task cannot be nil")
	}

	client, err := t.getClient()
	if err != nil {
		return nil, err
	}

	// 解析种子 ID
	gid, err := strconv.ParseInt(task.GID, 10, 64)
	if err != nil {
		return nil, errs.Wrapf(err, "invalid torrent ID: %s", task.GID)
	}

	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	// 获取种子信息
	infos, err := client.TorrentGetAllFor(ctx, []int64{gid})
	if err != nil {
		return nil, errs.Wrapf(err, "failed to get torrent status: %s", task.GID)
	}

	if len(infos) < 1 {
		return nil, errs.Wrapf(ErrTorrentNotFound, "torrent not found: %s", task.GID)
	}

	info := infos[0]

	// 创建状态对象
	status := &tool.Status{
		Completed: *info.IsFinished,
		Err:       nil,
	}

	// 设置进度信息
	status.Progress = *info.PercentDone * 100
	status.TotalBytes = int64(*info.SizeWhenDone / 8)

	// 根据种子状态设置下载状态
	switch *info.Status {
	case transmissionrpc.TorrentStatusCheckWait,
		transmissionrpc.TorrentStatusDownloadWait,
		transmissionrpc.TorrentStatusCheck,
		transmissionrpc.TorrentStatusDownload,
		transmissionrpc.TorrentStatusIsolated:
		status.Status = "[transmission] " + info.Status.String()
	case transmissionrpc.TorrentStatusSeedWait,
		transmissionrpc.TorrentStatusSeed:
		status.Completed = true
	case transmissionrpc.TorrentStatusStopped:
		errMsg := "unknown error"
		if info.ErrorString != nil {
			errMsg = *info.ErrorString
		}
		status.Err = errs.Errorf("[transmission] download failed: %s, status: %s, error: %s",
			task.GID, info.Status.String(), errMsg)
	default:
		errMsg := "unknown error"
		if info.ErrorString != nil {
			errMsg = *info.ErrorString
		}
		status.Err = errs.Errorf("[transmission] unknown status: %s, error: %s",
			info.Status.String(), errMsg)
	}

	return status, nil
}

// 确保 Transmission 实现了 tool.Tool 接口
var _ tool.Tool = (*Transmission)(nil)

// 初始化函数，注册 Transmission 工具
func init() {
	tool.Tools.Add(&Transmission{
		mutex: sync.RWMutex{},
	})
}