package app

import (
	"context"
	"time"

	"github.com/sgao19/erp-go/services/mail/internal/store"
)

const (
	toAllBatch = 50
	// 每批之间歇一下。每一行都要从对象存储里读一次原件再解析，而这是在补
	// 历史——历史已经等了这么久，不差这几秒。
	toAllInterval = 2 * time.Second
)

// RunToAllBackfill 把 00052 之前入库的信的收件人清单从原件里补回来。
//
// 入库那一步从前只留 To 里的第一个地址（见 mimeparse.go 的 ToAll），客户群发
// 给七个同事的信在库里变成了「发给一个人」。原件一直在对象存储里——这正是
// 它存在的理由之一——所以这不是丢了，只是没读出来。
//
// 队列是一条查询而不是标记列：一行合格的条件就是 to_all 还空着，补上它就
// 离开队列。补不回来的行（原件读不到、To 头本来就空、只有密送的信）会一直
// 留在队列里，所以整批没进展就停——和 RunContentIDBackfill 同一道闸。
func (s *Service) RunToAllBackfill(ctx context.Context, cfg SyncConfig) {
	cfg = cfg.withDefaults()
	if s.files == nil {
		return
	}
	repaired := 0
	for {
		rows, err := s.q.ListInboundNeedingToAll(ctx, store.ListInboundNeedingToAllParams{
			TenantID: cfg.TenantID, RowLimit: toAllBatch,
		})
		if err != nil {
			s.log.Warn("recipients backfill could not read a batch", "err", err)
			return
		}
		if len(rows) == 0 {
			break
		}
		before := repaired
		for _, r := range rows {
			if ctx.Err() != nil {
				return
			}
			if s.repairToAll(ctx, cfg.TenantID, r.ID, r.RawKey, r.ToEmail) {
				repaired++
			}
		}
		if repaired == before {
			s.log.Info("recipients backfill stopping: the remaining messages cannot be repaired",
				"repaired", repaired, "left", len(rows))
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(toAllInterval):
		}
	}
	if repaired > 0 {
		s.log.Info("recipients backfill finished", "repaired", repaired)
	}
}

// repairToAll 重读一封信的原件，把整段 To 头写回去。
//
// 原件里 To 头是空的（只有密送的信会这样）时，写 to_email：那是当年从
// 同一个头里取出来的第一个地址，两者一致；写空串的话这一行永远留在队列里。
// 两个都空就真的补不了，如实返回 false。
func (s *Service) repairToAll(ctx context.Context, tenantID, inboundID int64, rawKey, toEmail string) bool {
	raw, err := s.readRaw(ctx, rawKey)
	if err != nil {
		s.log.Warn("recipients backfill could not read the original message",
			"id", inboundID, "err", err)
		return false
	}
	parsed, err := ParseMail(raw)
	if err != nil {
		s.log.Warn("recipients backfill could not parse the original message",
			"id", inboundID, "err", err)
		return false
	}
	toAll := parsed.ToAll
	if toAll == "" {
		toAll = toEmail
	}
	if toAll == "" {
		return false
	}
	if err := s.q.SetInboundToAll(ctx, store.SetInboundToAllParams{
		TenantID: tenantID, ID: inboundID, ToAll: toAll,
	}); err != nil {
		s.log.Warn("recipients backfill could not write the recipients back", "id", inboundID, "err", err)
		return false
	}
	return true
}
