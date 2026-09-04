package app

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/iam/internal/store"
)

// presetRoles 是每家公司开张即有的六个预置角色，逐条取自给第一家公司播种的
// 迁移：角色本身建于 00011（物流）/00013（财务）/00033（采购两个），授权散在
// 00012/00016/00033/00034/00036/00038/00039/00040 等十几个迁移里。
//
// 这张表和迁移的一致**由测试钉住**，不靠人对眼：集成测试把新公司的每个预置
// 角色和同一个库里第一家公司的同名角色逐权限码对比——测试库每次都从迁移
// 现刷，第一家就是迁移的纯产物。以后谁给某个角色加了权限迁移，这张表没跟上，
// 测试当场红。这比编码规则那张表的「注释请两边同改」更硬。
//
// 为什么值得播：角色页确实能手建，但采购单的兜底审批按 PROCUREMENT_MANAGER
// 编码找角色（见 approval），没有它，第二家公司的第一张「提交人没有上级」的
// 采购单就停在一句「请先创建该角色」上。四个一起补，是因为它们本来就是第一家
// 公司开箱即有的东西——第二家没理由从零开始。
var presetRoles = []presetRole{
	{
		Code: "SALES", Name: "销售专员",
		Description: "维护本人客户询盘、客户报价和出口合同",
		Permissions: []string{
			"export:contract:read", "export:contract:write",
			"iam:employee:read", "masterdata:supplier:read",
			"export:quotation:read", "export:quotation:write",
			"masterdata:customer:read", "masterdata:port:read", "product:product:read",
			"sales:inquiry:read", "sales:inquiry:submit", "sales:inquiry:write",
			"sales:procurement-progress:read",
		},
		Scopes: []presetScope{
			{"export", "SELF"}, {"procurement_sourcing", "SELF"},
		},
	},
	{
		Code: "SALES_MANAGER", Name: "销售经理",
		Description: "管理团队客户询盘、客户报价、合同审批和负责人转移",
		Permissions: []string{
			"export:contract:approve", "export:contract:read", "export:contract:write",
			"iam:employee:read", "masterdata:supplier:read",
			"export:ownership:transfer", "export:quotation:read", "export:quotation:write",
			"masterdata:customer:read", "masterdata:port:read", "product:product:read",
			"sales:inquiry:read", "sales:inquiry:submit", "sales:inquiry:write",
			"sales:procurement-progress:read",
		},
		Scopes: []presetScope{
			{"export", "DEPT_AND_SUB"}, {"procurement_sourcing", "DEPT_AND_SUB"},
		},
	},
	{
		Code: "LOGISTICS", Name: "物流管理",
		Description: "仓储与发运：入库、出库、采购收货、库存查询",
		Permissions: []string{
			"export:contract:read", "export:shipment:read", "export:shipment:write",
			"inventory:stock:read", "inventory:stock:write",
			"masterdata:supplier:read", "masterdata:port:read",
			"procurement:exception:write", "procurement:order:read", "procurement:receipt:write",
			"product:product:read",
			"shipping:progress:write", "shipping:route:write",
			"shipping:schedule:read", "shipping:schedule:write",
			"shipping:sourcing:read", "shipping:sourcing:write",
		},
		Scopes: []presetScope{
			{"export", "ALL"}, {"mail", "SELF"},
			{"procurement_order", "SELF"}, {"procurement_requirement", "SELF"},
			{"procurement_sourcing", "SELF"}, {"shipping", "ALL"},
		},
	},
	{
		Code: "SHIPPING_MANAGER", Name: "船运经理",
		Description: "管理售前船运询价、主责人员、统一方案与正式船期",
		Permissions: []string{
			"shipping:sourcing:read", "shipping:sourcing:write", "shipping:sourcing:approve",
			"shipping:schedule:read", "shipping:schedule:write",
			"masterdata:supplier:read", "masterdata:port:read", "product:product:read",
		},
		Scopes: []presetScope{{"shipping", "ALL"}, {"procurement_sourcing", "ALL"}},
	},
	{
		Code: "FINANCE", Name: "财务",
		Description: "收款登记与合同核销、银行流水对账",
		Permissions: []string{
			"export:contract:read", "export:receipt:read", "export:receipt:write",
			"export:shipment:read",
			"fx:rate:read",
			"masterdata:customer:read", "masterdata:port:read",
			"procurement:payment:read", "procurement:payment:write",
			"procurement:recon:read", "procurement:recon:write",
		},
		Scopes: []presetScope{
			{"export", "ALL"}, {"mail", "SELF"},
			// 采购单范围必须是 ALL：财务不是任何一张采购单的 buyer，SELF 对它
			// 就等于零行，供应商对账页会打得开却一张单都没有，且不报错。
			// 和 00050 给 1 号租户改的那一条是同一件事——这里管的是新开的租户。
			{"procurement_order", "ALL"}, {"procurement_requirement", "SELF"},
			{"procurement_sourcing", "SELF"},
		},
	},
	{
		Code: "BUYER", Name: "采购专员",
		Description: "询价、匹配采购需求、维护采购单草稿并提交审批",
		Permissions: []string{
			"masterdata:factory:read", "masterdata:supplier:read",
			"procurement:exception:write",
			"procurement:order:read", "procurement:order:send",
			"procurement:order:submit", "procurement:order:write",
			"procurement:production:write", "procurement:requirement:read",
			"procurement:sourcing:price", "procurement:sourcing:read",
			"procurement:sourcing:send", "procurement:sourcing:write",
			"product:product:read",
		},
		Scopes: []presetScope{
			{"procurement_order", "SELF"}, {"procurement_requirement", "SELF"},
			{"procurement_sourcing", "ALL"},
		},
	},
	{
		Code: "PROCUREMENT_MANAGER", Name: "采购经理",
		Description: "管理采购询价、例外需求、采购单审批与取消",
		Permissions: []string{
			"approval:task:act",
			"masterdata:factory:read", "masterdata:supplier:read",
			"procurement:exception:write",
			"procurement:order:cancel", "procurement:order:read",
			"procurement:order:send", "procurement:order:submit", "procurement:order:write",
			"procurement:payment:read", "procurement:production:write",
			"procurement:requirement:exception", "procurement:requirement:read",
			"procurement:requirement:write",
			"procurement:sourcing:approve", "procurement:sourcing:price",
			"procurement:sourcing:read", "procurement:sourcing:send",
			"procurement:sourcing:write",
			"product:product:read",
		},
		Scopes: []presetScope{
			{"procurement_order", "SELF"}, {"procurement_requirement", "SELF"},
			{"procurement_sourcing", "ALL"},
		},
	},
}

type presetRole struct {
	Code        string
	Name        string
	Description string
	Permissions []string
	Scopes      []presetScope
}

type presetScope struct {
	Module string
	Type   string
}

// seedPresetRoles 给一家公司补上缺失的预置角色。成员不播——谁担任采购经理是
// 这家公司自己的决定，播出来的是空角色，等管理员往里加人。
//
// 判「缺失」只看角色编码在不在，任何状态都算：角色只能停用、不能删除，所以
// 「编码不存在」无歧义地等于「从没播过」；而「有但被人改过权限」的角色一个
// 字都不碰——默认值只填空。
func (s *Service) seedPresetRoles(ctx context.Context, q *store.Queries, tenantID int64) error {
	for _, pr := range presetRoles {
		exists, err := q.RoleExistsByCode(ctx, store.RoleExistsByCodeParams{
			TenantID: tenantID, Code: pr.Code,
		})
		if err != nil {
			return fmt.Errorf("iam: check preset role %s: %w", pr.Code, err)
		}
		if exists {
			continue
		}
		role, err := q.CreateRole(ctx, store.CreateRoleParams{
			TenantID: tenantID, Code: pr.Code, Name: pr.Name, Description: pr.Description,
		})
		if err != nil {
			return fmt.Errorf("iam: create preset role %s: %w", pr.Code, err)
		}
		ids, err := q.GetPermissionIDsByCodes(ctx, pr.Permissions)
		if err != nil {
			return fmt.Errorf("iam: resolve permissions for %s: %w", pr.Code, err)
		}
		// 找不齐就整体失败，不静默少给：表里的权限码写错（或权限被改名）时，
		// 一个静默缺了几项权限的角色会以「某人打不开某页」的形式在几周后
		// 出现，没人会想到根子在这里。
		if len(ids) != len(pr.Permissions) {
			return fmt.Errorf("iam: preset role %s names %d permissions but only %d exist — the table has drifted from the permissions catalogue",
				pr.Code, len(pr.Permissions), len(ids))
		}
		for _, pid := range ids {
			if err := q.AddRolePermission(ctx, store.AddRolePermissionParams{
				TenantID: tenantID, RoleID: role.ID, PermissionID: pid,
			}); err != nil {
				return fmt.Errorf("iam: grant %s: %w", pr.Code, err)
			}
		}
		for _, sc := range pr.Scopes {
			if err := q.SetRoleDataScope(ctx, store.SetRoleDataScopeParams{
				TenantID: tenantID, RoleID: role.ID, Module: sc.Module,
				ScopeType: sc.Type, CustomDeptIds: []int64{},
			}); err != nil {
				return fmt.Errorf("iam: scope %s/%s: %w", pr.Code, sc.Module, err)
			}
		}
	}
	return nil
}

// EnsurePresetRoles 在启动时给**已经存在**的公司补齐预置角色。
//
// 新公司在 seedTenantCore 里当场播；这个扫一遍是给此改动之前开出的公司
// （生产上的 263 测试公司）以及任何将来漏播的情况兜底。幂等：编码在就跳过，
// 每次启动跑一遍的代价是每家公司四次存在性查询。
func (s *Service) EnsurePresetRoles(ctx context.Context) error {
	tenants, err := s.q.ListTenantIDs(ctx)
	if err != nil {
		return fmt.Errorf("iam: list tenants: %w", err)
	}
	for _, tenantID := range tenants {
		err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
			return s.seedPresetRoles(ctx, s.q.WithTx(tx), tenantID)
		})
		if err != nil {
			return fmt.Errorf("iam: preset roles for tenant %d: %w", tenantID, err)
		}
	}
	return nil
}
