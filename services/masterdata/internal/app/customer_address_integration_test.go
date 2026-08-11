package app

import (
	"context"
	"os"
	"testing"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// TestCustomerAddressLifecycle 使用真实 PostgreSQL 验证默认地址切换和软停用。
// 未配置 MD_TEST_DSN 时跳过，CI 或本地集成测试可显式开启。
func TestCustomerAddressLifecycle(t *testing.T) {
	dsn := os.Getenv("MD_TEST_DSN")
	if dsn == "" {
		t.Skip("MD_TEST_DSN not set; skipping DB-backed test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool)

	customer, _, err := svc.CreateCustomer(ctx, 1, CustomerInput{Name: "Address Lifecycle Probe"})
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM customer_change_logs WHERE tenant_id = 1 AND customer_id = $1", customer.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM customer_addresses WHERE tenant_id = 1 AND customer_id = $1", customer.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM customers WHERE tenant_id = 1 AND id = $1", customer.ID)
	}()

	first, err := svc.CreateCustomerAddress(ctx, 1, customer.ID, CustomerAddressInput{
		AddressType: "SHIPPING", CountryCode: "US", AddressLine: "First address", IsDefault: true,
	})
	if err != nil {
		t.Fatalf("create first address: %v", err)
	}
	second, err := svc.CreateCustomerAddress(ctx, 1, customer.ID, CustomerAddressInput{
		AddressType: "SHIPPING", CountryCode: "US", AddressLine: "Second address", IsDefault: true,
	})
	if err != nil {
		t.Fatalf("create second address: %v", err)
	}

	rows, err := svc.ListCustomerAddresses(ctx, 1, customer.ID, "ALL")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].IsDefault || !rows[1].IsDefault {
		t.Fatalf("default address switch failed: %#v", rows)
	}

	if err := svc.DeactivateCustomerAddress(ctx, 1, customer.ID, second.ID, 9); err != nil {
		t.Fatalf("deactivate address: %v", err)
	}
	active, err := svc.ListCustomerAddresses(ctx, 1, customer.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != first.ID {
		t.Fatalf("active addresses after deactivate: %#v", active)
	}
}

// TestLegacyCustomerUpdatePreservesProfile 验证旧版基础信息表单不会清空详细资料。
func TestLegacyCustomerUpdatePreservesProfile(t *testing.T) {
	dsn := os.Getenv("MD_TEST_DSN")
	if dsn == "" {
		t.Skip("MD_TEST_DSN not set; skipping DB-backed test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool)

	customer, _, err := svc.CreateCustomer(ctx, 1, CustomerInput{Name: "Profile Compatibility Probe"})
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM customer_change_logs WHERE tenant_id = 1 AND customer_id = $1", customer.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM customers WHERE tenant_id = 1 AND id = $1", customer.ID)
	}()

	if _, err := svc.UpdateCustomerProfile(ctx, 1, customer.ID, CustomerProfileInput{
		ShortName: "Profile Saved", Tags: []string{"重点客户"}, CreditCurrency: "cny",
	}); err != nil {
		t.Fatalf("update profile: %v", err)
	}
	if _, _, err := svc.UpdateCustomer(ctx, 1, customer.ID, CustomerInput{
		Name: "Legacy Form Saved", Currency: "USD",
	}); err != nil {
		t.Fatalf("legacy update: %v", err)
	}

	got, _, err := svc.GetCustomer(ctx, 1, customer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ShortName != "Profile Saved" || len(got.Tags) != 1 || got.Tags[0] != "重点客户" || got.CreditCurrency != "CNY" {
		t.Fatalf("profile was changed by legacy update: %#v", got)
	}
}

// TestCustomerCountryClassification 验证国家和“未分类”筛选均由数据库分页处理。
func TestCustomerCountryClassification(t *testing.T) {
	dsn := os.Getenv("MD_TEST_DSN")
	if dsn == "" {
		t.Skip("MD_TEST_DSN not set; skipping DB-backed test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool)

	classified, _, err := svc.CreateCustomer(ctx, 1, CustomerInput{
		Name: "Country Classification Probe", CountryCode: "zw",
	})
	if err != nil {
		t.Fatalf("create classified customer: %v", err)
	}
	unclassified, _, err := svc.CreateCustomer(ctx, 1, CustomerInput{Name: "Unclassified Probe"})
	if err != nil {
		t.Fatalf("create unclassified customer: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM customers WHERE tenant_id = 1 AND id = ANY($1)", []int64{classified.ID, unclassified.ID})
	}()

	zwRows, _, err := svc.ListCustomers(ctx, 1, "Country Classification Probe", "", "ZW", "", "", "", 0, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(zwRows) != 1 || zwRows[0].ID != classified.ID {
		t.Fatalf("country filter returned %#v", zwRows)
	}
	emptyRows, _, err := svc.ListCustomers(ctx, 1, "Unclassified Probe", "", "__UNCLASSIFIED__", "", "", "", 0, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(emptyRows) != 1 || emptyRows[0].ID != unclassified.ID {
		t.Fatalf("unclassified filter returned %#v", emptyRows)
	}

	groups, err := svc.ListCustomerCountries(ctx, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	foundZW, foundEmpty := false, false
	for _, group := range groups {
		foundZW = foundZW || group.Code == "ZW"
		foundEmpty = foundEmpty || group.Code == ""
	}
	if !foundZW || !foundEmpty {
		t.Fatalf("country groups missing classified or unclassified: %#v", groups)
	}
}
