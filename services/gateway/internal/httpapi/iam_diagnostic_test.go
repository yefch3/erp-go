package httpapi

import "testing"

func TestEmployeeUsesScopeOnlyForAccessibleModules(t *testing.T) {
	permissions := map[string]bool{
		"export:contract:read":         true,
		"procurement:requirement:read": true,
		"masterdata:customer:read":     true,
	}

	for _, module := range []string{"export", "procurement_requirement"} {
		if !employeeUsesScope(module, permissions) {
			t.Fatalf("%s should be relevant to the employee's effective permissions", module)
		}
	}
	for _, module := range []string{"shipping", "quality", "mail", "procurement_sourcing", "procurement_order"} {
		if employeeUsesScope(module, permissions) {
			t.Fatalf("%s should not create a missing-scope warning for an inaccessible module", module)
		}
	}
}
