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
		"SALES":               {"sales:inquiry:write", "export:quotation:write"},
		"SALES_MANAGER":       {"sales:inquiry:write", "export:quotation:write", "export:contract:approve"},
		"BUYER":               {"procurement:sourcing:write", "procurement:sourcing:price"},
		"PROCUREMENT_MANAGER": {"procurement:sourcing:approve", "procurement:order:cancel"},
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
