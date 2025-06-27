package server

import (
	"github.com/gogf/gf/v2/net/ghttp"

	"github.com/dongdio/OpenList/internal/conf"
	"github.com/dongdio/OpenList/server/handles"
	"github.com/dongdio/OpenList/server/middlewares"
	"github.com/dongdio/OpenList/utility/utils"
)

func Init_bak(g *ghttp.RouterGroup) {
	if utils.SliceContains([]string{"", "/"}, conf.URL.Path) {
		g.GET("/", func(c *ghttp.Request) {
			c.Response.RedirectTo(conf.URL.Path, 302)
		})
	}
	g = g.Group(conf.URL.Path)
	if conf.Conf.Scheme.HttpPort != -1 && conf.Conf.Scheme.HttpsPort != -1 && conf.Conf.Scheme.ForceHttps {
		g.Middleware(middlewares.ForceHTTPS1)
	}
	g.ALL("/ping", func(c *ghttp.Request) {
		c.Response.WriteStatus(200, "pong")
	})
	g.GET("/favicon.ico", handles.Favicon1)
	g.GET("/robots.txt", handles.Robots1)
	g.GET("/i/:link_name", handles.Plist1)
}