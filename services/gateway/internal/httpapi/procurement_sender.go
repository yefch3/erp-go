package httpapi

import (
	"context"
	"net/http"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
)

// procurementSenderFor 返回本次请求可用的采购公共发件人，不可用时把话说清楚
// 并返回 false（响应已写出）。
//
// PROCUREMENT_MAIL_SENDER_ID 是一个**全局**员工编号，而员工编号在整个系统里
// 唯一——所以那个数字只可能属于某一家公司。第二家公司拿着它去发询价，邮箱那
// 一层查不到（账号是按公司过滤的，这点是对的，不会串），报回来的却是一句关于
// 邮箱的错，看的人只会去检查自己的邮箱设置，而问题根本不在那儿。
//
// 这里先问一句「这个人是不是本公司的」，好让拒绝落在真正的原因上。查询走的是
// 调用方自己的身份，所以别家公司的员工查不到，正是我们要的答案。
//
// 这不是终局：真正的解法是每家公司各配一个采购公共邮箱，而不是全系统一个。
// 那需要一处按公司存的设置，属于采购范围，见 PR 说明。
func (s *Server) procurementSenderFor(w http.ResponseWriter, r *http.Request) (int64, bool) {
	if s.ProcurementMailSenderID <= 0 {
		s.writeError(w, http.StatusConflict,
			"NT_PROCUREMENT_SENDER_NOT_CONFIGURED", "尚未配置采购公共发件邮箱")
		return 0, false
	}
	if !s.senderBelongsToCaller(r.Context(), s.ProcurementMailSenderID) {
		s.writeError(w, http.StatusConflict, "NT_PROCUREMENT_SENDER_NOT_IN_TENANT",
			"采购公共发件邮箱配置的是其他公司的员工，本公司暂不可用，请联系系统管理员")
		return 0, false
	}
	return s.ProcurementMailSenderID, true
}

// senderBelongsToCaller 问 iam 这个员工是不是调用方公司的人。
//
// 查不到就当成「不是」：宁可多说一句「本公司不可用」，也不要把信交给一个身份
// 存疑的发件人。
func (s *Server) senderBelongsToCaller(ctx context.Context, employeeID int64) bool {
	if s.Directory == nil {
		return false
	}
	resp, err := s.Directory.GetEmployee(ctx, &iamv1.GetEmployeeRequest{Id: employeeID})
	if err != nil {
		return false
	}
	return resp.GetEmployee().GetId() == employeeID
}
