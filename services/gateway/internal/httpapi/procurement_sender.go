package httpapi

import (
	"context"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
)

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
