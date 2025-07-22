package handles

import (
	"bytes"
	stdpath "path"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/microcosm-cc/bluemonday"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/yuin/goldmark"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/conf"
	"github.com/dongdio/OpenList/v4/internal/driver"
	"github.com/dongdio/OpenList/v4/internal/fs"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/setting"
	"github.com/dongdio/OpenList/v4/server/common"
	"github.com/dongdio/OpenList/v4/utility/net"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// Down 处理文件下载请求
// 根据存储配置决定是直接重定向还是通过代理下载
func Down(c *gin.Context) {
	// 获取文件路径
	rawPath := c.Value(consts.PathKey).(string)
	filename := stdpath.Base(rawPath)

	// 获取存储驱动
	storage, err := fs.GetStorage(rawPath, &fs.GetStoragesArgs{})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 判断是否需要代理
	if common.ShouldProxy(storage, filename) {
		Proxy(c)
		return
	}

	// 直接获取下载链接并重定向
	link, _, err := fs.Link(c.Request.Context(), rawPath, model.LinkArgs{
		IP:       c.ClientIP(),
		Header:   c.Request.Header,
		Type:     c.Query("type"),
		Redirect: true,
	})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	redirect(c, link)
}

// Proxy 处理需要代理的文件下载请求
func Proxy(c *gin.Context) {
	// 获取文件路径
	rawPath := c.Value(consts.PathKey).(string)
	filename := stdpath.Base(rawPath)

	// 获取存储驱动
	storage, err := fs.GetStorage(rawPath, &fs.GetStoragesArgs{})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// 检查是否可以代理
	if canProxy(storage, filename) {
		if _, ok := c.GetQuery("d"); !ok {
			if url := common.GenerateDownProxyURL(storage.GetStorage(), rawPath); url != "" {
				c.Redirect(302, url)
				return
			}
		}
		// 获取文件链接
		link, file, err := fs.Link(c.Request.Context(), rawPath, model.LinkArgs{
			Header: c.Request.Header,
			Type:   c.Query("type"),
		})
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
		proxy(c, link, file, storage.GetStorage().ProxyRange)
	} else {
		common.ErrorStrResp(c, "proxy not allowed", 403)
	}
}

// redirect 处理文件下载重定向
func redirect(c *gin.Context, link *model.Link) {
	defer link.Close()
	var err error
	// 设置安全相关头部
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("Cache-Control", "max-age=0, no-cache, no-store, must-revalidate")

	// 处理URL参数转发
	if setting.GetBool(consts.ForwardDirectLinkParams) {
		query := c.Request.URL.Query()

		// 移除忽略的参数
		for _, paramName := range conf.SlicesMap[consts.IgnoreDirectLinkParams] {
			query.Del(paramName)
		}
		// 注入查询参数到URL
		link.URL, err = utils.InjectQuery(link.URL, query)
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
	}
	// 重定向到文件URL
	c.Redirect(302, link.URL)
}

// localProxy 本地代理文件下载
func proxy(c *gin.Context, link *model.Link, file model.Obj, proxyRange bool) {
	defer link.Close()
	// 处理URL参数转发
	if link.URL != "" && setting.GetBool(consts.ForwardDirectLinkParams) {
		query := c.Request.URL.Query()

		// 移除忽略的参数
		for _, paramName := range conf.SlicesMap[consts.IgnoreDirectLinkParams] {
			query.Del(paramName)
		}

		// 注入查询参数到URL
		var err error
		link.URL, err = utils.InjectQuery(link.URL, query)
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
	}

	// 处理范围请求
	if proxyRange {
		link = common.ProxyRange(c, link, file.GetSize())
	}

	// 创建响应写入器
	writer := &common.WrittenResponseWriter{ResponseWriter: c.Writer}
	var err error

	// 特殊处理Markdown文件
	if utils.Ext(file.GetName()) == "md" && setting.GetBool(consts.FilterReadMeScripts) {
		// 预先分配合适大小的缓冲区
		buf := bytes.NewBuffer(make([]byte, 0, file.GetSize()))
		interceptWriter := &common.InterceptResponseWriter{ResponseWriter: writer, Writer: buf}

		// 代理请求并拦截响应
		err = common.Proxy(interceptWriter, c.Request, link, file)

		if err == nil && buf.Len() > 0 {
			// 如果状态码不是成功，直接返回原始内容
			if c.Writer.Status() < 200 || c.Writer.Status() > 300 {
				c.Writer.Write(buf.Bytes())
				return
			}

			// 将Markdown转换为HTML
			var html bytes.Buffer
			if err = goldmark.Convert(buf.Bytes(), &html); err != nil {
				err = errors.Wrap(err, "markdown conversion failed")
			} else {
				// 清空原缓冲区并进行安全过滤
				buf.Reset()
				err = bluemonday.UGCPolicy().SanitizeReaderToWriter(&html, buf)
				if err == nil {
					// 设置正确的内容类型和长度
					writer.Header().Set("Content-Length", strconv.FormatInt(int64(buf.Len()), 10))
					writer.Header().Set("Content-Type", "text/html; charset=utf-8")
					_, err = utils.CopyWithBuffer(writer, buf)
				}
			}
		}
	} else {
		// 直接代理其他类型文件
		err = common.Proxy(writer, c.Request, link, file)
	}

	// 错误处理
	if err == nil {
		return
	}

	if writer.IsWritten() {
		// 如果已经写入了响应，只能记录错误
		log.Errorf("%s %s local proxy error: %+v", c.Request.Method, c.Request.URL.Path, err)
	} else {
		// 否则返回错误响应
		var statusCode net.ErrorHTTPStatusCode
		if errors.As(errors.Unwrap(err), &statusCode) {
			common.ErrorResp(c, err, int(statusCode), true)
		}
	}
}

// canProxy 判断文件是否可以被代理
// 满足以下条件之一时可以代理:
// 1. 存储配置要求必须代理
// 2. 存储配置启用了Web代理
// 3. 存储配置启用了WebDAV代理
// 4. 文件扩展名在代理类型列表中
// 5. 文件扩展名在文本类型列表中
func canProxy(storage driver.Driver, filename string) bool {
	// 检查存储配置
	if storage.Config().MustProxy() ||
		storage.GetStorage().WebProxy ||
		storage.GetStorage().WebdavProxyURL() {
		return true
	}

	// 检查文件类型
	fileExt := utils.Ext(filename)
	if utils.SliceContains(conf.SlicesMap[consts.ProxyTypes], fileExt) {
		return true
	}

	if utils.SliceContains(conf.SlicesMap[consts.TextTypes], fileExt) {
		return true
	}

	return false
}