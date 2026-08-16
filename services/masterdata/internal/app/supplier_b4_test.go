package app

import "testing"

func TestNormalizeSupplierInput(t *testing.T) {
	in := SupplierInput{NameEn: " Demo Supplier ", CountryCode: "us", BusinessTypes: []string{"carrier", "CARRIER", "forwarder", "customs_broker", "warehouse"}}
	if err := normalizeSupplierInput(&in); err != nil {
		t.Fatalf("normalize supplier: %v", err)
	}
	if in.Name != "Demo Supplier" || in.CountryCode != "US" {
		t.Fatalf("unexpected normalized supplier: %#v", in)
	}
	if len(in.BusinessTypes) != 4 || in.BusinessTypes[0] != "CARRIER" || in.BusinessTypes[1] != "FORWARDER" || in.BusinessTypes[2] != "CUSTOMS_BROKER" || in.BusinessTypes[3] != "WAREHOUSE" {
		t.Fatalf("unexpected business types: %#v", in.BusinessTypes)
	}
}

func TestNormalizeFactoryInput(t *testing.T) {
	in := FactoryInput{SupplierID: 9, NameZh: " 测试工厂 ", CountryCode: "cn", Status: "cooperating", Timezone: "Asia/Shanghai"}
	if err := normalizeFactoryInput(&in); err != nil {
		t.Fatalf("normalize factory: %v", err)
	}
	if in.NameZh != "测试工厂" || in.CountryCode != "CN" || in.Status != "COOPERATING" {
		t.Fatalf("unexpected normalized factory: %#v", in)
	}
}

func TestNormalizeFactoryRequiresActiveLocation(t *testing.T) {
	in := FactoryInput{SupplierID: 9, NameEn: "Factory", Status: "COOPERATING"}
	if err := normalizeFactoryInput(&in); err == nil {
		t.Fatal("cooperating factory without country and timezone should be rejected")
	}
}

func TestImportRowNumberKeepsCSVLine(t *testing.T) {
	if got := importRowNumber(17, 0); got != 17 {
		t.Fatalf("explicit row number changed: %d", got)
	}
	if got := importRowNumber(0, 3); got != 5 {
		t.Fatalf("fallback row number = %d, want 5", got)
	}
}
