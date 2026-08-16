package app

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/shipping/internal/store"
)

const shippingScopeModule = "shipping"

// Scopes 统一向 IAM 询问当前人员的数据范围，船期服务不自行解释组织架构。
type Scopes interface {
	VisibleEmployees(ctx context.Context, employeeID int64, module string) (Visibility, error)
}

// CustomerAccess 只查询负责人对应的客户 ID，不复制客户敏感资料。
type CustomerAccess interface {
	CustomerIDsOwnedBy(ctx context.Context, employeeIDs []int64) ([]int64, error)
}

type Visibility struct {
	All         bool
	EmployeeIDs []int64
	CustomerIDs []int64
	ScopeType   string
}

// UseAccessControl 在生产启动时注入 IAM 和客户负责人查询端口。
// 测试未注入时保持旧有的全量行为，便于独立验证纯业务规则。
func (s *Service) UseAccessControl(scopes Scopes, customers CustomerAccess) {
	s.scopes = scopes
	s.customerAccess = customers
}

func (s *Service) visibleTo(ctx context.Context, op Operator) (Visibility, error) {
	if s.scopes == nil {
		return Visibility{All: true, ScopeType: "ALL"}, nil
	}
	visible, err := s.scopes.VisibleEmployees(ctx, op.ID, shippingScopeModule)
	if err != nil {
		return Visibility{}, err
	}
	if visible.All || s.customerAccess == nil || len(visible.EmployeeIDs) == 0 {
		return visible, nil
	}
	visible.CustomerIDs, err = s.customerAccess.CustomerIDsOwnedBy(ctx, visible.EmployeeIDs)
	return visible, err
}

func containsID(ids []int64, target int64) bool {
	if target == 0 {
		return false
	}
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func visibleSchedule(v Visibility, schedule store.ShippingSchedule) bool {
	if v.All || containsID(v.EmployeeIDs, schedule.ResponsibleEmployeeID) {
		return true
	}
	return schedule.CustomerID != nil && containsID(v.CustomerIDs, *schedule.CustomerID)
}

// authorizeSchedule 对越权和不存在统一返回“船期不存在”，避免泄露其他部门的数据。
func (s *Service) authorizeSchedule(ctx context.Context, tenantID, scheduleID int64, op Operator) (store.ShippingSchedule, error) {
	schedule, err := s.q.GetSchedule(ctx, store.GetScheduleParams{TenantID: tenantID, ID: scheduleID})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ShippingSchedule{}, apierr.NotFound("SHIPPING_NOT_FOUND", "船期不存在")
	}
	if err != nil {
		return store.ShippingSchedule{}, err
	}
	visible, err := s.visibleTo(ctx, op)
	if err != nil {
		return store.ShippingSchedule{}, err
	}
	if !visibleSchedule(visible, schedule) {
		return store.ShippingSchedule{}, apierr.NotFound("SHIPPING_NOT_FOUND", "船期不存在")
	}
	return schedule, nil
}

// authorizeAssignment 防止普通员工把船期转给范围外人员，也防止主管跨部门指派。
func (s *Service) authorizeAssignment(ctx context.Context, responsibleEmployeeID int64, op Operator) error {
	visible, err := s.visibleTo(ctx, op)
	if err != nil {
		return err
	}
	if visible.All || containsID(visible.EmployeeIDs, responsibleEmployeeID) {
		return nil
	}
	return apierr.Permission("SHIPPING_RESPONSIBLE_OUT_OF_SCOPE", "负责人不在当前用户可管理的数据范围内")
}
