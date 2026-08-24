package app

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sgao19/erp-go/pkg/apierr"
)

func TestNormalizeWarehouseInputDefaults(t *testing.T) {
	in, err := normalizeWarehouseInput(WarehouseInput{
		Code:        " WH-SH-01 ",
		Name:        " 上海港仓 ",
		ProfileType: "PORT",
		Contacts: []WarehouseContactInput{{
			ContactType: "CONTACT",
			Name:        " 港口联系人 ",
		}},
	})
	if err != nil {
		t.Fatalf("normalize warehouse: %v", err)
	}
	if in.Code != "WH-SH-01" || in.Name != "上海港仓" {
		t.Fatalf("expected trimmed code and name, got code=%q name=%q", in.Code, in.Name)
	}
	if in.Timezone != "UTC" || in.AccountingMode != "SYNC_INVENTORY" || in.Status != "ACTIVE" {
		t.Fatalf("unexpected defaults: timezone=%q accounting=%q status=%q", in.Timezone, in.AccountingMode, in.Status)
	}
	if in.Contacts[0].Status != "ACTIVE" {
		t.Fatalf("expected active contact, got %q", in.Contacts[0].Status)
	}
}

func TestNormalizeWarehouseInputRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		in   WarehouseInput
	}{
		{name: "missing required fields", in: WarehouseInput{ProfileType: "OWN"}},
		{name: "invalid profile type", in: WarehouseInput{Code: "WH-1", Name: "仓库", ProfileType: "VIRTUAL"}},
		{name: "invalid accounting mode", in: WarehouseInput{Code: "WH-1", Name: "仓库", ProfileType: "OWN", AccountingMode: "UNKNOWN"}},
		{name: "invalid contact", in: WarehouseInput{Code: "WH-1", Name: "仓库", ProfileType: "OWN", Contacts: []WarehouseContactInput{{ContactType: "CONTACT"}}}},
		{name: "owner without employee", in: WarehouseInput{Code: "WH-1", Name: "仓库", ProfileType: "OWN", Contacts: []WarehouseContactInput{{ContactType: "OWNER", Name: "负责人"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := normalizeWarehouseInput(tt.in); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestProfileToWHTypeUsesValuesAllowedByDatabase(t *testing.T) {
	tests := map[string]string{
		"OWN":         "NORMAL",
		"THIRD_PARTY": "NORMAL",
		"PORT":        "PORT_TERMINAL",
	}
	for profile, want := range tests {
		if got := profileToWHType(profile); got != want {
			t.Errorf("profileToWHType(%q) = %q, want %q", profile, got, want)
		}
	}
}

func TestTranslateWarehouseWriteError(t *testing.T) {
	err := translateWarehouseWriteError(&pgconn.PgError{
		Code:           "23505",
		ConstraintName: "warehouses_tenant_id_code_key",
	})
	if got := apierr.CodeFromError(err); got != "WAREHOUSE_CODE_TAKEN" {
		t.Fatalf("business code = %q, want WAREHOUSE_CODE_TAKEN", got)
	}
}
