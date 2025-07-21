package authn

import (
	"fmt"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/setting"
	"github.com/dongdio/OpenList/v4/server/common"
)

func NewAuthnInstance(ctx *gin.Context) (*webauthn.WebAuthn, error) {
	siteUrl, err := url.Parse(common.GetApiURL(ctx.Request.Context()))
	if err != nil {
		return nil, err
	}
	return webauthn.New(&webauthn.Config{
		RPDisplayName: setting.GetStr(consts.SiteTitle),
		RPID:          siteUrl.Hostname(),
		RPOrigins:     []string{fmt.Sprintf("%s://%s", siteUrl.Scheme, siteUrl.Host)},
	})
}
