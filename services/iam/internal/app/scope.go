package app

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/iam/internal/store"
)

// Visibility answers "whose documents may this person see" for one module.
// Business services ask iam rather than inventing their own idea of ownership,
// so a change to the rule lands in one place.
type Visibility struct {
	// All short-circuits everything: no id list is worth building.
	All         bool
	EmployeeIDs []int64
	ScopeType   string
}

// VisibleEmployees resolves the data scope attached to someone's roles.
//
// The default when no scope is configured is SELF, not ALL. A role nobody has
// thought about yet should reveal the least, not the most; the alternative
// fails open, which is how "we forgot to configure it" turns into a leak.
func (s *Service) VisibleEmployees(ctx context.Context, tenantID, employeeID int64, module string) (Visibility, error) {
	scope, err := s.q.WidestDataScope(ctx, store.WidestDataScopeParams{
		TenantID: tenantID, EmployeeID: employeeID, Module: module,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Visibility{}, err
	}
	scopeType := scope.ScopeType
	if errors.Is(err, pgx.ErrNoRows) || scopeType == "" {
		scopeType = "SELF"
	}

	switch scopeType {
	case "ALL":
		return Visibility{All: true, ScopeType: scopeType}, nil
	case "DEPT":
		ids, err := s.q.EmployeesInMyDept(ctx, store.EmployeesInMyDeptParams{TenantID: tenantID, ID: employeeID})
		return Visibility{EmployeeIDs: withSelf(ids, employeeID), ScopeType: scopeType}, err
	case "DEPT_AND_SUB":
		ids, err := s.q.EmployeesInMyDeptTree(ctx, store.EmployeesInMyDeptTreeParams{TenantID: tenantID, ID: employeeID})
		return Visibility{EmployeeIDs: withSelf(ids, employeeID), ScopeType: scopeType}, err
	case "CUSTOM":
		ids, err := s.q.EmployeesInDepts(ctx, store.EmployeesInDeptsParams{
			TenantID: tenantID, DeptIds: scope.CustomDeptIds,
		})
		return Visibility{EmployeeIDs: withSelf(ids, employeeID), ScopeType: scopeType}, err
	default: // SELF
		return Visibility{EmployeeIDs: []int64{employeeID}, ScopeType: "SELF"}, nil
	}
}

// withSelf guarantees people can always see their own work, whatever the
// scope says. Someone locked out of a document they created would file a bug,
// and rightly.
func withSelf(ids []int64, employeeID int64) []int64 {
	for _, id := range ids {
		if id == employeeID {
			return ids
		}
	}
	return append(ids, employeeID)
}

// ManagersOf walks the reporting line upwards. Level 1 is the direct manager,
// 2 is theirs. Returns nothing once the chain runs out, which is the honest
// answer for someone at the top: there is nobody above them to ask.
func (s *Service) ManagersOf(ctx context.Context, tenantID, employeeID int64, levels int32) ([]int64, error) {
	if levels < 1 {
		levels = 1
	}
	return s.q.ManagerAtLevel(ctx, store.ManagerAtLevelParams{
		TenantID: tenantID, ID: employeeID, Levels: levels,
	})
}

// SetManager 修改直属主管，并拒绝本人、离职主管以及任意深度的循环汇报关系。
func (s *Service) SetManager(ctx context.Context, tenantID, employeeID, managerID, operatorID int64) error {
	if managerID != 0 && managerID == employeeID {
		return apierr.Invalid("IAM_MANAGER_SELF", "不能把自己设为自己的直属主管")
	}
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		current, err := q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: employeeID})
		if err != nil {
			return apierr.NotFound("IAM_EMP_NOT_FOUND", "员工不存在")
		}
		if managerID > 0 {
			manager, err := q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: managerID})
			if err != nil || manager.Status != "ACTIVE" {
				return apierr.Invalid("IAM_MANAGER_INVALID", "直属主管必须是在职员工")
			}
			cycle, err := q.ManagerCycleExists(ctx, store.ManagerCycleExistsParams{
				TenantID: tenantID, ManagerID: managerID, EmployeeID: employeeID,
			})
			if err != nil {
				return err
			}
			if cycle {
				return apierr.Invalid("IAM_MANAGER_CYCLE", "直属主管关系不能形成循环")
			}
		}
		n, err := q.SetEmployeeManager(ctx, store.SetEmployeeManagerParams{
			TenantID: tenantID, ID: employeeID, ManagerID: managerID,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return apierr.NotFound("IAM_EMP_NOT_FOUND", "员工不存在")
		}
		after, err := q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: employeeID})
		if err != nil {
			return err
		}
		return q.InsertDirectoryChange(ctx, store.InsertDirectoryChangeParams{
			TenantID: tenantID, EntityType: "EMPLOYEE", EntityID: employeeID, Action: "SET_MANAGER",
			BeforeData: snapshotJSON(current), AfterData: snapshotJSON(after), OperatorID: operatorID,
		})
	})
}

// ListDataScopes returns every configured scope, for the administration
// screen. Roles without a row simply do not appear; the resolver treats their
// absence as SELF.
func (s *Service) ListDataScopes(ctx context.Context, tenantID int64) ([]store.ListRoleDataScopesRow, error) {
	return s.q.ListRoleDataScopes(ctx, tenantID)
}

var scopeTypes = map[string]bool{
	"SELF": true, "DEPT": true, "DEPT_AND_SUB": true, "ALL": true, "CUSTOM": true,
}

// SetDataScope 保存角色的数据范围，并把修改前后内容写入统一变更记录。
// operatorIDs 使用可选参数是为了兼容已有的内部调用；来自 API 的请求必须传入操作员工 ID。
func (s *Service) SetDataScope(ctx context.Context, tenantID, roleID int64, module, scopeType string, deptIDs []int64, operatorIDs ...int64) error {
	if roleID == 0 || module == "" {
		return apierr.Invalid("IAM_SCOPE_FIELDS_REQUIRED", "角色和模块必填")
	}
	if !scopeTypes[scopeType] {
		return apierr.Invalid("IAM_SCOPE_TYPE_INVALID", "不支持的数据范围").
			WithMeta("scope_type", scopeType)
	}
	// A custom scope naming no departments would silently behave as "only
	// myself", which is not what anyone picking CUSTOM meant.
	if scopeType == "CUSTOM" && len(deptIDs) == 0 {
		return apierr.Invalid("IAM_SCOPE_DEPTS_REQUIRED", "自定义范围必须选择部门")
	}
	var operatorID int64
	if len(operatorIDs) > 0 {
		operatorID = operatorIDs[0]
	}
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		before := map[string]any{
			"role_id": roleID, "module": module, "scope_type": "SELF", "department_ids": []int64{},
		}
		rows, err := q.ListRoleDataScopes(ctx, tenantID)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.RoleID == roleID && row.Module == module {
				before["scope_type"] = row.ScopeType
				before["department_ids"] = row.CustomDeptIds
				break
			}
		}
		if err := q.SetRoleDataScope(ctx, store.SetRoleDataScopeParams{
			TenantID: tenantID, RoleID: roleID, Module: module,
			ScopeType: scopeType, CustomDeptIds: deptIDs,
		}); err != nil {
			return err
		}
		after := map[string]any{
			"role_id": roleID, "module": module, "scope_type": scopeType, "department_ids": deptIDs,
		}
		return q.InsertDirectoryChange(ctx, store.InsertDirectoryChangeParams{
			TenantID: tenantID, EntityType: "ROLE", EntityID: roleID, Action: "SET_DATA_SCOPE",
			BeforeData: snapshotJSON(before), AfterData: snapshotJSON(after), OperatorID: operatorID,
		})
	})
}
