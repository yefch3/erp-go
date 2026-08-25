package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/iam/internal/store"
)

// 角色只停用、不真删，这是一个有意的选择。
//
// 角色编号被三张表和审批流的节点引用着（employee_roles、role_permissions、
// role_data_scopes，以及 approval_nodes 里 approver_type=ROLE 的 approver_ref）。
// 真删会让这些引用指向不存在的东西：审批流会变成一句「审批节点没有可用审批
// 人」，而没有任何线索指向「那个角色被删了」。停用把「不再使用」和「曾经存在」
// 两件事同时留住。
//
// 停用是真的收权：EmployeeHasPermission / ListEmployeePermissionCodes /
// WidestDataScope / ListRoleMembers 四条查询都会跳过停用的角色。这一条以前
// 不成立——status 列存在但没人读，停用等于只是从列表里藏起来，持有人权限
// 一个不少。一个说「停用」却什么都没拿走的按钮，比没有按钮更坏。
//
// 顺带：预置角色被停用之后不会被开机补种复活——补种判「有没有」看的是编码
// 在不在（任何状态），停用留着行，所以那是一个被尊重的决定。

// SetRoleStatus 停用或启用一个角色。
func (s *Service) SetRoleStatus(ctx context.Context, tenantID, roleID int64, status string) error {
	if status != "ACTIVE" && status != "INACTIVE" {
		return apierr.Invalid("IAM_ROLE_STATUS_INVALID", "角色状态只能是启用或停用")
	}
	role, err := s.q.GetRole(ctx, store.GetRoleParams{TenantID: tenantID, ID: roleID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("IAM_ROLE_NOT_FOUND", "角色不存在")
		}
		return fmt.Errorf("iam: read role: %w", err)
	}
	if role.Status == status {
		return nil // 已经是这个状态，不算错
	}

	if status == "INACTIVE" {
		// 超管停不得：它是唯一一个持有全部权限的角色，停掉之后没有人能把它
		// 启用回来——钥匙反锁进屋，而屋里没有备用钥匙。
		if role.Code == "SUPER_ADMIN" {
			return apierr.Invalid("IAM_ROLE_SUPER_ADMIN_LOCKED",
				"超级管理员角色不能停用：停用之后没有人能把它启用回来")
		}
		// 还有人持有就先拒绝，并把人数说出来。停用会当场收走这些人的权限，
		// 让他们在下一次点击时撞上「没有权限」——那时没人会把两件事联系起来。
		// 先改人、再停角色，顺序反过来就是一次无声的权限事故。
		n, err := s.q.CountRoleHolders(ctx, store.CountRoleHoldersParams{
			TenantID: tenantID, RoleID: roleID,
		})
		if err != nil {
			return fmt.Errorf("iam: count role holders: %w", err)
		}
		if n > 0 {
			return apierr.Conflict("IAM_ROLE_IN_USE",
				fmt.Sprintf("还有 %d 位在职员工持有「%s」，请先改派他们的角色再停用", n, role.Name)).
				WithMeta("holders", fmt.Sprint(n))
		}
	}

	rows, err := s.q.SetRoleStatus(ctx, store.SetRoleStatusParams{
		TenantID: tenantID, ID: roleID, Status: status,
	})
	if err != nil {
		return fmt.Errorf("iam: set role status: %w", err)
	}
	if rows == 0 {
		return apierr.NotFound("IAM_ROLE_NOT_FOUND", "角色不存在")
	}
	s.log.Info("role status changed",
		"tenant", tenantID, "role", role.Code, "from", role.Status, "to", status)
	return nil
}

// ListRolesForAdmin 列出角色管理页要看的全部角色，含停用的。
//
// 与 ListRoles 分开而不是加个开关：ListRoles 的「只有启用的」是别处依赖的
// 语义（审批按编码找角色、给员工分配角色的候选列表），加开关早晚会有人在
// 那些地方顺手传 true。
func (s *Service) ListRolesForAdmin(ctx context.Context, tenantID int64) ([]store.Role, map[int64][]string, error) {
	roles, err := s.q.ListRolesIncludingInactive(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	codes := make(map[int64][]string, len(roles))
	for _, r := range roles {
		cs, err := s.q.ListRolePermissionCodes(ctx, store.ListRolePermissionCodesParams{
			TenantID: tenantID, RoleID: r.ID,
		})
		if err != nil {
			return nil, nil, err
		}
		codes[r.ID] = cs
	}
	return roles, codes, nil
}
