package app

import (
	"context"
	"fmt"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"os"
	"testing"
	"time"
)

func TestTemplateContactMatching(t *testing.T) {
	for _, tc := range []struct {
		a, b templateContact
		want bool
	}{
		{templateContact{email: "A@example.com"}, templateContact{email: "a@example.com"}, true},
		{templateContact{name: "Amy", phone: "+86 (010) 123"}, templateContact{name: "Amy", mobile: "+86-010-123"}, true},
		{templateContact{name: "Amy", phone: "123"}, templateContact{name: "Bob", phone: "123"}, false},
		{templateContact{name: "Amy", email: "a@example.com"}, templateContact{name: "Amy", email: "b@example.com"}, false},
	} {
		if got := sameImportContact(tc.a, tc.b); got != tc.want {
			t.Fatalf("match=%v want=%v", got, tc.want)
		}
	}
}

func TestCustomerTemplateImportIntegration(t *testing.T) {
	dsn := os.Getenv("MD_TEST_DSN")
	if dsn == "" {
		t.Skip("MD_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool)
	tenant := int64(time.Now().UnixNano()%100000000 + 10000)
	defer func() {
		for _, table := range []string{"customer_change_logs", "customer_custom_field_values", "customer_contacts", "customer_owners", "customer_addresses", "customers", "customer_field_definitions"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenant)
			_, _ = pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenant+1)
		}
	}()
	sales := WithCustomerAccess(ctx, 11)
	row := CustomerImportRow{Code: "IMPORT-TEST", Name: "Template Test", ShortName: "TT", CountryCode: "CN", CountryRegion: "中国", Address: "Test office", PostalCode: "001001", CompanyPhone: "01012345678", CompanyEmail: "office@example.com", CreditGrade: "A", ArchiveCreator: "Source Author", ContactName: "Amy", ContactEmail: "amy@example.com", ContactPhone: "01011111111", OwnerEmployeeIDs: []int64{12}, OwnerNames: []string{"Sales B"}}
	second := row
	second.ContactName = "Bob"
	second.ContactEmail = "bob@example.com"
	second.ContactPhone = "01022222222"
	run := func(c context.Context, tenantID int64, rows []CustomerImportRow, dry bool) ([]CustomerImportVerdict, int32) {
		t.Helper()
		v, n, e := svc.ImportCustomerTemplate(c, tenantID, rows, nil, dry, 11, "Sales A")
		if e != nil {
			t.Fatal(e)
		}
		return v, n
	}
	v, n := run(sales, tenant, []CustomerImportRow{row, second}, true)
	if !v[0].OK || !v[1].OK || n != 0 {
		t.Fatalf("preview: %#v", v)
	}
	v, n = run(sales, tenant, []CustomerImportRow{row, second}, false)
	if n != 2 {
		t.Fatalf("import: %#v count=%d", v, n)
	}
	var id int64
	if err = pool.QueryRow(ctx, "SELECT id FROM customers WHERE tenant_id=$1 AND code=$2", tenant, row.Code).Scan(&id); err != nil {
		t.Fatal(err)
	}
	assertCount := func(query string, want int) {
		t.Helper()
		var count int
		if err := pool.QueryRow(ctx, query, tenant, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Fatalf("%s count=%d want=%d", query, count, want)
		}
	}
	assertCount("SELECT count(*) FROM customers WHERE tenant_id=$1 AND id=$2", 1)
	assertCount("SELECT count(*) FROM customer_contacts WHERE tenant_id=$1 AND customer_id=$2", 2)
	assertCount("SELECT count(*) FROM customer_contacts WHERE tenant_id=$1 AND customer_id=$2 AND is_primary", 1)
	assertCount("SELECT count(*) FROM customer_owners WHERE tenant_id=$1 AND customer_id=$2", 2)
	basic, err := svc.GetCustomerBasic(sales, tenant, id)
	if err != nil || basic.CompanyEmail != row.CompanyEmail || basic.PostalCode != "001001" || basic.ArchiveCreator != "Source Author" {
		t.Fatalf("basic=%#v err=%v", basic, err)
	}
	v, n = run(sales, tenant, []CustomerImportRow{row}, false)
	if n != 0 || v[0].OK || !v[0].ExistingCustomer || !v[0].DuplicateContact {
		t.Fatalf("requires confirmation %#v", v)
	}
	update := row
	update.CustomerAction = "UPDATE"
	update.ContactAction = "UPDATE"
	update.CompanyEmail = ""
	update.ShortName = "Updated"
	update.ContactPhone = ""
	update.ContactMobile = "13800138000"
	v, n = run(sales, tenant, []CustomerImportRow{update}, false)
	if n != 1 {
		t.Fatalf("update %#v", v)
	}
	basic, _ = svc.GetCustomerBasic(sales, tenant, id)
	if basic.CompanyEmail != row.CompanyEmail || basic.ShortName != "Updated" {
		t.Fatalf("blank overwrite %#v", basic)
	}
	var phone, mobile string
	if err = pool.QueryRow(ctx, "SELECT phone,mobile FROM customer_contacts WHERE tenant_id=$1 AND customer_id=$2 AND email='amy@example.com'", tenant, id).Scan(&phone, &mobile); err != nil || phone != row.ContactPhone || mobile != "13800138000" {
		t.Fatalf("contact update phone=%s mobile=%s err=%v", phone, mobile, err)
	}
	assertCount("SELECT count(*) FROM customer_contacts WHERE tenant_id=$1 AND customer_id=$2", 2)
	v, n = run(WithCustomerAccess(ctx, 99), tenant, []CustomerImportRow{update}, false)
	if n != 0 || v[0].OK || v[0].ExistingCustomer {
		t.Fatalf("unauthorized leak %#v", v)
	}
	if _, err = svc.GetCustomerBasic(WithCustomerAccess(ctx, 99), tenant, id); err == nil {
		t.Fatal("unauthorized basic read")
	}
	if _, err = svc.SaveCustomerBasic(WithCustomerAccess(ctx, 99), tenant, id, basic, 99, "Other"); err == nil {
		t.Fatal("unauthorized basic write")
	}
	v, n = run(sales, tenant+1, []CustomerImportRow{row}, false)
	if n != 1 {
		t.Fatalf("tenant isolation %#v", v)
	}
	bad := row
	bad.Code = "BATCH-BAD"
	bad.Name = "Bad"
	bad.ContactName = ""
	bad.ContactEmail = "not-an-email"
	good := row
	good.Code = "BATCH-GOOD"
	good.Name = "Good"
	// Email identifies a contact even when its name is not yet known.
	good.ContactName = ""
	v, n = run(sales, tenant, []CustomerImportRow{good, bad}, false)
	if n != 1 || !v[0].OK || v[1].OK {
		t.Fatalf("valid row was not imported independently: %#v count=%d", v, n)
	}
	var count int
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM customers WHERE tenant_id=$1 AND code LIKE 'BATCH-%'", tenant).Scan(&count)
	if count != 1 {
		t.Fatalf("partial import count=%d want=1", count)
	}
	shortOnly := CustomerImportRow{ShortName: "Short Name Only"}
	v, n = run(sales, tenant, []CustomerImportRow{shortOnly}, false)
	if n != 1 || !v[0].OK || v[0].Name != shortOnly.ShortName {
		t.Fatalf("short-name-only customer import: %#v count=%d", v, n)
	}
	// A repeated contact in a new customer batch can explicitly update the prior row.
	first := row
	first.Code = "BATCH-CONTACT"
	first.Name = "Batch Contact"
	next := first
	next.ContactMobile = "13900000000"
	next.ContactAction = "UPDATE"
	v, n = run(sales, tenant, []CustomerImportRow{first, next}, false)
	if n != 2 {
		t.Fatal(fmt.Sprint(v))
	}
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM customer_contacts WHERE tenant_id=$1 AND customer_id=(SELECT id FROM customers WHERE tenant_id=$1 AND code='BATCH-CONTACT')`, tenant).Scan(&count)
	if count != 1 {
		t.Fatal("in-batch contact update duplicated")
	}
}
