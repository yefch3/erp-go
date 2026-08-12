package app

import (
	"context"
	"os"
	"testing"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// TestCustomerRelationshipLifecycle 覆盖 B2 最容易出错的三件事：主要联系人唯一、
// 同员工同职责不能重复，以及每次关系变化都能在客户历史中追溯。
func TestCustomerRelationshipLifecycle(t *testing.T) {
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
	customer, _, err := svc.CreateCustomer(ctx, 1, CustomerInput{Name: "Relationship Lifecycle Probe", CountryCode: "CN"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM customer_change_logs WHERE tenant_id=1 AND customer_id=$1", customer.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM customer_owners WHERE tenant_id=1 AND customer_id=$1", customer.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM customer_contacts WHERE tenant_id=1 AND customer_id=$1", customer.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM customers WHERE tenant_id=1 AND id=$1", customer.ID)
	}()

	first, err := svc.CreateCustomerContact(ctx, 1, customer.ID, CustomerContactInput{Name: "Alice", Email: "alice@example.com", IsPrimary: true, OperatorID: 9, OperatorName: "测试员"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateCustomerContact(ctx, 1, customer.ID, CustomerContactInput{Name: "Bob", IsPrimary: true, OperatorID: 9, OperatorName: "测试员"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateCustomerContact(ctx, 1, customer.ID, CustomerContactInput{
		Name: "Opted Out", Email: "optedout@example.com", EmailPermission: "OPTED_OUT",
		OperatorID: 9, OperatorName: "测试员",
	}); err != nil {
		t.Fatal(err)
	}
	mailingContacts, err := svc.ListMailingContacts(ctx, 1, "optedout", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(mailingContacts) != 0 {
		t.Fatalf("opted-out contact leaked into mail picker: %#v", mailingContacts)
	}
	contacts, err := svc.ListCustomerContactsDetailed(ctx, 1, customer.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(contacts) != 3 || contacts[0].ID != second.ID || !contacts[0].IsPrimary || contacts[1].ID != first.ID || contacts[1].IsPrimary {
		t.Fatalf("primary contact switch failed: %#v", contacts)
	}
	if _, err := svc.UpdateCustomerProfile(ctx, 1, customer.ID, CustomerProfileInput{BusinessStatus: "COOPERATING"}); err == nil {
		t.Fatal("customer without owner should not become cooperating")
	}

	owner, err := svc.CreateCustomerOwner(ctx, 1, customer.ID, CustomerOwnerInput{EmployeeID: 77, EmployeeName: "王业务", ResponsibilityCode: "SALES", OperatorID: 9, OperatorName: "测试员"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateCustomerOwner(ctx, 1, customer.ID, CustomerOwnerInput{EmployeeID: 77, EmployeeName: "王业务", ResponsibilityCode: "SALES"}); err == nil {
		t.Fatal("duplicate owner responsibility should be rejected")
	}
	if _, err := svc.UpdateCustomerProfile(ctx, 1, customer.ID, CustomerProfileInput{BusinessStatus: "COOPERATING", OperatorID: 9, OperatorName: "测试员"}); err != nil {
		t.Fatalf("customer with country and owner should become cooperating: %v", err)
	}
	if err := svc.DeactivateCustomerOwner(ctx, 1, customer.ID, owner.ID, "", 9, "测试员"); err != nil {
		t.Fatal(err)
	}
	changes, total, err := svc.ListCustomerChanges(ctx, 1, customer.ID, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total < 4 || len(changes) < 4 {
		t.Fatalf("expected relationship history, total=%d rows=%d", total, len(changes))
	}
}

// TestCustomerImportAllOrNothing 确认预检和正式导入使用同一套规则，
// 任意一行错误时整批数据都不会写入数据库。
func TestCustomerImportAllOrNothing(t *testing.T) {
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
	rows := []CustomerImportRow{
		{Code: "B2-IMPORT-OK", Name: "B2 Import Valid Probe", CountryCode: "US"},
		{Code: "B2-IMPORT-BAD", Name: "B2 Import Invalid Probe", CountryCode: "USA"},
	}
	verdicts, imported, err := svc.ImportCustomers(ctx, 1, rows, false, 9, "测试员")
	if err != nil {
		t.Fatal(err)
	}
	if imported != 0 || len(verdicts) != 2 || !verdicts[0].OK || verdicts[1].OK {
		t.Fatalf("unexpected import verdicts: imported=%d verdicts=%#v", imported, verdicts)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM customers WHERE tenant_id=1 AND code IN ('B2-IMPORT-OK','B2-IMPORT-BAD')").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("invalid batch wrote %d customer rows", count)
	}
}

func TestCustomerContactValidation(t *testing.T) {
	for _, input := range []CustomerContactInput{
		{},
		{Name: "Alice", Email: "not-an-email"},
		{Name: "Alice", Phone: "call-me"},
		{Name: "Alice", EmailPermission: "UNKNOWN"},
		{Name: "Alice", EmailCategories: []string{"UNKNOWN"}},
	} {
		if err := input.normalizeAndValidate(); err == nil {
			t.Fatalf("expected invalid contact: %#v", input)
		}
	}

	valid := CustomerContactInput{
		Name: " Alice ", Email: "alice@example.com", Phone: "+1 (212) 555-0100",
		EmailCategories: []string{"shipping", "SHIPPING", "business"},
	}
	if err := valid.normalizeAndValidate(); err != nil {
		t.Fatalf("valid contact: %v", err)
	}
	if valid.EmailPermission != "ALLOWED" || len(valid.EmailCategories) != 2 {
		t.Fatalf("normalized email preferences = %q/%#v", valid.EmailPermission, valid.EmailCategories)
	}
}
