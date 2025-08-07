package server

import (
	"context"
	"net/http"
	"time"

	"github.com/OpenListTeam/sftpd-openlist"
	"golang.org/x/crypto/ssh"

	"github.com/dongdio/OpenList/v4/utility/errs"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/global"
	"github.com/dongdio/OpenList/v4/internal/conf"
	"github.com/dongdio/OpenList/v4/internal/model"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/internal/setting"
	"github.com/dongdio/OpenList/v4/server/ftp"
	"github.com/dongdio/OpenList/v4/server/sftp"
	"github.com/dongdio/OpenList/v4/utility/utils"
)

type SftpDriver struct {
	proxyHeader http.Header
	config      *sftpd.Config
}

func NewSftpDriver() (*SftpDriver, error) {
	sftp.InitHostKey(global.DataDir)
	return &SftpDriver{
		proxyHeader: http.Header{
			"User-Agent": {setting.GetStr(consts.FTPProxyUserAgent)},
		},
	}, nil
}

func (d *SftpDriver) GetConfig() *sftpd.Config {
	if d.config != nil {
		return d.config
	}
	serverConfig := ssh.ServerConfig{
		NoClientAuth:         true,
		NoClientAuthCallback: d.NoClientAuth,
		PasswordCallback:     d.PasswordAuth,
		PublicKeyCallback:    d.PublicKeyAuth,
		AuthLogCallback:      d.AuthLogCallback,
		BannerCallback:       d.GetBanner,
	}
	for _, k := range sftp.SSHSigners {
		serverConfig.AddHostKey(k)
	}
	d.config = &sftpd.Config{
		ServerConfig: serverConfig,
		HostPort:     conf.Conf.SFTP.Listen,
		ErrorLogFunc: utils.Log.Error,
		// DebugLogFunc: utils.Log.Debugf,
	}
	return d.config
}

func (d *SftpDriver) GetFileSystem(sc *ssh.ServerConn) (sftpd.FileSystem, error) {
	userObj, err := op.GetUserByName(sc.User())
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	ctx = context.WithValue(ctx, consts.UserKey, userObj)
	ctx = context.WithValue(ctx, consts.MetaPassKey, "")
	ctx = context.WithValue(ctx, consts.ClientIPKey, sc.RemoteAddr().String())
	ctx = context.WithValue(ctx, consts.ProxyHeaderKey, d.proxyHeader)
	return &sftp.DriverAdapter{FtpDriver: ftp.NewAferoAdapter(ctx)}, nil
}

func (d *SftpDriver) Close() {
}

func (d *SftpDriver) NoClientAuth(conn ssh.ConnMetadata) (*ssh.Permissions, error) {
	if conn.User() != "guest" {
		return nil, errs.New("only guest is allowed to login without authorization")
	}
	guest, err := op.GetGuest()
	if err != nil {
		return nil, err
	}
	if guest.Disabled || !guest.CanFTPAccess() {
		return nil, errs.New("user is not allowed to access via SFTP")
	}
	return nil, nil
}

func (d *SftpDriver) PasswordAuth(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
	userObj, err := op.GetUserByName(conn.User())
	if err != nil {
		return nil, err
	}
	if userObj.Disabled || !userObj.CanFTPAccess() {
		return nil, errs.New("user is not allowed to access via SFTP")
	}
	passHash := model.StaticHash(string(password))
	if err = userObj.ValidatePwdStaticHash(passHash); err != nil {
		return nil, err
	}
	return nil, nil
}

func (d *SftpDriver) PublicKeyAuth(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	userObj, err := op.GetUserByName(conn.User())
	if err != nil {
		return nil, err
	}
	if userObj.Disabled || !userObj.CanFTPAccess() {
		return nil, errs.New("user is not allowed to access via SFTP")
	}
	keys, _, err := op.GetSSHPublicKeyByUserID(userObj.ID, 1, -1)
	if err != nil {
		return nil, err
	}
	marshal := string(key.Marshal())
	for _, sk := range keys {
		if marshal != sk.KeyStr {
			pubKey, _, _, _, e := ssh.ParseAuthorizedKey([]byte(sk.KeyStr))
			if e != nil || marshal != string(pubKey.Marshal()) {
				continue
			}
		}
		sk.LastUsedTime = time.Now()
		_ = op.UpdateSSHPublicKey(&sk)
		return nil, nil
	}
	return nil, errs.New("public key refused")
}

func (d *SftpDriver) AuthLogCallback(conn ssh.ConnMetadata, method string, err error) {
	ip := conn.RemoteAddr().String()
	if err == nil {
		utils.Log.Infof("[SFTP] %s(%s) logged in via %s", conn.User(), ip, method)
	} else if method != "none" {
		utils.Log.Infof("[SFTP] %s(%s) tries logging in via %s but with error: %s", conn.User(), ip, method, err)
	}
}

func (d *SftpDriver) GetBanner(_ ssh.ConnMetadata) string {
	return setting.GetStr(consts.Announcement)
}