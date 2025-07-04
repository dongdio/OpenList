package initialize

import (
	"context"

	"golang.org/x/time/rate"

	"github.com/dongdio/OpenList/v4/consts"
	"github.com/dongdio/OpenList/v4/internal/op"
	"github.com/dongdio/OpenList/v4/internal/setting"
	"github.com/dongdio/OpenList/v4/utility/stream"
)

type blockBurstLimiter struct {
	*rate.Limiter
}

func (l blockBurstLimiter) WaitN(ctx context.Context, total int) error {
	for total > 0 {
		n := l.Burst()
		if l.Limiter.Limit() == rate.Inf || n > total {
			n = total
		}
		err := l.Limiter.WaitN(ctx, n)
		if err != nil {
			return err
		}
		total -= n
	}
	return nil
}

func streamFilterNegative(limit int) (rate.Limit, int) {
	if limit < 0 {
		return rate.Inf, 0
	}
	return rate.Limit(limit) * 1024.0, limit * 1024
}

func initLimiter(limiter *stream.Limiter, s string) {
	clientDownLimit, burst := streamFilterNegative(setting.GetInt(s, -1))
	*limiter = blockBurstLimiter{Limiter: rate.NewLimiter(clientDownLimit, burst)}
	op.RegisterSettingChangingCallback(func() {
		newLimit, newBurst := streamFilterNegative(setting.GetInt(s, -1))
		(*limiter).SetLimit(newLimit)
		(*limiter).SetBurst(newBurst)
	})
}

func initStreamLimit() {
	initLimiter(&stream.ClientDownloadLimit, consts.StreamMaxClientDownloadSpeed)
	initLimiter(&stream.ClientUploadLimit, consts.StreamMaxClientUploadSpeed)
	initLimiter(&stream.ServerDownloadLimit, consts.StreamMaxServerDownloadSpeed)
	initLimiter(&stream.ServerUploadLimit, consts.StreamMaxServerUploadSpeed)
}