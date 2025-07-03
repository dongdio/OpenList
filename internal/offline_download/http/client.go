package http

import (
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"

	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/offline_download/tool"
	"github.com/dongdio/OpenList/v4/utility/http_range"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

type SimpleHttp struct {
	client http.Client
}

func (s SimpleHttp) Name() string {
	return "SimpleHttp"
}

func (s SimpleHttp) Items() []model.SettingItem {
	return nil
}

func (s SimpleHttp) Init() (string, error) {
	return "ok", nil
}

func (s SimpleHttp) IsReady() bool {
	return true
}

func (s SimpleHttp) AddURL(args *tool.AddUrlArgs) (string, error) {
	panic("should not be called")
}

func (s SimpleHttp) Remove(task *tool.DownloadTask) error {
	panic("should not be called")
}

func (s SimpleHttp) Status(task *tool.DownloadTask) (*tool.Status, error) {
	panic("should not be called")
}

func (s SimpleHttp) Run(task *tool.DownloadTask) error {
	u := task.Url
	// parse url
	_u, err := url.Parse(u)
	if err != nil {
		return err
	}
	streamPut := task.DeletePolicy == tool.UploadDownloadStream
	method := http.MethodGet
	if streamPut {
		method = http.MethodHead
	}
	req, err := http.NewRequestWithContext(task.Ctx(), method, u, nil)
	if err != nil {
		return err
	}
	if streamPut {
		req.Header.Set("Range", "bytes=0-")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return errors.Errorf("http status code %d", resp.StatusCode)
	}
	// If Path is empty, use Hostname; otherwise, filePath euqals TempDir which causes os.Create to fail
	urlPath := _u.Path
	if urlPath == "" {
		urlPath = strings.ReplaceAll(_u.Host, ".", "_")
	}
	filename := path.Base(urlPath)
	var disposition string
	disposition, err = parseFilenameFromContentDisposition(resp.Header.Get("Content-Disposition"))
	if err == nil {
		filename = disposition
	}
	fileSize := resp.ContentLength
	if streamPut {
		if fileSize == 0 {
			start, end, _ := http_range.ParseContentRange(resp.Header.Get("Content-Range"))
			fileSize = start + end
		}
		task.SetTotalBytes(fileSize)
		task.TempDir = filename
		return nil
	}
	task.SetTotalBytes(fileSize)
	// save to temp dir
	_ = os.MkdirAll(task.TempDir, os.ModePerm)
	filePath := filepath.Join(task.TempDir, filename)
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	err = utils.CopyWithCtx(task.Ctx(), file, resp.Body, fileSize, task.SetProgress)
	return err
}

func init() {
	tool.Tools.Add(&SimpleHttp{})
}