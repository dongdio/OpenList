package _123

import (
	"context"
	"fmt"
	"hash/crc32"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"resty.dev/v3"

	"github.com/dongdio/OpenList/v4/drivers/base"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

// API端点常量定义
const (
	Api              = "https://www.123pan.com/api"
	AApi             = "https://www.123pan.com/a/api"
	BApi             = "https://www.123pan.com/b/api"
	LoginApi         = "https://login.123pan.com/api"
	MainApi          = BApi
	SignIn           = LoginApi + "/user/sign_in"
	Logout           = MainApi + "/user/logout"
	UserInfo         = MainApi + "/user/info"
	FileList         = MainApi + "/file/list/new"
	DownloadInfo     = MainApi + "/file/download_info"
	Mkdir            = MainApi + "/file/upload_request"
	Move             = MainApi + "/file/mod_pid"
	Rename           = MainApi + "/file/rename"
	Trash            = MainApi + "/file/trash"
	UploadRequest    = MainApi + "/file/upload_request"
	UploadComplete   = MainApi + "/file/upload_complete"
	S3PreSignedUrls  = MainApi + "/file/s3_repare_upload_parts_batch"
	S3Auth           = MainApi + "/file/s3_upload_object/auth"
	UploadCompleteV2 = MainApi + "/file/upload_complete/v2"
	S3Complete       = MainApi + "/file/s3_complete_multipart_upload"
	// AuthKeySalt      = "8-8D$sL8gPjom7bk#cY"
)

// signPath 生成API请求签名
// 参数：
//   - path: API路径
//   - os: 操作系统标识
//   - version: 客户端版本
//
// 返回：
//   - 时间签名和完整签名
func signPath(path string, os string, version string) (k string, v string) {
	// 字符表，用于时间格式转换
	table := []byte{'a', 'd', 'e', 'f', 'g', 'h', 'l', 'm', 'y', 'i', 'j', 'n', 'o', 'p', 'k', 'q', 'r', 's', 't', 'u', 'b', 'c', 'v', 'w', 's', 'z'}

	// 生成随机数
	random := fmt.Sprintf("%.f", math.Round(1e7*rand.Float64()))

	// 获取当前时间（中国时区）
	now := time.Now().In(time.FixedZone("CST", 8*3600))
	timestamp := fmt.Sprint(now.Unix())

	// 转换时间格式
	nowStr := []byte(now.Format("200601021504"))
	for i := range nowStr {
		nowStr[i] = table[nowStr[i]-48]
	}

	// 计算时间签名
	timeSign := fmt.Sprint(crc32.ChecksumIEEE(nowStr))

	// 构建签名数据
	data := strings.Join([]string{timestamp, random, path, os, version, timeSign}, "|")

	// 计算数据签名
	dataSign := fmt.Sprint(crc32.ChecksumIEEE([]byte(data)))

	return timeSign, strings.Join([]string{timestamp, random, dataSign}, "-")
}

// GetApi 为API URL添加签名参数
// 参数：
//   - rawUrl: 原始API URL
//
// 返回：
//   - 添加签名后的URL
func GetApi(rawUrl string) string {
	u, _ := url.Parse(rawUrl)
	query := u.Query()
	k, v := signPath(u.Path, "web", "3")
	query.Add(k, v)
	u.RawQuery = query.Encode()
	return u.String()
}

// func GetApi(url string) string {
//	vm := js.New()
//	vm.Set("url", url[22:])
//	r, err := vm.RunString(`
//	(function(e){
//        function A(t, e) {
//            e = 1 < arguments.length && void 0 !== e ? e : 10;
//            for (var n = function() {
//                for (var t = [], e = 0; e < 256; e++) {
//                    for (var n = e, r = 0; r < 8; r++)
//                        n = 1 & n ? 3988292384 ^ n >>> 1 : n >>> 1;
//                    t[e] = n
//                }
//                return t
//            }(), r = function(t) {
//                t = t.replace(/\\r\\n/g, "\\n");
//                for (var e = "", n = 0; n < t.length; n++) {
//                    var r = t.charCodeAt(n);
//                    r < 128 ? e += String.fromCharCode(r) : e = 127 < r && r < 2048 ? (e += String.fromCharCode(r >> 6 | 192)) + String.fromCharCode(63 & r | 128) : (e = (e += String.fromCharCode(r >> 12 | 224)) + String.fromCharCode(r >> 6 & 63 | 128)) + String.fromCharCode(63 & r | 128)
//                }
//                return e
//            }(t), a = -1, i = 0; i < r.length; i++)
//                a = a >>> 8 ^ n[255 & (a ^ r.charCodeAt(i))];
//            return (a = (-1 ^ a) >>> 0).toString(e)
//        }
//
//	   function v(t) {
//	       return (v = "function" == typeof Symbol && "symbol" == typeof Symbol.iterator ? function(t) {
//	                   return typeof t
//	               }
//	               : function(t) {
//	                   return t && "function" == typeof Symbol && t.constructor === Symbol && t !== Symbol.prototype ? "symbol" : typeof t
//	               }
//	       )(t)
//	   }
//
//		for (p in a = Math.round(1e7 * Math.random()),
//		o = Math.round(((new Date).getTime() + 60 * (new Date).getTimezoneOffset() * 1e3 + 288e5) / 1e3).toString(),
//		m = ["a", "d", "e", "f", "g", "h", "l", "m", "y", "i", "j", "n", "o", "p", "k", "q", "r", "s", "t", "u", "b", "c", "v", "w", "s", "z"],
//		u = function(t, e, n) {
//			var r;
//			n = 2 < arguments.length && void 0 !== n ? n : 8;
//			return 0 === arguments.length ? null : (r = "object" === v(t) ? t : (10 === "".concat(t).length && (t = 1e3 * Number.parseInt(t)),
//			new Date(t)),
//			t += 6e4 * new Date(t).getTimezoneOffset(),
//			{
//				y: (r = new Date(t + 36e5 * n)).getFullYear(),
//				m: r.getMonth() + 1 < 10 ? "0".concat(r.getMonth() + 1) : r.getMonth() + 1,
//				d: r.getDate() < 10 ? "0".concat(r.getDate()) : r.getDate(),
//				h: r.getHours() < 10 ? "0".concat(r.getHours()) : r.getHours(),
//				f: r.getMinutes() < 10 ? "0".concat(r.getMinutes()) : r.getMinutes()
//			})
//		}(o),
//		h = u.y,
//		g = u.m,
//		l = u.d,
//		c = u.h,
//		u = u.f,
//		d = [h, g, l, c, u].join(""),
//		f = [],
//		d)
//			f.push(m[Number(d[p])]);
//		return h = A(f.join("")),
//		g = A("".concat(o, "|").concat(a, "|").concat(e, "|").concat("web", "|").concat("3", "|").concat(h)),
//		"".concat(h, "=").concat(o, "-").concat(a, "-").concat(g);
//	})(url)
//	   `)
//	if err != nil {
//		fmt.Println(err)
//		return url
//	}
//	v, _ := r.Export().(string)
//	return url + "?" + v
// }

// login 执行登录操作获取访问令牌
// 返回：
//   - 可能的错误
func (d *Pan123) login() error {
	var body base.Json

	// 根据用户名格式选择不同的登录方式
	if utils.IsEmailFormat(d.Username) {
		// 邮箱登录
		body = base.Json{
			"mail":     d.Username,
			"password": d.Password,
			"type":     2,
		}
	} else {
		// 用户名登录
		body = base.Json{
			"passport": d.Username,
			"password": d.Password,
			"remember": true,
		}
	}

	// 发送登录请求
	res, err := base.RestyClient.R().
		SetHeaders(map[string]string{
			"origin":      "https://www.123pan.com",
			"referer":     "https://www.123pan.com/",
			"user-agent":  "Dart/2.19(dart:io)-openlist",
			"platform":    "web",
			"app-version": "3",
			// "user-agent":  consts.ChromeUserAgent,
		}).
		SetBody(body).Post(SignIn)
	if err != nil {
		return err
	}

	// 检查响应状态
	if utils.GetBytes(res.Bytes(), "code").Int() != 200 {
		err = errors.New(utils.GetBytes(res.Bytes(), "message").String())
	} else {
		// 提取访问令牌
		d.AccessToken = utils.GetBytes(res.Bytes(), "data", "token").String()
	}
	return err
}

// func authKey(reqUrl string) (*string, error) {
//	reqURL, err := url.Parse(reqUrl)
//	if err != nil {
//		return nil, err
//	}
//
//	nowUnix := time.Now().Unix()
//	random := rand.Intn(0x989680)
//
//	p4 := fmt.Sprintf("%d|%d|%s|%s|%s|%s", nowUnix, random, reqURL.Path, "web", "3", AuthKeySalt)
//	authKey := fmt.Sprintf("%d-%d-%x", nowUnix, random, md5.Sum([]byte(p4)))
//	return &authKey, nil
// }

// Request 发送API请求
// 参数：
//   - url: API URL
//   - method: HTTP方法
//   - callback: 请求回调函数，用于自定义请求
//   - resp: 响应结构体指针
//
// 返回：
//   - 响应体字节数组和可能的错误
func (d *Pan123) Request(url string, method string, callback base.ReqCallback, resp any) ([]byte, error) {
	isRetry := false
do:
	// 创建请求
	req := base.RestyClient.R()
	req.SetHeaders(map[string]string{
		"origin":        "https://www.123pan.com",
		"referer":       "https://www.123pan.com/",
		"authorization": "Bearer " + d.AccessToken,
		"user-agent":    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) openlist-client",
		"platform":      "web",
		"app-version":   "3",
		// "user-agent":    consts.ChromeUserAgent,
	})

	// 应用回调函数
	if callback != nil {
		callback(req)
	}

	// 设置响应结构体
	if resp != nil {
		req.SetResult(resp)
	}

	// 发送请求
	res, err := req.Execute(method, GetApi(url))
	if err != nil {
		return nil, err
	}

	// 解析响应
	body := res.Bytes()
	code := utils.GetBytes(body, "code").Int()

	// 处理错误响应
	if code != 0 {
		// 处理授权失效的情况
		if !isRetry && code == 401 {
			err := d.login()
			if err != nil {
				return nil, err
			}
			isRetry = true
			goto do
		}
		return nil, errors.New(utils.GetBytes(body, "message").String())
	}

	return body, nil
}

// getFiles 获取指定目录下的文件列表
// 参数：
//   - ctx: 上下文
//   - parentId: 父目录ID
//   - name: 父目录名称（用于日志）
//
// 返回：
//   - 文件列表和可能的错误
func (d *Pan123) getFiles(ctx context.Context, parentId string, name string) ([]File, error) {
	page := 1
	total := 0
	res := make([]File, 0)

	// 分页获取文件列表
	for {
		// 应用API速率限制
		if err := d.APIRateLimit(ctx, FileList); err != nil {
			return nil, err
		}

		// 准备查询参数
		var resp Files
		query := map[string]string{
			"driveId":              "0",
			"limit":                "100",
			"next":                 "0",
			"orderBy":              "file_id",
			"orderDirection":       "desc",
			"parentFileId":         parentId,
			"trashed":              "false",
			"SearchData":           "",
			"Page":                 strconv.Itoa(page),
			"OnlyLookAbnormalFile": "0",
			"event":                "homeListFile",
			"operateType":          "4",
			"inDirectSpace":        "false",
		}

		// 发送请求
		_res, err := d.Request(FileList, http.MethodGet, func(req *resty.Request) {
			req.SetQueryParams(query)
		}, &resp)
		if err != nil {
			return nil, err
		}
		log.Debug(string(_res))

		// 处理响应
		page++
		res = append(res, resp.Data.InfoList...)
		total = resp.Data.Total

		// 检查是否已获取所有文件
		if len(resp.Data.InfoList) == 0 || resp.Data.Next == "-1" {
			break
		}
	}

	// 验证文件数量是否一致
	if len(res) != total {
		log.Warnf("从远程获取的文件数量不正确，路径 %s: 期望 %d，实际 %d", name, total, len(res))
	}

	return res, nil
}