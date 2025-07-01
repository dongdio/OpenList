package authn

import (
	"context"
	"fmt"
	"net/url"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/dongdio/OpenList/consts"
	"github.com/dongdio/OpenList/internal/setting"
	"github.com/dongdio/OpenList/server/common"
)

func NewAuthnInstance(ctx context.Context) (*webauthn.WebAuthn, error) {
	siteUrl, err := url.Parse(common.GetApiUrl(ctx))
	if err != nil {
		return nil, err
	}
	return webauthn.New(&webauthn.Config{
		RPDisplayName: setting.GetStr(consts.SiteTitle),
		RPID:          siteUrl.Hostname(),
		RPOrigins:     []string{fmt.Sprintf("%s://%s", siteUrl.Scheme, siteUrl.Host)},
	})
}