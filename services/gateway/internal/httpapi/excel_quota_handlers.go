package httpapi

import (
	"encoding/json"
	"net/http"

	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
)

// 智能转换的额度：平台给每家客户公司定的每月转换次数上限。
//
// 为什么归平台管、而不是让公司自己填：每一次智能转换都是我们付给模型厂的
// 钱。让用钱的一方自己定上限，那不叫上限，叫偏好设置。所以这两条路由守的
// 是平台操作员名单——和死信台账、平台开户同一道门，同一个理由：s.perm 那
// 一套权限每家公司的超管都有，用它守平台等于没守。
//
// 公司这边看得到自己的额度和百分比（走 /api/excel-usage），改不了。

// tenantQuotaView 是平台页上的一行。
type tenantQuotaView struct {
	TenantID int64 `json:"tenantId"`
	// Limited=false 表示没设上限。**和「上限 0」是相反的两件事**，所以这
	// 一位单独存在，没有让 0 兼职表示「不限」。
	Limited       bool  `json:"limited"`
	MonthlyRuns   int64 `json:"monthlyRuns"`
	UsedThisMonth int64 `json:"usedThisMonth"`
	// 本月成本。只出现在这条平台专用的路由上——客户那一侧的 /api/excel-usage
	// 不返回金额。空串表示没配单价，前端照原样显示「未配单价」而不是 0。
	InputTokens   int64  `json:"inputTokens"`
	OutputTokens  int64  `json:"outputTokens"`
	EstimatedCost string `json:"estimatedCost"`
	Currency      string `json:"currency"`
}

func (s *Server) listExcelQuotas(w http.ResponseWriter, r *http.Request) {
	if !s.requirePlatformOperator(w, r) {
		return
	}
	resp, err := s.Emails.ListExcelQuotas(r.Context(), &mailv1.ListExcelQuotasRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	out := []tenantQuotaView{}
	for _, q := range resp.GetQuotas() {
		out = append(out, tenantQuotaView{
			TenantID: q.GetTenantId(), Limited: q.GetLimited(),
			MonthlyRuns: q.GetMonthlyRuns(), UsedThisMonth: q.GetUsedThisMonth(),
			InputTokens: q.GetInputTokens(), OutputTokens: q.GetOutputTokens(),
			EstimatedCost: q.GetEstimatedCost(), Currency: q.GetCurrency(),
		})
	}
	s.writeJSON(w, map[string]any{"quotas": out, "currentMonth": resp.GetCurrentMonth()})
}

func (s *Server) setExcelQuota(w http.ResponseWriter, r *http.Request) {
	if !s.requirePlatformOperator(w, r) {
		return
	}
	var input struct {
		TenantID int64 `json:"tenantId"`
		// 指针而不是 int64：不带这个字段表示「恢复不限」，带 0 表示「一次
		// 都不许用」。用零值区分不出这两件事，而它们的后果正好相反。
		MonthlyRuns *int64 `json:"monthlyRuns"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "BAD_JSON", "请求格式不正确")
		return
	}
	if input.TenantID <= 0 {
		s.writeError(w, http.StatusBadRequest, "MAIL_EXCEL_QUOTA_TENANT_REQUIRED", "请指定公司")
		return
	}
	req := &mailv1.SetExcelQuotaRequest{TenantId: input.TenantID}
	if input.MonthlyRuns != nil {
		req.Limited = true
		req.MonthlyRuns = *input.MonthlyRuns
	}
	if _, err := s.Emails.SetExcelQuota(r.Context(), req); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeJSON(w, map[string]any{"ok": true})
}
