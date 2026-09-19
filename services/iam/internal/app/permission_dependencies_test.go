package app

import (
	"slices"
	"testing"
)

func TestExpandPermissionCodesAddsPagePrerequisites(t *testing.T) {
	known := map[string]bool{
		"procurement:receipt:write": true,
		"procurement:order:read":    true,
		"shipping:document:upload":  true,
		"shipping:document:view":    true,
		"shipping:schedule:read":    true,
	}
	got := expandPermissionCodes([]string{"procurement:receipt:write", "shipping:document:upload"}, known)
	for _, want := range []string{
		"procurement:order:read",
		"shipping:document:view",
		"shipping:schedule:read",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("expanded permissions missing %s: %v", want, got)
		}
	}
}

func TestInspectionRequestDoesNotOpenQualityWorkbench(t *testing.T) {
	known := map[string]bool{
		"quality:task:request": true,
		"quality:task:read":    true,
	}
	got := expandPermissionCodes([]string{"quality:task:request"}, known)
	if slices.Contains(got, "quality:task:read") {
		t.Fatalf("inspection request must not grant the quality workbench: %v", got)
	}
}

func TestPresetRolesContainEveryRequiredPermission(t *testing.T) {
	for _, role := range presetRoles {
		granted := make(map[string]bool, len(role.Permissions))
		for _, code := range role.Permissions {
			granted[code] = true
		}
		for code := range granted {
			for _, required := range permissionDependencies[code] {
				if !granted[required] {
					t.Errorf("preset role %s grants %s but misses prerequisite %s", role.Code, code, required)
				}
			}
		}
	}
}
