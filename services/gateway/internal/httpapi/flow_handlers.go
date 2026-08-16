package httpapi

import (
	"net/http"

	apv1 "github.com/sgao19/erp-go/gen/go/erp/approval/v1"
	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

// ---------------------------------------------------------------- flows

func (s *Server) listFlows(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Approval.ListDefinitions(r.Context(), &apv1.ListDefinitionsRequest{
		BizType: r.URL.Query().Get("biz_type"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getFlow(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Approval.GetDefinition(r.Context(), &apv1.GetDefinitionRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) saveFlow(w http.ResponseWriter, r *http.Request) {
	req := &apv1.SaveDefinitionRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Approval.SaveDefinition(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// ---------------------------------------------------------------- scopes

func (s *Server) listDataScopes(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Access.ListDataScopes(r.Context(), &iamv1.ListDataScopesRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) setDataScope(w http.ResponseWriter, r *http.Request) {
	req := &iamv1.SetDataScopeRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	if req.Scope == nil {
		req.Scope = &iamv1.DataScope{}
	}
	req.Scope.RoleId = idFromPath(r)
	resp, err := s.Access.SetDataScope(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// listVisibleEmployees 为业务负责人选择器提供已经过数据范围过滤的员工列表。
// 业务页面不能先拿到全公司员工再在浏览器过滤，否则仍会泄露越权数据。
func (s *Server) listVisibleEmployees(w http.ResponseWriter, r *http.Request) {
	// 该路由只服务船期模块，模块名不能由请求者指定，否则可借用其他模块的更大范围查询员工。
	const module = "shipping"
	op, ok := grpcx.OperatorFromContext(r.Context())
	if !ok || op.EmployeeID == 0 {
		s.writeError(w, http.StatusUnauthorized, "IAM_OPERATOR_REQUIRED", "登录信息已失效")
		return
	}
	visibility, err := s.Access.VisibleEmployees(r.Context(), &iamv1.VisibleEmployeesRequest{
		EmployeeId: op.EmployeeID, Module: module,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	resp, err := s.Directory.ListEmployees(r.Context(), &iamv1.ListEmployeesRequest{
		Page: &commonv1.PageRequest{Page: 1, PageSize: 200}, Keyword: r.URL.Query().Get("keyword"), EmploymentStatus: "ACTIVE",
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if !visibility.GetAll() {
		// 部门树可能超过单页；受限范围必须先收齐候选员工，再按 IAM 返回的 ID 过滤。
		for page := int32(2); resp.GetMeta() != nil && int64(len(resp.Employees)) < resp.GetMeta().GetTotal(); page++ {
			next, nextErr := s.Directory.ListEmployees(r.Context(), &iamv1.ListEmployeesRequest{
				Page: &commonv1.PageRequest{Page: page, PageSize: 200}, Keyword: r.URL.Query().Get("keyword"), EmploymentStatus: "ACTIVE",
			})
			if nextErr != nil {
				s.writeGRPCError(w, nextErr)
				return
			}
			if len(next.Employees) == 0 {
				break
			}
			resp.Employees = append(resp.Employees, next.Employees...)
		}
		allowed := make(map[int64]struct{}, len(visibility.GetEmployeeIds()))
		for _, id := range visibility.GetEmployeeIds() {
			allowed[id] = struct{}{}
		}
		filtered := resp.Employees[:0]
		for _, employee := range resp.Employees {
			if _, ok := allowed[employee.GetId()]; ok {
				filtered = append(filtered, employee)
			}
		}
		resp.Employees = filtered
		resp.Meta = &commonv1.PageMeta{Page: 1, PageSize: int32(len(filtered)), Total: int64(len(filtered))}
	}
	s.writeProto(w, resp)
}

func (s *Server) deleteFlowBand(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Approval.DeleteBand(r.Context(), &apv1.DeleteBandRequest{
		BizType:   r.URL.Query().Get("biz_type"),
		MinAmount: r.URL.Query().Get("min_amount"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
