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
	"github.com/dongdio/OpenList/v4/drivers/base"
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
	utils.Log.Debug("Initializing static file system...")
	if conf.Conf.DistDir == "" {
		// 使用嵌入的发布文件
		dist, err := fs.Sub(public.Public, "dist")
		if err != nil {
			utils.Log.Fatalf("无法读取嵌入的dist目录: %v", err)
		}
		staticFS = dist
		utils.Log.Debug("Using embedded dist directory")
		return
	}
	// 使用物理文件系统中的发布目录
	staticFS = os.DirFS(conf.Conf.DistDir)
	utils.Log.Infof("Using custom dist directory: %s", conf.Conf.DistDir)
}

func replaceStrings(content string, replacements map[string]string) string {
	for old, n := range replacements {
		content = strings.Replace(content, old, n, 1)
	}
	return content
}

// initIndexHTML 初始化索引HTML文件
// 读取index.html并进行基本替换
func initIndexHTML() {
	utils.Log.Debug("Initializing index.html...")
	siteConfig := getSiteConfig()
	if conf.Conf.DistDir != "" || (conf.Conf.Cdn != "" && (conf.WebVersion == "" || conf.WebVersion == "beta" || conf.WebVersion == "dev")) {
		utils.Log.Infof("Fetching index.html from CDN: %s/index.html...", conf.Conf.Cdn)
		resp, err := base.RestyClient.R().
			SetHeader("Accept", "text/html").
			Get(fmt.Sprintf("%s/index.html", siteConfig.Cdn))
		if err != nil {
			utils.Log.Fatalf("failed to fetch index.html from CDN: %v", err)
		}
		if resp.StatusCode() != http.StatusOK {
			utils.Log.Fatalf("failed to fetch index.html from CDN, status code: %d", resp.StatusCode())
		}
		conf.RawIndexHTML = resp.String()
		utils.Log.Info("Successfully fetched index.html from CDN")
	} else {
		utils.Log.Debug("Reading index.html from static files system...")
		indexFile, err := staticFS.Open("index.html")
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				utils.Log.Fatalf("index.html not exist, you may forget to put dist of frontend to public/dist")
			}
			utils.Log.Fatalf("failed to read index.html: %v", err)
		}
		defer func() {
			_ = indexFile.Close()
		}()
		index, err := io.ReadAll(indexFile)
		if err != nil {
			utils.Log.Fatalf("failed to read dist/index.html")
		}
		conf.RawIndexHTML = string(index)
		utils.Log.Debug("Successfully read index.html from static files system")
	}
	utils.Log.Debug("Replacing placeholders in index.html...")
	replaceMap := map[string]string{
		"cdn: undefined":       fmt.Sprintf("cdn: '%s'", siteConfig.Cdn),
		"base_path: undefined": fmt.Sprintf("base_path: '%s'", siteConfig.BasePath),
	}
	conf.RawIndexHTML = replaceStrings(conf.RawIndexHTML, replaceMap)

	// 更新索引HTML
	UpdateIndexHTML()
}

// UpdateIndexHTML 根据最新设置更新索引HTML
// 将设置中的配置项应用到HTML模板中
func UpdateIndexHTML() {
	utils.Log.Debug("Updating index.html with settings...")
	// 获取设置
	favicon := setting.GetStr(consts.Favicon)
	title := setting.GetStr(consts.SiteTitle)
	customizeHead := setting.GetStr(consts.CustomizeHead)
	customizeBody := setting.GetStr(consts.CustomizeBody)
	mainColor := setting.GetStr(consts.MainColor)

	utils.Log.Debug("Applying replacements for default pages...")
	// 首先更新管理页面HTML
	conf.ManageHTML = conf.RawIndexHTML

	// 替换管理页面的基本配置
	baseReplaceMap := map[string]string{
		"https://cdn.oplist.org/gh/OpenListTeam/Logo@main/logo.svg": favicon,
		"Loading...":            title,
		"main_color: undefined": fmt.Sprintf("main_color: '%s'", mainColor),
	}

	conf.ManageHTML = replaceStrings(conf.ManageHTML, baseReplaceMap)
	utils.Log.Debug("Applying replacements for manage pages...")

	// 替换用户自定义的头部和正文内容
	customReplaceMap := map[string]string{
		"<!-- customize head -->": customizeHead,
		"<!-- customize body -->": customizeBody,
	}

	conf.IndexHTML = replaceStrings(conf.ManageHTML, customReplaceMap)
	utils.Log.Debug("Index.html update completed")
}

// Static 配置静态资源路由和处理无路由请求
// 设置静态文件服务并配置前端路由
//
// 参数:
//   - r: Gin路由组
//   - noRoute: 无路由处理函数
func Static(r *gin.RouterGroup, noRoute func(handlers ...gin.HandlerFunc)) {
	utils.Log.Debug("Setting up static routes...")
	// 初始化静态文件系统和索引HTML
	initStaticFS()
	initIndexHTML()

	// 静态资源文件夹
	staticFolders := []string{"assets", "images", "streamer", "static"}

	if conf.Conf.Cdn == "" {
		utils.Log.Debug("Setting up static file serving...")
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
			utils.Log.Debugf("Setting up route for folder: %s", folder)
			r.StaticFS(fmt.Sprintf("/%s/", folder), http.FS(sub))
		}
	} else {
		// Ensure static file redirected to CDN
		for _, folder := range staticFolders {
			r.GET(fmt.Sprintf("/%s/*filepath", folder), func(c *gin.Context) {
				filepath := c.Param("filepath")
				c.Redirect(http.StatusFound, fmt.Sprintf("%s/%s%s", conf.Conf.Cdn, folder, filepath))
			})
		}
	}

	utils.Log.Debug("Setting up catch-all route...")

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
			if _, err := c.Writer.WriteString(conf.ManageHTML); err != nil {
				utils.Log.Errorf("写入管理页面HTML失败: %v", err)
			}
		} else {
			// 普通页面
			if _, err := c.Writer.WriteString(conf.IndexHTML); err != nil {
				utils.Log.Errorf("写入索引页面HTML失败: %v", err)
			}
		}

		// 刷新并立即写入响应头
		c.Writer.Flush()
		c.Writer.WriteHeaderNow()
	})
}