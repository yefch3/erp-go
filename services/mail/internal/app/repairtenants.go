package app

import "context"

// tenantsToRepair 是一趟修复任务要走的公司名单。
//
// 启动时传进来的 SyncConfig 没有租户号——邮件服务从 #216 起是多家公司共用
// 一个进程，withDefaults 也刻意不再把它兜底成 1。可重读原件的那几趟修复
// （raw_key 撞车、Content-ID 补全、贴图找回……）从前直接拿 cfg.TenantID 去查，
// 也就是拿着 0 查 `WHERE tenant_id = 0`：一行都查不到，静默退出，既不报错也
// 没日志。从多租户那天起它们在生产上一直是这么空跑的。
//
// 指定了租户就只跑那一家（测试用）；没指定就按 tenantsToServe 挨家跑，和
// BackfillSearchText / RunToAllBackfill 同一个样子。名单取不到时 tenantsToServe
// 返回空、这一趟空转，**不回落到「第一家公司」**——理由见那边。
func (s *Service) tenantsToRepair(ctx context.Context, cfg SyncConfig) []int64 {
	if cfg.TenantID > 0 {
		return []int64{cfg.TenantID}
	}
	return s.tenantsToServe(ctx)
}
