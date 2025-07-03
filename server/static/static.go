package static

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/conf"
	"github.com/dongdio/OpenList/v4/internal/setting"
	"github.com/dongdio/OpenList/v4/public"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// staticFS 存储静态文件系统
// 可以是嵌入的文件系统或物理文件系统
var staticFS fs.FS

// initStaticFS 初始化静态文件系统
// 如果配置中指定了发布目录，则使用该目录
// 否则使用嵌入在二进制文件中的发布文件
func initStaticFS() {
	if conf.Conf.DistDir == "" {
		// 使用嵌入的发布文件
		dist, err := fs.Sub(public.Public, "dist")
		if err != nil {
			utils.Log.Fatalf("无法读取嵌入的dist目录: %v", err)
		}
		staticFS = dist
		return
	}
	// 使用物理文件系统中的发布目录
	staticFS = os.DirFS(conf.Conf.DistDir)
}

// initIndexHTML 初始化索引HTML文件
// 读取index.html并进行基本替换
func initIndexHTML() {
	// 打开索引文件
	indexFile, err := staticFS.Open("index.html")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			utils.Log.Fatalf("index.html文件不存在，您可能忘记将前端构建的dist目录放入public/dist")
		}
		utils.Log.Fatalf("无法读取index.html文件: %v", err)
	}
	defer func() {
		if err := indexFile.Close(); err != nil {
			utils.Log.Errorf("关闭index.html文件失败: %v", err)
		}
	}()

	// 读取索引文件内容
	indexContent, err := io.ReadAll(indexFile)
	if err != nil {
		utils.Log.Fatalf("无法读取dist/index.html内容: %v", err)
	}

	// 保存原始HTML内容
	conf.RawIndexHtml = string(indexContent)

	// 获取站点配置并替换相关配置
	siteConfig := getSiteConfig()
	replaceMap := map[string]string{
		"cdn: undefined":       fmt.Sprintf("cdn: '%s'", siteConfig.Cdn),
		"base_path: undefined": fmt.Sprintf("base_path: '%s'", siteConfig.BasePath),
	}

	// 替换配置变量
	for k, v := range replaceMap {
		conf.RawIndexHtml = strings.Replace(conf.RawIndexHtml, k, v, 1)
	}

	// 更新索引HTML
	UpdateIndexHTML()
}

// UpdateIndexHTML 根据最新设置更新索引HTML
// 将设置中的配置项应用到HTML模板中
func UpdateIndexHTML() {
	// 获取设置
	favicon := setting.GetStr(consts.Favicon)
	title := setting.GetStr(consts.SiteTitle)
	customizeHead := setting.GetStr(consts.CustomizeHead)
	customizeBody := setting.GetStr(consts.CustomizeBody)
	mainColor := setting.GetStr(consts.MainColor)

	// 首先更新管理页面HTML
	conf.ManageHtml = conf.RawIndexHtml

	// 替换管理页面的基本配置
	baseReplaceMap := map[string]string{
		"https://cdn.oplist.org/gh/OpenListTeam/Logo@main/logo.svg": favicon,
		"Loading...":            title,
		"main_color: undefined": fmt.Sprintf("main_color: '%s'", mainColor),
	}

	for k, v := range baseReplaceMap {
		conf.ManageHtml = strings.Replace(conf.ManageHtml, k, v, 1)
	}

	// 复制管理页面HTML作为基础
	conf.IndexHtml = conf.ManageHtml

	// 替换用户自定义的头部和正文内容
	customReplaceMap := map[string]string{
		"<!-- customize head -->": customizeHead,
		"<!-- customize body -->": customizeBody,
	}

	for k, v := range customReplaceMap {
		conf.IndexHtml = strings.Replace(conf.IndexHtml, k, v, 1)
	}
}

// Static 配置静态资源路由和处理无路由请求
// 设置静态文件服务并配置前端路由
//
// 参数:
//   - r: Gin路由组
//   - noRoute: 无路由处理函数
func Static(r *gin.RouterGroup, noRoute func(handlers ...gin.HandlerFunc)) {
	// 初始化静态文件系统和索引HTML
	initStaticFS()
	initIndexHTML()

	// 静态资源文件夹
	staticFolders := []string{"assets", "images", "streamer", "static"}

	// 设置静态资源缓存控制
	r.Use(func(c *gin.Context) {
		requestURI := c.Request.RequestURI
		for _, folder := range staticFolders {
			if strings.HasPrefix(requestURI, fmt.Sprintf("/%s/", folder)) {
				// 为静态资源设置长期缓存（6个月）
				c.Header("Cache-Control", "public, max-age=15552000")
				break
			}
		}
	})

	// 配置静态文件服务
	for _, folder := range staticFolders {
		sub, err := fs.Sub(staticFS, folder)
		if err != nil {
			utils.Log.Fatalf("无法找到静态资源目录: %s, 错误: %v", folder, err)
		}
		r.StaticFS(fmt.Sprintf("/%s/", folder), http.FS(sub))
	}

	// 配置无路由处理
	noRoute(func(c *gin.Context) {
		// 只允许GET和POST请求
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodPost {
			c.Status(http.StatusMethodNotAllowed) // 405 Method Not Allowed
			return
		}

		// 设置内容类型和状态
		c.Header("Content-Type", "text/html")
		c.Status(http.StatusOK)

		// 根据路径返回不同的HTML
		if strings.HasPrefix(c.Request.URL.Path, "/@manage") {
			// 管理页面
			if _, err := c.Writer.WriteString(conf.ManageHtml); err != nil {
				utils.Log.Errorf("写入管理页面HTML失败: %v", err)
			}
		} else {
			// 普通页面
			if _, err := c.Writer.WriteString(conf.IndexHtml); err != nil {
				utils.Log.Errorf("写入索引页面HTML失败: %v", err)
			}
		}

		// 刷新并立即写入响应头
		c.Writer.Flush()
		c.Writer.WriteHeaderNow()
	})
}