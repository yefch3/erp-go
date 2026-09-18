package app

import (
	"strings"
	"testing"
)

func TestSalesPresetRolesDoNotGrantProcurementOperations(t *testing.T) {
	for _, code := range []string{"SALES", "SALES_MANAGER"} {
		var permissions []string
		for _, role := range presetRoles {
			if role.Code == code {
				permissions = role.Permissions
				break
			}
		}
		if len(permissions) == 0 {
			t.Fatalf("missing preset role %s", code)
		}
		for _, permission := range permissions {
			if strings.HasPrefix(permission, "procurement:") {
				t.Fatalf("%s must not receive procurement operation permission %s", code, permission)
			}
		}
	}
}

func TestSalesAndProcurementPresetRoleBoundary(t *testing.T) {
	required := map[string][]string{
		"BOSS":                {"approval:task:act", "procurement:order:read"},
		"SALES":               {"sales:inquiry:write", "export:quotation:write", "masterdata:port:read", "masterdata:customer:write"},
		"SALES_MANAGER":       {"sales:inquiry:write", "export:quotation:write", "export:contract:approve", "masterdata:port:read", "masterdata:customer:write"},
		"BUYER":               {"procurement:sourcing:write", "procurement:sourcing:price", "masterdata:factory:read"},
		"PROCUREMENT_MANAGER": {"procurement:sourcing:approve", "procurement:order:cancel", "masterdata:factory:read"},
	}
	for roleCode, wants := range required {
		var permissions []string
		for _, role := range presetRoles {
			if role.Code == roleCode {
				permissions = role.Permissions
				break
			}
		}
		granted := make(map[string]bool, len(permissions))
		for _, permission := range permissions {
			granted[permission] = true
		}
		for _, want := range wants {
			if !granted[want] {
				t.Errorf("%s missing required permission %s", roleCode, want)
			}
		}
		if strings.HasPrefix(roleCode, "PROCUREMENT") || roleCode == "BUYER" {
			for _, forbidden := range []string{"sales:inquiry:write", "sales:inquiry:submit", "export:quotation:write"} {
				if granted[forbidden] {
					t.Errorf("%s unexpectedly received sales responsibility %s", roleCode, forbidden)
				}
			}
		}
	}
}

func TestQualityModuleIsRestrictedToQualityDepartment(t *testing.T) {
	roles := make(map[string]map[string]bool, len(presetRoles))
	for _, role := range presetRoles {
		roles[role.Code] = make(map[string]bool, len(role.Permissions))
		for _, permission := range role.Permissions {
			roles[role.Code][permission] = true
		}
	}

	for _, code := range []string{"BOSS", "SALES", "SALES_MANAGER", "LOGISTICS", "SHIPPING_MANAGER", "BUYER", "PROCUREMENT_MANAGER"} {
		if roles[code]["quality:task:read"] {
			t.Errorf("%s must not receive access to the quality module", code)
		}
	}
	for _, permission := range []string{"quality:task:read", "quality:task:write", "quality:file:upload"} {
		if !roles["QUALITY_INSPECTOR"][permission] {
			t.Errorf("QUALITY_INSPECTOR missing %s", permission)
		}
	}
	for _, code := range []string{"BUYER", "PROCUREMENT_MANAGER"} {
		if !roles[code]["quality:task:request"] {
			t.Errorf("%s must retain the purchase-order inspection request action", code)
		}
	}
}

func TestOperationalPresetRoleScopesExposeSharedWorkQueues(t *testing.T) {
	tests := []struct {
		roleCode string
		module   string
		want     string
	}{
		// 采购员处理全公司的采购需求，但采购单仍只看本人负责的单据。
		{"BUYER", "procurement_requirement", "ALL"},
		{"BUYER", "procurement_order", "SELF"},
		// 采购经理需要查看并审批团队全部采购单和采购需求。
		{"PROCUREMENT_MANAGER", "procurement_requirement", "ALL"},
		{"PROCUREMENT_MANAGER", "procurement_order", "ALL"},
		// 物流需要查看全公司的采购单才能执行收货。
		{"LOGISTICS", "procurement_order", "ALL"},
	}

	for _, tt := range tests {
		t.Run(tt.roleCode+"/"+tt.module, func(t *testing.T) {
			got := ""
			for _, role := range presetRoles {
				if role.Code != tt.roleCode {
					continue
				}
				for _, scope := range role.Scopes {
					if scope.Module == tt.module {
						got = scope.Type
						break
					}
				}
				break
			}
			if got != tt.want {
				t.Fatalf("%s scope for %s = %q, want %q", tt.module, tt.roleCode, got, tt.want)
			}
		})
	}
}
