package httpapi

import (
	"encoding/json"
	"net/http"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
)

// 平台开户页的四个动作。守卫（platform_operators 名单）在 iam 内部：这里不加
// s.perm(...)——普通权限每家公司的超管都有，用它守平台等于没守；名单校验网关
// 复述一遍也只是第二份会过期的规则。网关只转发身份，iam 说不行就是不行。

func (s *Server) platformMe(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Platform.CheckOperator(r.Context(), &iamv1.CheckOperatorRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) platformTenants(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Platform.ListPlatformTenants(r.Context(), &iamv1.ListPlatformTenantsRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) platformCreateTenant(w http.ResponseWriter, r *http.Request) {
	req := &iamv1.CreatePlatformTenantRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Platform.CreatePlatformTenant(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	// 公司已开出。邀请信从操作员自己绑定的邮箱发出——和员工邀请同一条路。
	// 发信失败不撤开户：前端据 mailSent 提示「开户成功但邮件没发出去」，
	// 页面上的「重发邀请」就是为这一刻准备的。
	sent, sendErr := s.sendActivation(r, resp.GetInvitation())
	s.writePlatformResult(w, resp.GetTenant(), sent, sendErr)
}

func (s *Server) platformReinvite(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Platform.ReinvitePlatformAdmin(r.Context(), &iamv1.ReinvitePlatformAdminRequest{
		TenantId: idFromPath(r),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	sent, sendErr := s.sendActivation(r, resp.GetInvitation())
	s.writePlatformResult(w, nil, sent, sendErr)
}

func (s *Server) platformSetTenantStatus(w http.ResponseWriter, r *http.Request) {
	req := &iamv1.SetPlatformTenantStatusRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.TenantId = idFromPath(r)
	resp, err := s.Platform.SetPlatformTenantStatus(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// sendActivation 把一张已铸好的邀请寄出去，走调用者自己的邮箱。
// 返回是否已入队，以及一句给人看的失败原因（成功时为空）。
func (s *Server) sendActivation(r *http.Request, inv *iamv1.InviteEmployeeResponse) (bool, string) {
	subject, body := activationMail(inv.GetName(), s.activationLink(inv.GetToken()))
	out, err := s.Emails.CreateCampaign(r.Context(), &mailv1.CreateCampaignRequest{
		Subject: subject, Body: body,
		BodyFormat: "TEXT", Kind: "TRANSACTIONAL",
		Recipients: []*mailv1.Recipient{{Name: inv.GetName(), Email: inv.GetEmail()}},
	})
	if err != nil {
		return false, "激活链接已生成，但邮件发送失败：" + statusMessage(err)
	}
	if out.GetQueued() == 0 {
		reason := "该地址未能进入发送队列"
		if sup := out.GetSuppressed(); len(sup) > 0 {
			reason = "该地址在拒收名单中（" + sup[0].GetReason() + "），邮件未发送"
		}
		return false, reason
	}
	return true, ""
}

// writePlatformResult 拼「开户/重邀的结果 + 邮件是否寄出」这一种响应。
// 不复用 writeProto 是因为它要在 proto 消息旁边多背两个布尔/文案字段。
func (s *Server) writePlatformResult(w http.ResponseWriter, tenant *iamv1.PlatformTenant, sent bool, sendErr string) {
	payload := map[string]any{"mailSent": sent}
	if sendErr != "" {
		payload["mailError"] = sendErr
	}
	if tenant != nil {
		if raw, err := pj.Marshal(tenant); err == nil {
			payload["tenant"] = json.RawMessage(raw)
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "GATEWAY_MARSHAL", "响应编码失败")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(envelope{Success: true, Data: data})
}
