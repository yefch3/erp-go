package app

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
)

type TodoReception struct {
	ApprovalsOnly    bool
	Categories       []string
	EmployeeOverride bool
}

var todoCategories = []string{"SALES_INQUIRY", "FACTORY_INQUIRY", "LOGISTICS_INQUIRY", "EXECUTION_PROCUREMENT", "QUALITY"}

// Defaults apply to built-in roles until an administrator saves an explicit
// setting. Role names and employee job titles play no part in routing.
func defaultTodoReception(code string) TodoReception {
	switch code {
	case "BOSS", "SUPER_ADMIN":
		return TodoReception{ApprovalsOnly: true}
	case "SALES", "SALES_MANAGER":
		return TodoReception{Categories: []string{"SALES_INQUIRY"}}
	case "BUYER", "PROCUREMENT_MANAGER":
		return TodoReception{Categories: []string{"FACTORY_INQUIRY", "EXECUTION_PROCUREMENT", "QUALITY"}}
	case "LOGISTICS", "SHIPPING_MANAGER":
		return TodoReception{Categories: []string{"LOGISTICS_INQUIRY"}}
	case "QUALITY_INSPECTOR":
		return TodoReception{Categories: []string{"QUALITY"}}
	}
	return TodoReception{}
}

func roleTodoReception(code string, configured, approvalsOnly bool, categories []string) TodoReception {
	if !configured {
		return defaultTodoReception(code)
	}
	return TodoReception{ApprovalsOnly: approvalsOnly, Categories: categories}
}

func mergeTodoReception(personal bool, roles []TodoReception) TodoReception {
	result := TodoReception{ApprovalsOnly: personal, EmployeeOverride: personal}
	for _, role := range roles {
		result.ApprovalsOnly = result.ApprovalsOnly || role.ApprovalsOnly
	}
	if result.ApprovalsOnly {
		return result
	}
	for _, category := range todoCategories {
		for _, role := range roles {
			if slices.Contains(role.Categories, category) {
				result.Categories = append(result.Categories, category)
				break
			}
		}
	}
	return result
}

func (s *Service) GetRoleTodoReception(ctx context.Context, tenantID, roleID int64) (TodoReception, error) {
	var code string
	var configured, approvalsOnly bool
	var categories []string
	err := s.pool.QueryRow(ctx, `SELECT r.code, p.role_id IS NOT NULL,
  COALESCE(p.approvals_only,false), COALESCE(p.categories,ARRAY[]::text[])
  FROM roles r LEFT JOIN role_todo_reception p ON p.tenant_id=r.tenant_id AND p.role_id=r.id
  WHERE r.tenant_id=$1 AND r.id=$2`, tenantID, roleID).Scan(&code, &configured, &approvalsOnly, &categories)
	if errors.Is(err, pgx.ErrNoRows) {
		return TodoReception{}, apierr.NotFound("IAM_ROLE_NOT_FOUND", "角色不存在")
	}
	if err != nil {
		return TodoReception{}, err
	}
	return roleTodoReception(code, configured, approvalsOnly, categories), nil
}

func (s *Service) GetEmployeeTodoReception(ctx context.Context, tenantID, employeeID int64) (TodoReception, error) {
	var personal bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM employee_todo_reception p
  WHERE p.tenant_id=e.tenant_id AND p.employee_id=e.id)
  FROM employees e WHERE e.tenant_id=$1 AND e.id=$2`, tenantID, employeeID).Scan(&personal)
	if errors.Is(err, pgx.ErrNoRows) {
		return TodoReception{}, apierr.NotFound("IAM_EMPLOYEE_NOT_FOUND", "员工不存在")
	}
	if err != nil {
		return TodoReception{}, err
	}
	if personal {
		return mergeTodoReception(true, nil), nil
	}
	rows, err := s.pool.Query(ctx, `SELECT r.code, p.role_id IS NOT NULL,
  COALESCE(p.approvals_only,false), COALESCE(p.categories,ARRAY[]::text[])
  FROM employee_roles er JOIN roles r ON r.tenant_id=er.tenant_id AND r.id=er.role_id
  LEFT JOIN role_todo_reception p ON p.tenant_id=r.tenant_id AND p.role_id=r.id
  WHERE er.tenant_id=$1 AND er.employee_id=$2 AND r.status='ACTIVE'`, tenantID, employeeID)
	if err != nil {
		return TodoReception{}, err
	}
	defer rows.Close()
	var roles []TodoReception
	for rows.Next() {
		var code string
		var configured, approvalsOnly bool
		var categories []string
		if err := rows.Scan(&code, &configured, &approvalsOnly, &categories); err != nil {
			return TodoReception{}, err
		}
		roles = append(roles, roleTodoReception(code, configured, approvalsOnly, categories))
	}
	return mergeTodoReception(false, roles), rows.Err()
}

func (s *Service) SetRoleTodoReception(ctx context.Context, tenantID, roleID int64, setting TodoReception) (TodoReception, error) {
	for _, category := range setting.Categories {
		if !slices.Contains(todoCategories, category) {
			return TodoReception{}, apierr.Invalid("IAM_TODO_CATEGORY", "不支持的待办类别")
		}
	}
	// Keep only registered categories, in display order, and remove duplicates.
	categories := append([]string{}, mergeTodoReception(false, []TodoReception{{Categories: setting.Categories}}).Categories...)
	reg, err := s.pool.Exec(ctx, `INSERT INTO role_todo_reception (tenant_id,role_id,approvals_only,categories)
  SELECT tenant_id,id,$3,$4 FROM roles WHERE tenant_id=$1 AND id=$2
  ON CONFLICT (tenant_id,role_id) DO UPDATE SET approvals_only=EXCLUDED.approvals_only,categories=EXCLUDED.categories`, tenantID, roleID, setting.ApprovalsOnly, categories)
	if err != nil {
		return TodoReception{}, err
	}
	if reg.RowsAffected() == 0 {
		return TodoReception{}, apierr.NotFound("IAM_ROLE_NOT_FOUND", "角色不存在")
	}
	setting.Categories = categories
	return setting, nil
}

func (s *Service) SetEmployeeTodoReception(ctx context.Context, tenantID, employeeID int64, approvalsOnly bool) (TodoReception, error) {
	var err error
	if approvalsOnly {
		_, err = s.pool.Exec(ctx, `INSERT INTO employee_todo_reception (tenant_id,employee_id)
   SELECT tenant_id,id FROM employees WHERE tenant_id=$1 AND id=$2 ON CONFLICT DO NOTHING`, tenantID, employeeID)
	} else {
		_, err = s.pool.Exec(ctx, `DELETE FROM employee_todo_reception WHERE tenant_id=$1 AND employee_id=$2`, tenantID, employeeID)
	}
	if err != nil {
		return TodoReception{}, err
	}
	return s.GetEmployeeTodoReception(ctx, tenantID, employeeID)
}
