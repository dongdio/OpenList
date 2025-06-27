package initialize

import (
	"strconv"

	"github.com/pkg/errors"
	"gorm.io/gorm"

	"github.com/dongdio/OpenList/consts"
	"github.com/dongdio/OpenList/global"
	"github.com/dongdio/OpenList/internal/conf"
	"github.com/dongdio/OpenList/internal/db"
	"github.com/dongdio/OpenList/internal/model"
	"github.com/dongdio/OpenList/internal/offline_download/tool"
	"github.com/dongdio/OpenList/internal/op"
	"github.com/dongdio/OpenList/utility/utils"
	"github.com/dongdio/OpenList/utility/utils/random"
)

var initialSettingItems []model.SettingItem

func initSettings() {
	InitialSettings()
	// check deprecated
	settings, err := op.GetSettingItems()
	if err != nil {
		utils.Log.Fatalf("failed get settings: %+v", err)
	}
	settingMap := map[string]*model.SettingItem{}
	for _, v := range settings {
		if !isActive(v.Key) && v.Flag != model.DEPRECATED {
			v.Flag = model.DEPRECATED
			err = op.SaveSettingItem(&v)
			if err != nil {
				utils.Log.Fatalf("failed save setting: %+v", err)
			}
		}
		settingMap[v.Key] = &v
	}
	// create or save setting
	save := false
	for i := range initialSettingItems {
		item := &initialSettingItems[i]
		item.Index = uint(i)
		if item.PreDefault == "" {
			item.PreDefault = item.Value
		}
		// err
		stored, ok := settingMap[item.Key]
		if !ok {
			stored, err = op.GetSettingItemByKey(item.Key)
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				utils.Log.Fatalf("failed get setting: %+v", err)
				continue
			}
		}
		if stored != nil && item.Key != consts.VERSION && stored.Value != item.PreDefault {
			item.Value = stored.Value
		}
		_, err = op.HandleSettingItemHook(item)
		if err != nil {
			utils.Log.Errorf("failed to execute hook on %s: %+v", item.Key, err)
			continue
		}
		// save
		if stored == nil || *item != *stored {
			save = true
		}
	}
	if save {
		err = db.SaveSettingItems(initialSettingItems)
		if err != nil {
			utils.Log.Fatalf("failed save setting: %+v", err)
		} else {
			op.SettingCacheUpdate()
		}
	}
}

func isActive(key string) bool {
	for _, item := range initialSettingItems {
		if item.Key == key {
			return true
		}
	}
	return false
}

func InitialSettings() []model.SettingItem {
	var token string
	if global.Dev {
		token = "dev_token"
	} else {
		token = random.Token()
	}
	initialSettingItems = []model.SettingItem{
		// site settings
		{Key: consts.VERSION, Value: conf.Version, Type: consts.TypeString, Group: model.SITE, Flag: model.READONLY},
		// {Key: conf.ApiUrl, Value: "", Type: conf.TypeString, Group: model.SITE},
		// {Key: conf.BasePath, Value: "", Type: conf.TypeString, Group: model.SITE},
		{Key: consts.SiteTitle, Value: "OpenList", Type: consts.TypeString, Group: model.SITE},
		{Key: consts.Announcement, Value: "### repo\nhttps://github.com/dongdio/OpenList", Type: consts.TypeText, Group: model.SITE},
		{Key: "pagination_type", Value: "all", Type: consts.TypeSelect, Options: "all,pagination,load_more,auto_load_more", Group: model.SITE},
		{Key: "default_page_size", Value: "30", Type: consts.TypeNumber, Group: model.SITE},
		{Key: consts.AllowIndexed, Value: "false", Type: consts.TypeBool, Group: model.SITE},
		{Key: consts.AllowMounted, Value: "true", Type: consts.TypeBool, Group: model.SITE},
		{Key: consts.RobotsTxt, Value: "User-agent: *\nAllow: /", Type: consts.TypeText, Group: model.SITE},
		// style settings
		{Key: consts.Logo, Value: "https://cdn.oplist.org/gh/OpenListTeam/Logo@main/logo.svg", Type: consts.TypeText, Group: model.STYLE},
		{Key: consts.Favicon, Value: "https://cdn.oplist.org/gh/OpenListTeam/Logo@main/logo.svg", Type: consts.TypeString, Group: model.STYLE},
		{Key: consts.MainColor, Value: "#1890ff", Type: consts.TypeString, Group: model.STYLE},
		{Key: "home_icon", Value: "🏠", Type: consts.TypeString, Group: model.STYLE},
		{Key: "home_container", Value: "max_980px", Type: consts.TypeSelect, Options: "max_980px,hope_container", Group: model.STYLE},
		{Key: "settings_layout", Value: "list", Type: consts.TypeSelect, Options: "list,responsive", Group: model.STYLE},
		// preview settings
		{Key: consts.TextTypes, Value: "txt,htm,html,xml,java,properties,sql,js,md,json,conf,ini,vue,php,py,bat,gitignore,yml,go,sh,c,cpp,h,hpp,tsx,vtt,srt,ass,rs,lrc", Type: consts.TypeText, Group: model.PREVIEW, Flag: model.PRIVATE},
		{Key: consts.AudioTypes, Value: "mp3,flac,ogg,m4a,wav,opus,wma", Type: consts.TypeText, Group: model.PREVIEW, Flag: model.PRIVATE},
		{Key: consts.VideoTypes, Value: "mp4,mkv,avi,mov,rmvb,webm,flv,m3u8", Type: consts.TypeText, Group: model.PREVIEW, Flag: model.PRIVATE},
		{Key: consts.ImageTypes, Value: "jpg,tiff,jpeg,png,gif,bmp,svg,ico,swf,webp", Type: consts.TypeText, Group: model.PREVIEW, Flag: model.PRIVATE},
		// {Key: conf.OfficeTypes, Value: "doc,docx,xls,xlsx,ppt,pptx", Type: conf.TypeText, Group: model.PREVIEW, Flag: model.PRIVATE},
		{Key: consts.ProxyTypes, Value: "m3u8,url", Type: consts.TypeText, Group: model.PREVIEW, Flag: model.PRIVATE},
		{Key: consts.ProxyIgnoreHeaders, Value: "authorization,referer", Type: consts.TypeText, Group: model.PREVIEW, Flag: model.PRIVATE},
		{Key: "external_previews", Value: `{}`, Type: consts.TypeText, Group: model.PREVIEW},
		{Key: "iframe_previews", Value: `{
			"doc,docx,xls,xlsx,ppt,pptx": {
				"Microsoft":"https://view.officeapps.live.com/op/view.aspx?src=$e_url",
				"Google":"https://docs.google.com/gview?url=$e_url&embedded=true"
			},
			"pdf": {
				"PDF.js": "https://res.oplist.org/pdf.js/web/viewer.html?file=$e_url" 
			},
			"epub": {
				"EPUB.js":"https://res.oplist.org/epub.js/viewer.html?url=$e_url"
			}
		}`, Type: consts.TypeText, Group: model.PREVIEW},
		//		{Key: conf.OfficeViewers, Value: `{
		//	"Microsoft":"https://view.officeapps.live.com/op/view.aspx?src=$url",
		//	"Google":"https://docs.google.com/gview?url=$url&embedded=true",
		// }`, Type: conf.TypeText, Group: model.PREVIEW},
		//		{Key: conf.PdfViewers, Value: `{
		//	"pdf.js":"https://openlistteam.github.io/pdf.js/web/viewer.html?file=$url"
		// }`, Type: conf.TypeText, Group: model.PREVIEW},
		{Key: "audio_cover", Value: "https://cdn.oplist.org/gh/OpenListTeam/Logo@main/logo.svg", Type: consts.TypeString, Group: model.PREVIEW},
		{Key: consts.AudioAutoplay, Value: "true", Type: consts.TypeBool, Group: model.PREVIEW},
		{Key: consts.VideoAutoplay, Value: "true", Type: consts.TypeBool, Group: model.PREVIEW},
		{Key: consts.PreviewArchivesByDefault, Value: "true", Type: consts.TypeBool, Group: model.PREVIEW},
		{Key: consts.ReadMeAutoRender, Value: "true", Type: consts.TypeBool, Group: model.PREVIEW},
		{Key: consts.FilterReadMeScripts, Value: "true", Type: consts.TypeBool, Group: model.PREVIEW},
		// global settings
		{Key: consts.HideFiles, Value: "/\\/README.md/i", Type: consts.TypeText, Group: model.GLOBAL},
		{Key: "package_download", Value: "true", Type: consts.TypeBool, Group: model.GLOBAL},
		{Key: consts.CustomizeHead, PreDefault: `<script src="https://cdnjs.cloudflare.com/polyfill/v3/polyfill.min.js?features=String.prototype.replaceAll"></script>`, Type: consts.TypeText, Group: model.GLOBAL, Flag: model.PRIVATE},
		{Key: consts.CustomizeBody, Type: consts.TypeText, Group: model.GLOBAL, Flag: model.PRIVATE},
		{Key: consts.LinkExpiration, Value: "0", Type: consts.TypeNumber, Group: model.GLOBAL, Flag: model.PRIVATE},
		{Key: consts.SignAll, Value: "true", Type: consts.TypeBool, Group: model.GLOBAL, Flag: model.PRIVATE},
		{Key: consts.PrivacyRegs, Value: `(?:(?:\d|[1-9]\d|1\d\d|2[0-4]\d|25[0-5])\.){3}(?:\d|[1-9]\d|1\d\d|2[0-4]\d|25[0-5])
([[:xdigit:]]{1,4}(?::[[:xdigit:]]{1,4}){7}|::|:(?::[[:xdigit:]]{1,4}){1,6}|[[:xdigit:]]{1,4}:(?::[[:xdigit:]]{1,4}){1,5}|(?:[[:xdigit:]]{1,4}:){2}(?::[[:xdigit:]]{1,4}){1,4}|(?:[[:xdigit:]]{1,4}:){3}(?::[[:xdigit:]]{1,4}){1,3}|(?:[[:xdigit:]]{1,4}:){4}(?::[[:xdigit:]]{1,4}){1,2}|(?:[[:xdigit:]]{1,4}:){5}:[[:xdigit:]]{1,4}|(?:[[:xdigit:]]{1,4}:){1,6}:)
(?U)access_token=(.*)&`,
			Type: consts.TypeText, Group: model.GLOBAL, Flag: model.PRIVATE},
		{Key: consts.OcrApi, Value: "https://api.example.com/ocr/file/json", Type: consts.TypeString, Group: model.GLOBAL}, // TODO: This can be replace by a community-hosted endpoint, see https://github.com/xhofe/ocr_api_server
		{Key: consts.FilenameCharMapping, Value: `{"/": "|"}`, Type: consts.TypeText, Group: model.GLOBAL},
		{Key: consts.ForwardDirectLinkParams, Value: "false", Type: consts.TypeBool, Group: model.GLOBAL},
		{Key: consts.IgnoreDirectLinkParams, Value: "sign,openlist_ts", Type: consts.TypeString, Group: model.GLOBAL},
		{Key: consts.WebauthnLoginEnabled, Value: "false", Type: consts.TypeBool, Group: model.GLOBAL, Flag: model.PUBLIC},

		// single settings
		{Key: consts.Token, Value: token, Type: consts.TypeString, Group: model.SINGLE, Flag: model.PRIVATE},
		{Key: consts.SearchIndex, Value: "none", Type: consts.TypeSelect, Options: "database,database_non_full_text,bleve,meilisearch,none", Group: model.INDEX},
		{Key: consts.AutoUpdateIndex, Value: "false", Type: consts.TypeBool, Group: model.INDEX},
		{Key: consts.IgnorePaths, Value: "", Type: consts.TypeText, Group: model.INDEX, Flag: model.PRIVATE, Help: `one path per line`},
		{Key: consts.MaxIndexDepth, Value: "20", Type: consts.TypeNumber, Group: model.INDEX, Flag: model.PRIVATE, Help: `max depth of index`},
		{Key: consts.IndexProgress, Value: "{}", Type: consts.TypeText, Group: model.SINGLE, Flag: model.PRIVATE},

		// SSO settings
		{Key: consts.SSOLoginEnabled, Value: "false", Type: consts.TypeBool, Group: model.SSO, Flag: model.PUBLIC},
		{Key: consts.SSOLoginPlatform, Type: consts.TypeSelect, Options: "Casdoor,Github,Microsoft,Google,Dingtalk,OIDC", Group: model.SSO, Flag: model.PUBLIC},
		{Key: consts.SSOClientId, Value: "", Type: consts.TypeString, Group: model.SSO, Flag: model.PRIVATE},
		{Key: consts.SSOClientSecret, Value: "", Type: consts.TypeString, Group: model.SSO, Flag: model.PRIVATE},
		{Key: consts.SSOOIDCUsernameKey, Value: "name", Type: consts.TypeString, Group: model.SSO, Flag: model.PRIVATE},
		{Key: consts.SSOOrganizationName, Value: "", Type: consts.TypeString, Group: model.SSO, Flag: model.PRIVATE},
		{Key: consts.SSOApplicationName, Value: "", Type: consts.TypeString, Group: model.SSO, Flag: model.PRIVATE},
		{Key: consts.SSOEndpointName, Value: "", Type: consts.TypeString, Group: model.SSO, Flag: model.PRIVATE},
		{Key: consts.SSOJwtPublicKey, Value: "", Type: consts.TypeString, Group: model.SSO, Flag: model.PRIVATE},
		{Key: consts.SSOExtraScopes, Value: "", Type: consts.TypeString, Group: model.SSO, Flag: model.PRIVATE},
		{Key: consts.SSOAutoRegister, Value: "false", Type: consts.TypeBool, Group: model.SSO, Flag: model.PRIVATE},
		{Key: consts.SSODefaultDir, Value: "/", Type: consts.TypeString, Group: model.SSO, Flag: model.PRIVATE},
		{Key: consts.SSODefaultPermission, Value: "0", Type: consts.TypeNumber, Group: model.SSO, Flag: model.PRIVATE},
		{Key: consts.SSOCompatibilityMode, Value: "false", Type: consts.TypeBool, Group: model.SSO, Flag: model.PUBLIC},

		// ldap settings
		{Key: consts.LdapLoginEnabled, Value: "false", Type: consts.TypeBool, Group: model.LDAP, Flag: model.PUBLIC},
		{Key: consts.LdapServer, Value: "", Type: consts.TypeString, Group: model.LDAP, Flag: model.PRIVATE},
		{Key: consts.LdapManagerDN, Value: "", Type: consts.TypeString, Group: model.LDAP, Flag: model.PRIVATE},
		{Key: consts.LdapManagerPassword, Value: "", Type: consts.TypeString, Group: model.LDAP, Flag: model.PRIVATE},
		{Key: consts.LdapUserSearchBase, Value: "", Type: consts.TypeString, Group: model.LDAP, Flag: model.PRIVATE},
		{Key: consts.LdapUserSearchFilter, Value: "(uid=%s)", Type: consts.TypeString, Group: model.LDAP, Flag: model.PRIVATE},
		{Key: consts.LdapDefaultDir, Value: "/", Type: consts.TypeString, Group: model.LDAP, Flag: model.PRIVATE},
		{Key: consts.LdapDefaultPermission, Value: "0", Type: consts.TypeNumber, Group: model.LDAP, Flag: model.PRIVATE},
		{Key: consts.LdapLoginTips, Value: "login with ldap", Type: consts.TypeString, Group: model.LDAP, Flag: model.PUBLIC},

		// s3 settings
		{Key: consts.S3AccessKeyId, Value: "", Type: consts.TypeString, Group: model.S3, Flag: model.PRIVATE},
		{Key: consts.S3SecretAccessKey, Value: "", Type: consts.TypeString, Group: model.S3, Flag: model.PRIVATE},
		{Key: consts.S3Buckets, Value: "[]", Type: consts.TypeString, Group: model.S3, Flag: model.PRIVATE},

		// ftp settings
		{Key: consts.FTPPublicHost, Value: "127.0.0.1", Type: consts.TypeString, Group: model.FTP, Flag: model.PRIVATE},
		{Key: consts.FTPPasvPortMap, Value: "", Type: consts.TypeText, Group: model.FTP, Flag: model.PRIVATE},
		{Key: consts.FTPProxyUserAgent, Value: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) " +
			"Chrome/87.0.4280.88 Safari/537.36", Type: consts.TypeString, Group: model.FTP, Flag: model.PRIVATE},
		{Key: consts.FTPMandatoryTLS, Value: "false", Type: consts.TypeBool, Group: model.FTP, Flag: model.PRIVATE},
		{Key: consts.FTPImplicitTLS, Value: "false", Type: consts.TypeBool, Group: model.FTP, Flag: model.PRIVATE},
		{Key: consts.FTPTLSPrivateKeyPath, Value: "", Type: consts.TypeString, Group: model.FTP, Flag: model.PRIVATE},
		{Key: consts.FTPTLSPublicCertPath, Value: "", Type: consts.TypeString, Group: model.FTP, Flag: model.PRIVATE},

		// traffic settings
		{Key: consts.TaskOfflineDownloadThreadsNum, Value: strconv.Itoa(conf.Conf.Tasks.Download.Workers), Type: consts.TypeNumber, Group: model.TRAFFIC, Flag: model.PRIVATE},
		{Key: consts.TaskOfflineDownloadTransferThreadsNum, Value: strconv.Itoa(conf.Conf.Tasks.Transfer.Workers), Type: consts.TypeNumber, Group: model.TRAFFIC, Flag: model.PRIVATE},
		{Key: consts.TaskUploadThreadsNum, Value: strconv.Itoa(conf.Conf.Tasks.Upload.Workers), Type: consts.TypeNumber, Group: model.TRAFFIC, Flag: model.PRIVATE},
		{Key: consts.TaskCopyThreadsNum, Value: strconv.Itoa(conf.Conf.Tasks.Copy.Workers), Type: consts.TypeNumber, Group: model.TRAFFIC, Flag: model.PRIVATE},
		{Key: consts.TaskDecompressDownloadThreadsNum, Value: strconv.Itoa(conf.Conf.Tasks.Decompress.Workers), Type: consts.TypeNumber, Group: model.TRAFFIC, Flag: model.PRIVATE},
		{Key: consts.TaskDecompressUploadThreadsNum, Value: strconv.Itoa(conf.Conf.Tasks.DecompressUpload.Workers), Type: consts.TypeNumber, Group: model.TRAFFIC, Flag: model.PRIVATE},
		{Key: consts.StreamMaxClientDownloadSpeed, Value: "-1", Type: consts.TypeNumber, Group: model.TRAFFIC, Flag: model.PRIVATE},
		{Key: consts.StreamMaxClientUploadSpeed, Value: "-1", Type: consts.TypeNumber, Group: model.TRAFFIC, Flag: model.PRIVATE},
		{Key: consts.StreamMaxServerDownloadSpeed, Value: "-1", Type: consts.TypeNumber, Group: model.TRAFFIC, Flag: model.PRIVATE},
		{Key: consts.StreamMaxServerUploadSpeed, Value: "-1", Type: consts.TypeNumber, Group: model.TRAFFIC, Flag: model.PRIVATE},
	}
	initialSettingItems = append(initialSettingItems, tool.Tools.Items()...)
	if global.Dev {
		initialSettingItems = append(initialSettingItems, []model.SettingItem{
			{Key: "test_deprecated", Value: "test_value", Type: consts.TypeString, Flag: model.DEPRECATED},
			{Key: "test_options", Value: "a", Type: consts.TypeSelect, Options: "a,b,c"},
			{Key: "test_help", Type: consts.TypeString, Help: "this is a help message"},
		}...)
	}
	return initialSettingItems
}