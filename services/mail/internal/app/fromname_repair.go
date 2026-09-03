package app

import (
	"context"

	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 变量不是常量：测试把它调成 1，好验「解不开的行不会堵住后面的」。
var fromNameBatch int32 = 50

// RunFromNameRepair 把存量里还是一串 =?utf-8?B?…?= 的发件人名字解开。
//
// QQ 邮箱、以及不少群发平台，把 RFC 2047 编码过的显示名再套一层引号发出来：
//
//	From: "=?utf-8?B?RnVuY3Rpb24gWWU=?=" <875172387@qq.com>
//
// 标准说引号里不该有编码词，net/mail 于是对引号里的内容原样保留，入库的
// from_name 就是那串编码，列表和详情页照着显示。解析那一步（mimeparse.go）
// 已经改成再解一次；这里管的是改之前入库的存量。
//
// 不读原件：坏的就是 from_name 这一列本身，解一下写回去。search_text 是从
// from_name 算出来的，一起重算，不然按人名搜不到这封信。队列由问题本身定义
// （from_name 里还有 =?…?=），解开一行它就离开队列；解不开的（编码或字符集
// 认不得）原样留下，按 id 游标往下走，不堵后面的。
//
// 每家公司各补一遍：启动时传进来的 SyncConfig 没有租户号，和 RunToAllBackfill
// 一个样子。
func (s *Service) RunFromNameRepair(ctx context.Context, cfg SyncConfig) {
	tenants := []int64{cfg.TenantID}
	if cfg.TenantID <= 0 {
		tenants = s.tenantsToServe(ctx)
	}
	for _, tenantID := range tenants {
		if ctx.Err() != nil {
			return
		}
		s.repairTenantFromNames(ctx, tenantID)
	}
}

func (s *Service) repairTenantFromNames(ctx context.Context, tenantID int64) {
	repaired, skipped := 0, 0
	var before *int64
	for {
		rows, err := s.q.ListInboundEncodedFromName(ctx, store.ListInboundEncodedFromNameParams{
			TenantID: tenantID, RowLimit: fromNameBatch, BeforeID: before,
		})
		if err != nil {
			s.log.Warn("sender name repair could not read a batch", "tenant", tenantID, "err", err)
			return
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			if ctx.Err() != nil {
				return
			}
			name := decodeHeader(r.FromName)
			if name == r.FromName {
				// 解不开：编码或字符集认不得。留着，别编一个。
				skipped++
				continue
			}
			if err := s.q.SetInboundFromName(ctx, store.SetInboundFromNameParams{
				TenantID: tenantID, ID: r.ID, FromName: name,
				// 和入库那一步同一个函数算出来的。
				SearchText: searchTextOf(r.Subject, name, r.FromEmail,
					firstNonEmpty(r.ToAll, r.ToEmail), r.BodyText, r.BodyHtml),
			}); err != nil {
				s.log.Warn("sender name repair could not write a row", "id", r.ID, "err", err)
				skipped++
				continue
			}
			repaired++
		}
		last := rows[len(rows)-1].ID
		before = &last
	}
	if repaired > 0 || skipped > 0 {
		s.log.Info("sender name repair finished", "tenant", tenantID, "repaired", repaired, "unrepairable", skipped)
	}
}
