package config

import "testing"

func TestSellerHasNoImplicitCompany(t *testing.T) {
	t.Setenv("SELLER_NAME", "")
	t.Setenv("SELLER_ADDRESS", "")
	cfg := Load()
	if cfg.SellerName != "" || cfg.SellerAddress != "" {
		t.Fatalf("unconfigured seller must remain blank: %q %q", cfg.SellerName, cfg.SellerAddress)
	}
	t.Setenv("SELLER_NAME", " Actual seller ")
	t.Setenv("SELLER_ADDRESS", " Actual address ")
	cfg = Load()
	if cfg.SellerName != "Actual seller" || cfg.SellerAddress != "Actual address" {
		t.Fatal("explicit seller configuration was not retained")
	}
}
