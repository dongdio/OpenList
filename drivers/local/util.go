package local

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/disintegration/imaging"
	ffmpeg "github.com/u2takey/ffmpeg-go"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// 缩略图相关常量
const (
	thumbPrefix   = "openlist_thumb_" // 缩略图文件名前缀
	thumbWidth    = 144               // 缩略图宽度
	thumbFileMode = 0666              // 缩略图文件权限
)

// 缓存视频探针结果，避免重复探测
var (
	videoProbeCache     = make(map[string]videoProbeResult)
	videoProbeCache_mtx sync.RWMutex
)

// 视频探针结果结构
type videoProbeResult struct {
	duration float64
	err      error
}

// isSymlinkDir 判断文件是否为指向目录的符号链接
// 参数:
//   - f: 文件信息
//   - path: 文件所在目录的路径
//
// 返回:
//   - bool: 如果是指向目录的符号链接则返回true，否则返回false
func isSymlinkDir(f fs.FileInfo, path string) bool {
	// 检查是否为符号链接
	if f.Mode()&os.ModeSymlink != os.ModeSymlink {
		return false
	}
	// 读取符号链接的目标路径
	linkPath := filepath.Join(path, f.Name())
	dst, err := os.Readlink(linkPath)
	if err != nil {
		return false
	}

	// 如果是相对路径，转换为绝对路径
	if !filepath.IsAbs(dst) {
		dst = filepath.Join(path, dst)
	}

	// 获取目标的文件信息
	stat, err := os.Stat(dst)
	if err != nil {
		return false
	}

	// 判断目标是否为目录
	return stat.IsDir()
}

// GetSnapshot 获取视频的缩略图快照
// 参数:
//   - ctx: 上下文，用于取消操作
//   - videoPath: 视频文件的完整路径
//
// 返回:
//   - *bytes.Buffer: 缩略图数据
//   - error: 错误信息
func (d *Local) GetSnapshot(ctx context.Context, videoPath string) (imgData *bytes.Buffer, err error) {
	// 检查缓存中是否有视频时长信息
	var totalDuration float64
	videoProbeCache_mtx.RLock()
	if probe, ok := videoProbeCache[videoPath]; ok {
		if probe.err != nil {
			videoProbeCache_mtx.RUnlock()
			return nil, probe.err
		}
		totalDuration = probe.duration
		videoProbeCache_mtx.RUnlock()
	} else {
		videoProbeCache_mtx.RUnlock()

		// 使用ffprobe获取视频时长
		jsonOutput, err := ffmpeg.Probe(videoPath)
		if err != nil {
			// 缓存错误结果
			videoProbeCache_mtx.Lock()
			videoProbeCache[videoPath] = videoProbeResult{err: fmt.Errorf("ffprobe执行失败: %w", err)}
			videoProbeCache_mtx.Unlock()
			return nil, fmt.Errorf("ffprobe执行失败: %w", err)
		}

		// 解析JSON输出，获取视频时长
		type probeFormat struct {
			Duration string `json:"duration"`
		}
		type probeData struct {
			Format probeFormat `json:"format"`
		}
		var probe probeData
		err = utils.JSONTool.Unmarshal([]byte(jsonOutput), &probe)
		if err != nil {
			// 缓存错误结果
			videoProbeCache_mtx.Lock()
			videoProbeCache[videoPath] = videoProbeResult{err: fmt.Errorf("解析ffprobe输出失败: %w", err)}
			videoProbeCache_mtx.Unlock()
			return nil, fmt.Errorf("解析ffprobe输出失败: %w", err)
		}

		totalDuration, err = strconv.ParseFloat(probe.Format.Duration, 64)
		if err != nil {
			// 缓存错误结果
			videoProbeCache_mtx.Lock()
			videoProbeCache[videoPath] = videoProbeResult{err: fmt.Errorf("解析视频时长失败: %w", err)}
			videoProbeCache_mtx.Unlock()
			return nil, fmt.Errorf("解析视频时长失败: %w", err)
		}

		// 缓存成功结果
		videoProbeCache_mtx.Lock()
		videoProbeCache[videoPath] = videoProbeResult{duration: totalDuration}
		videoProbeCache_mtx.Unlock()
	}

	// 计算截图时间点
	var seekPosition string
	if d.videoThumbPosIsPercentage {
		// 按百分比计算时间点
		seekPosition = fmt.Sprintf("%.3f", totalDuration*d.videoThumbPos)
	} else {
		// 使用指定的时间点，如果超过视频长度则使用视频末尾
		if d.videoThumbPos > totalDuration {
			seekPosition = fmt.Sprintf("%.3f", totalDuration*0.5) // 如果超出范围，使用视频中点
		} else {
			seekPosition = fmt.Sprintf("%.3f", d.videoThumbPos)
		}
	}

	// 使用ffmpeg提取视频帧
	outputBuffer := bytes.NewBuffer(nil)
	// 使用noaccurate_seek选项加速定位并避免错误
	// 当定位点到视频结尾的时间小于一帧时长时，ffmpeg可能无法提取帧并报错
	stream := ffmpeg.Input(videoPath, ffmpeg.KwArgs{
		"ss":              seekPosition,
		"noaccurate_seek": "",
	}).
		Output("pipe:", ffmpeg.KwArgs{
			"vframes": 1,        // 只提取一帧
			"format":  "image2", // 输出为图片
			"vcodec":  "mjpeg",  // 使用MJPEG编码
		}).
		GlobalArgs("-loglevel", "error"). // 只输出错误信息
		Silent(true).                     // 静默模式
		WithOutput(outputBuffer, os.Stdout)

	if err = stream.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg执行失败: %w", err)
	}

	return outputBuffer, nil
}

// readDir 读取目录内容并按名称排序
// 参数:
//   - dirname: 要读取的目录路径
//
// 返回:
//   - []fs.FileInfo: 排序后的文件信息列表
//   - error: 错误信息
func readDir(dirname string) ([]fs.FileInfo, error) {
	// 打开目录
	dirHandle, err := os.Open(dirname)
	if err != nil {
		return nil, fmt.Errorf("无法打开目录 '%s': %w", dirname, err)
	}
	defer dirHandle.Close()

	// 读取目录内容
	fileList, err := dirHandle.Readdir(-1) // -1表示读取所有条目
	if err != nil {
		return nil, fmt.Errorf("无法读取目录内容 '%s': %w", dirname, err)
	}

	// 按文件名排序
	sort.Slice(fileList, func(i, j int) bool {
		return fileList[i].Name() < fileList[j].Name()
	})

	return fileList, nil
}

// getThumb 获取文件的缩略图
// 参数:
//   - file: 文件对象
//
// 返回:
//   - *bytes.Buffer: 缩略图数据
//   - *string: 缩略图路径（如果使用缓存）
//   - error: 错误信息
func (d *Local) getThumb(file model.Obj) (*bytes.Buffer, *string, error) {
	filePath := file.GetPath()
	fileName := file.GetName()

	// 如果文件本身就是缩略图，直接返回
	if strings.HasPrefix(fileName, thumbPrefix) {
		return nil, &filePath, nil
	}

	// 生成缩略图文件名
	thumbName := thumbPrefix + utils.GetMD5EncodeStr(filePath) + ".png"

	// 检查缓存目录是否配置
	if d.ThumbCacheFolder != "" {
		// 检查缓存中是否已存在缩略图
		thumbPath := filepath.Join(d.ThumbCacheFolder, thumbName)
		if utils.Exists(thumbPath) {
			return nil, &thumbPath, nil
		}
	}

	// 根据文件类型获取源数据
	var sourceBuffer *bytes.Buffer
	fileType := utils.GetFileType(fileName)

	if fileType == consts.VIDEO {
		// 视频文件，获取视频快照
		ctx := context.Background() // 使用背景上下文，不设置超时
		videoBuf, err := d.GetSnapshot(ctx, filePath)
		if err != nil {
			return nil, nil, fmt.Errorf("获取视频快照失败: %w", err)
		}
		sourceBuffer = videoBuf
	} else {
		// 图片文件，直接读取
		imgData, err := os.ReadFile(filePath)
		if err != nil {
			return nil, nil, fmt.Errorf("读取图片文件失败: %w", err)
		}
		sourceBuffer = bytes.NewBuffer(imgData)
	}

	// 解码图像，自动处理方向信息
	image, err := imaging.Decode(sourceBuffer, imaging.AutoOrientation(true))
	if err != nil {
		return nil, nil, fmt.Errorf("解码图像失败: %w", err)
	}

	// 调整图像大小，保持宽高比
	thumbImg := imaging.Resize(image, thumbWidth, 0, imaging.Lanczos)

	// 编码为PNG格式，使用配置的质量
	var resultBuffer bytes.Buffer
	quality := d.ThumbQuality
	if quality < 1 || quality > 100 {
		quality = 85 // 使用默认质量
	}

	err = imaging.Encode(&resultBuffer, thumbImg, imaging.PNG, imaging.JPEGQuality(quality))
	if err != nil {
		return nil, nil, fmt.Errorf("编码缩略图失败: %w", err)
	}

	// 如果配置了缓存目录，保存缩略图
	if d.ThumbCacheFolder != "" {
		cachePath := filepath.Join(d.ThumbCacheFolder, thumbName)
		err = os.WriteFile(cachePath, resultBuffer.Bytes(), thumbFileMode)
		if err != nil {
			// 缓存失败不影响返回结果，只是不能缓存
			// 可以记录日志，但继续返回生成的缩略图
		} else {
			// 缓存成功，返回缓存路径
			return nil, &cachePath, nil
		}
	}

	return &resultBuffer, nil, nil
}

// ClearThumbCache 清除特定文件的缩略图缓存
// 参数:
//   - filePath: 原始文件路径
//
// 返回:
//   - bool: 是否成功清除缓存
//   - error: 错误信息
func (d *Local) ClearThumbCache(filePath string) (bool, error) {
	if d.ThumbCacheFolder == "" {
		return false, nil // 未配置缓存目录
	}

	thumbName := thumbPrefix + utils.GetMD5EncodeStr(filePath) + ".png"
	thumbPath := filepath.Join(d.ThumbCacheFolder, thumbName)

	if !utils.Exists(thumbPath) {
		return false, nil // 缓存不存在
	}

	err := os.Remove(thumbPath)
	if err != nil {
		return false, fmt.Errorf("删除缩略图缓存失败: %w", err)
	}

	return true, nil
}

// CleanThumbCache 清理所有过期的缩略图缓存
// 参数:
//   - maxAge: 最大缓存时间（秒）
//
// 返回:
//   - int: 清理的文件数量
//   - error: 错误信息
func (d *Local) CleanThumbCache(maxAge int64) (int, error) {
	if d.ThumbCacheFolder == "" {
		return 0, nil // 未配置缓存目录
	}

	now := time.Now()
	count := 0

	err := filepath.Walk(d.ThumbCacheFolder, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 跳过目录
		if info.IsDir() {
			return nil
		}

		// 只处理缩略图文件
		if !strings.HasPrefix(info.Name(), thumbPrefix) {
			return nil
		}

		// 检查文件修改时间
		if now.Sub(info.ModTime()).Seconds() > float64(maxAge) {
			if err := os.Remove(path); err != nil {
				return err
			}
			count++
		}

		return nil
	})

	if err != nil {
		return count, fmt.Errorf("清理缩略图缓存失败: %w", err)
	}

	return count, nil
}
