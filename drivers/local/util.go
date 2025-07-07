package local

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/disintegration/imaging"
	ffmpeg "github.com/u2takey/ffmpeg-go"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

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
//   - videoPath: 视频文件的完整路径
//
// 返回:
//   - *bytes.Buffer: 缩略图数据
//   - error: 错误信息
func (d *Local) GetSnapshot(videoPath string) (imgData *bytes.Buffer, err error) {
	// 使用ffprobe获取视频时长
	jsonOutput, err := ffmpeg.Probe(videoPath)
	if err != nil {
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
	err = utils.Json.Unmarshal([]byte(jsonOutput), &probe)
	if err != nil {
		return nil, fmt.Errorf("解析ffprobe输出失败: %w", err)
	}

	totalDuration, err := strconv.ParseFloat(probe.Format.Duration, 64)
	if err != nil {
		return nil, fmt.Errorf("解析视频时长失败: %w", err)
	}

	// 计算截图时间点
	var seekPosition string
	if d.videoThumbPosIsPercentage {
		// 按百分比计算时间点
		seekPosition = fmt.Sprintf("%f", totalDuration*d.videoThumbPos)
	} else {
		// 使用指定的时间点，如果超过视频长度则使用视频末尾
		if d.videoThumbPos > totalDuration {
			seekPosition = fmt.Sprintf("%f", totalDuration)
		} else {
			seekPosition = fmt.Sprintf("%f", d.videoThumbPos)
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
		return nil, err
	}
	defer dirHandle.Close()

	// 读取目录内容
	fileList, err := dirHandle.Readdir(-1) // -1表示读取所有条目
	if err != nil {
		return nil, err
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
	thumbPrefix := "openlist_thumb_"
	thumbName := thumbPrefix + utils.GetMD5EncodeStr(filePath) + ".png"

	// 检查缓存目录是否配置
	if d.ThumbCacheFolder != "" {
		// 跳过已经是缩略图的文件
		if strings.HasPrefix(file.GetName(), thumbPrefix) {
			return nil, &filePath, nil
		}

		// 检查缓存中是否已存在缩略图
		thumbPath := filepath.Join(d.ThumbCacheFolder, thumbName)
		if utils.Exists(thumbPath) {
			return nil, &thumbPath, nil
		}
	}

	// 根据文件类型获取源数据
	var sourceBuffer *bytes.Buffer
	fileType := utils.GetFileType(file.GetName())

	if fileType == consts.VIDEO {
		// 视频文件，获取视频快照
		videoBuf, err := d.GetSnapshot(filePath)
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
	thumbImg := imaging.Resize(image, 144, 0, imaging.Lanczos)

	// 编码为PNG格式
	var resultBuffer bytes.Buffer
	err = imaging.Encode(&resultBuffer, thumbImg, imaging.PNG)
	if err != nil {
		return nil, nil, fmt.Errorf("编码缩略图失败: %w", err)
	}

	// 如果配置了缓存目录，保存缩略图
	if d.ThumbCacheFolder != "" {
		cachePath := filepath.Join(d.ThumbCacheFolder, thumbName)
		err = os.WriteFile(cachePath, resultBuffer.Bytes(), 0666)
		if err != nil {
			// 缓存失败不影响返回结果，只是不能缓存
			// 可以记录日志，但继续返回生成的缩略图
		}
	}

	return &resultBuffer, nil, nil
}
