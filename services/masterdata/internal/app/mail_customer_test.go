package app

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestMailCustomerRegistration(t *testing.T) {
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
	tenant := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"customer_mail_links", "customer_change_logs", "customer_owners", "customer_contacts", "customers", "number_sequences", "number_rules"} {
			if _, e := pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenant); e != nil {
				t.Error(e)
			}
		}
	}()
	if _, err = pool.Exec(ctx, `INSERT INTO number_rules(tenant_id,biz_type,prefix,period,seq_len) VALUES($1,'CUSTOMER','CU','NONE',4)`, tenant); err != nil {
		t.Fatal(err)
	}
	base := SaveMailCustomerInput{InboundID: 1, Action: "CREATE", CompanyName: "Mail Test Ltd.", Email: "one@example.test", OperatorID: 7, OperatorName: "Sales"}
	first, err := svc.SaveMailCustomer(ctx, tenant, base)
	if err != nil {
		t.Fatal(err)
	}
	if first.Customer.Name != base.CompanyName || first.Contact.Name != "" || first.Contact.ID == 0 {
		t.Fatalf("bad email-only contact: %+v", first)
	}
	// Retry after a lost response must preserve both the company and contact.
	retry, err := svc.SaveMailCustomer(ctx, tenant, base)
	if err != nil || retry.Customer.ID != first.Customer.ID || retry.Contact.ID != first.Contact.ID {
		t.Fatalf("retry duplicated: %+v %v", retry, err)
	}
	link, err := svc.GetMailCustomerLink(ctx, tenant, 1)
	if err != nil || link.Contact.ID != first.Contact.ID {
		t.Fatalf("missing link: %+v %v", link, err)
	}
	missing, err := svc.GetMailCustomerLink(ctx, tenant+1, 1)
	if err != nil || missing.Customer.ID != 0 {
		t.Fatalf("cross tenant link: %+v %v", missing, err)
	}
	if _, err = svc.GetMailCustomerLink(WithCustomerAccess(ctx, 99), tenant, 1); err == nil {
		t.Fatal("unowned customer link disclosed")
	}
	input := base
	input.InboundID = 2
	input.CustomerID = first.Customer.ID
	input.Action = "ADD"
	input.Email = "alice@example.test"
	input.ContactName = "Alice"
	added, err := svc.SaveMailCustomer(ctx, tenant, input)
	if err != nil {
		t.Fatal(err)
	}
	input.InboundID = 3
	input.Email = "alice.new@example.test"
	_, err = svc.SaveMailCustomer(ctx, tenant, input)
	var businessErr *apierr.Error
	if !errors.As(err, &businessErr) || businessErr.Code != "MD_MAIL_SAME_NAME" {
		t.Fatalf("same name not guarded: %v", err)
	}
	input.Action = "SUPPLEMENT"
	input.ContactID = added.Contact.ID
	supplemented, err := svc.SaveMailCustomer(ctx, tenant, input)
	if err != nil {
		t.Fatal(err)
	}
	if supplemented.Contact.Email != "alice@example.test" || !contactHasEmail(supplemented.Contact, input.Email) {
		t.Fatalf("original email overwritten: %+v", supplemented.Contact)
	}
	matches, err := svc.CheckCustomerDuplicates(ctx, tenant, "", "", input.Email, 0)
	if err != nil || len(matches) != 1 || matches[0].MatchFields[len(matches[0].MatchFields)-1] != "EMAIL" {
		t.Fatalf("additional email not matched: %+v %v", matches, err)
	}
	input.Action = "USE"
	input.InboundID = 4
	if _, err = svc.SaveMailCustomer(ctx, tenant, input); err != nil {
		t.Fatal(err)
	}
	input.CustomerID = first.Customer.ID
	input.InboundID = 5
	input.ContactID = first.Contact.ID
	if _, err = svc.SaveMailCustomer(ctx, tenant, input); err == nil {
		t.Fatal("used contact with wrong email")
	}
	input = base
	input.InboundID = 6
	input.Email = "another@example.test"
	if _, err = svc.SaveMailCustomer(ctx, tenant, input); err == nil {
		t.Fatal("created duplicate company")
	}
	input = base
	input.Action = "ADD"
	input.InboundID = 7
	input.CustomerID = first.Customer.ID
	if _, err = svc.SaveMailCustomer(WithCustomerAccess(ctx, 99), tenant, input); err == nil {
		t.Fatal("added contact to unowned customer")
	}
	if _, err = svc.SaveMailCustomer(ctx, tenant+1, input); err == nil {
		t.Fatal("cross tenant write")
	}
	// Concurrent new messages for one new contact create exactly one person.
	var wg sync.WaitGroup
	ids := make(chan int64, 4)
	failures := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v := base
			v.Action = "ADD"
			v.CustomerID = first.Customer.ID
			v.InboundID = int64(20 + i)
			v.Email = "parallel@example.test"
			v.ContactName = "Bob"
			out, e := svc.SaveMailCustomer(ctx, tenant, v)
			if e != nil {
				failures <- e
			} else {
				ids <- out.Contact.ID
			}
		}(i)
	}
	wg.Wait()
	close(ids)
	close(failures)
	for e := range failures {
		t.Error(e)
	}
	var id int64
	for got := range ids {
		if id != 0 && got != id {
			t.Fatal("concurrent duplicate")
		}
		id = got
	}
	// Generic customer edits must retain the contact id and email aliases.
	contacts, err := svc.ListCustomerContactsDetailed(ctx, tenant, first.Customer.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	list := []ContactInput{}
	for _, c := range contacts {
		list = append(list, ContactInput{Name: c.Name, Email: c.Email, IsPrimary: c.IsPrimary})
	}
	_, _, err = svc.UpdateCustomer(ctx, tenant, first.Customer.ID, CustomerInput{Name: base.CompanyName, Currency: "USD", Contacts: list, OperatorID: 7})
	if err != nil {
		t.Fatal(err)
	}
	link, err = svc.GetMailCustomerLink(ctx, tenant, 3)
	if err != nil || link.Contact.ID != added.Contact.ID || !contactHasEmail(link.Contact, "alice.new@example.test") {
		t.Fatalf("edit lost link: %+v %v", link, err)
	}
	if err = svc.DeactivateCustomer(ctx, tenant, first.Customer.ID, 7, "Sales", "test"); err != nil {
		t.Fatal(err)
	}
	input = base
	input.Action = "ADD"
	input.CustomerID = first.Customer.ID
	input.InboundID = 55
	input.Email = "stopped@example.test"
	if _, err = svc.SaveMailCustomer(ctx, tenant, input); err == nil {
		t.Fatal("added to inactive company")
	}
	matches2, err := svc.MatchMailCustomer(ctx, tenant, base.CompanyName, "one@example.test")
	if err != nil || len(matches2.Customers) != 1 || matches2.Customers[0].Status != "INACTIVE" {
		t.Fatalf("inactive match not classified: %+v %v", matches2, err)
	}
	var n int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM customers WHERE tenant_id=$1`, tenant).Scan(&n); err != nil || n != 1 {
		t.Fatalf("unexpected company count %d: %v", n, err)
	}
}
