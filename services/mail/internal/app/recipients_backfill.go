package app

import (
	"context"
	"time"

	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 变量不是常量：测试把它调成 1，好验「补不了的行不会堵住后面的」。
var toAllBatch int32 = 50

// 每批之间歇一下。每一行都要从对象存储里读一次原件再解析，而这是在补
// 历史——历史已经等了这么久，不差这几秒。
var toAllInterval = 2 * time.Second

// RunToAllBackfill 把 00052 之前入库的信的收件人清单从原件里补回来。
//
// 入库那一步从前只留 To 里的第一个地址（见 mimeparse.go 的 ToAll），客户群发
// 给七个同事的信在库里变成了「发给一个人」。原件一直在对象存储里——这正是
// 它存在的理由之一——所以这不是丢了，只是没读出来。
//
// 队列是一条查询而不是标记列：一行合格的条件就是 to_all 还空着，补上它就
// 离开队列。补不回来的行（原件读不到、只有密送的信）留在原地，**按 id 游标
// 往下走**，不会堵住后面的——RunContentIDBackfill 那道「整批没进展就停」的
// 闸在这里会出事：前 50 行恰好都补不了，后面几千行就永远轮不到。补不了的
// 下次服务重启再试一遍，反正读一次原件很便宜。
func (s *Service) RunToAllBackfill(ctx context.Context, cfg SyncConfig) {
	cfg = cfg.withDefaults()
	if s.files == nil {
		return
	}
	repaired, skipped := 0, 0
	var before *int64
	for {
		rows, err := s.q.ListInboundNeedingToAll(ctx, store.ListInboundNeedingToAllParams{
			TenantID: cfg.TenantID, RowLimit: toAllBatch, BeforeID: before,
		})
		if err != nil {
			s.log.Warn("recipients backfill could not read a batch", "err", err)
			return
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			if ctx.Err() != nil {
				return
			}
			if s.repairToAll(ctx, cfg.TenantID, r.ID, r.RawKey, r.ToEmail) {
				repaired++
			} else {
				skipped++
			}
		}
		last := rows[len(rows)-1].ID
		before = &last
		select {
		case <-ctx.Done():
			return
		case <-time.After(toAllInterval):
		}
	}
	if repaired > 0 || skipped > 0 {
		s.log.Info("recipients backfill finished", "repaired", repaired, "unrepairable", skipped)
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
		// 和入库那一步同一个函数算出来的：老信和新信按同一份文本搜。
		SearchText: searchTextOf(parsed.Subject, parsed.FromName, parsed.FromEmail,
			toAll, parsed.BodyText, parsed.BodyHTML),
	}); err != nil {
		s.log.Warn("recipients backfill could not write the recipients back", "id", inboundID, "err", err)
		return false
	}
	return true
}
