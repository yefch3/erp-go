package app

import (
	"bytes"
	"context"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

type documentMemoryFiles struct{ objects map[string][]byte }

func (f *documentMemoryFiles) PresignPut(context.Context, string) (string, int32, error) {
	return "", 0, nil
}
func (f *documentMemoryFiles) PresignGet(context.Context, string) (string, error) { return "", nil }
func (f *documentMemoryFiles) Remove(ctx context.Context, key string) error {
	delete(f.objects, key)
	return nil
}
func (f *documentMemoryFiles) Put(ctx context.Context, key string, r io.Reader, size int64, kind string) (string, error) {
	b, e := io.ReadAll(r)
	f.objects[key] = b
	return key, e
}
func (f *documentMemoryFiles) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.objects[key])), nil
}
func TestCustomerDocumentValidation(t *testing.T) {
	valid := CustomerDocumentInput{CustomerID: 1, OperatorID: 2, Title: "License", FileName: "license.pdf", Content: []byte("%PDF-1.4"), RemindDays: 30}
	if err := validateCustomerDocument(valid); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*CustomerDocumentInput){func(v *CustomerDocumentInput) { v.ExpiresOn = "2026-02-30" }, func(v *CustomerDocumentInput) { v.RemindDays = -1 }, func(v *CustomerDocumentInput) { v.RemindDays = 3651 }, func(v *CustomerDocumentInput) { v.Content = nil }, func(v *CustomerDocumentInput) { v.Content = make([]byte, CustomerDocumentMaxBytes+1) }} {
		in := valid
		mutate(&in)
		if validateCustomerDocument(in) == nil {
			t.Fatal("invalid input accepted")
		}
	}
}
func TestCustomerDocumentLifecycleIntegration(t *testing.T) {
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
	s := New(pool)
	files := &documentMemoryFiles{objects: map[string][]byte{}}
	s.UseFiles(files)
	a, b, admin := WithCustomerAccess(ctx, 8101), WithCustomerAccess(ctx, 8102), WithCustomerAccess(ctx, 0)
	c, _, err := s.CreateCustomer(a, 1, CustomerInput{Name: "Document lifecycle test", CountryCode: "US", OperatorID: 8101, OperatorName: "A"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, sql := range []string{"DELETE FROM customer_document_reads WHERE tenant_id=1 AND revision_id IN (SELECT id FROM customer_documents WHERE tenant_id=1 AND customer_id=$1)", "DELETE FROM customer_documents WHERE tenant_id=1 AND customer_id=$1", "DELETE FROM customer_change_logs WHERE tenant_id=1 AND customer_id=$1", "DELETE FROM customer_owners WHERE tenant_id=1 AND customer_id=$1", "DELETE FROM customers WHERE tenant_id=1 AND id=$1"} {
			_, _ = pool.Exec(ctx, sql, c.ID)
		}
	}()
	in := CustomerDocumentInput{CustomerID: c.ID, OperatorID: 8101, OperatorName: "A", Title: "License", FileName: "license.pdf", Content: []byte("%PDF-1.4 test"), ExpiresOn: time.Now().UTC().AddDate(0, 0, 7).Format("2006-01-02"), RemindDays: 30, ReminderEnabled: true}
	first, err := s.SaveCustomerDocument(a, 1, in)
	if err != nil {
		t.Fatal(err)
	}
	for key := range files.objects {
		if !strings.HasPrefix(key, "customer-documents/1/") {
			t.Fatal("missing tenant namespace")
		}
	}
	if _, err = s.SaveCustomerDocument(b, 1, in); err == nil {
		t.Fatal("non-owner upload accepted")
	}
	if _, _, _, err = s.GetCustomerDocumentFile(b, 1, c.ID, first); err == nil {
		t.Fatal("non-owner file exposed")
	}
	if _, _, _, err = s.GetCustomerDocumentFile(admin, 2, c.ID, first); err == nil {
		t.Fatal("cross-tenant file exposed")
	}
	rows, err := s.ListCustomerDocuments(a, 1, 0, 8101, true)
	if err != nil || len(rows) != 1 || !rows[0].Unread {
		t.Fatalf("reminder: %#v %v", rows, err)
	}
	if n, err := s.MarkCustomerDocumentRemindersRead(a, 1, 8101, []int64{first}); err != nil || n != 1 {
		t.Fatalf("read: %d %v", n, err)
	}
	if n, err := s.MarkCustomerDocumentRemindersRead(a, 1, 8101, []int64{first}); err != nil || n != 0 {
		t.Fatalf("duplicate read: %d %v", n, err)
	}
	owner, err := s.CreateCustomerOwner(a, 1, c.ID, CustomerOwnerInput{EmployeeID: 8102, EmployeeName: "B", ResponsibilityCode: "SALES", OperatorID: 8101})
	if err != nil {
		t.Fatal(err)
	}
	rows, err = s.ListCustomerDocuments(b, 1, 0, 8102, true)
	if err != nil || len(rows) != 1 || !rows[0].Unread {
		t.Fatalf("new owner reminder: %#v %v", rows, err)
	}
	if _, _, _, err = s.GetCustomerDocumentFile(b, 1, c.ID, first); err != nil {
		t.Fatal(err)
	}
	// Replacing validity preserves the old file and removes the old reminder.
	in.ReplacesID = first
	in.Content = nil
	in.ExpiresOn = time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")
	second, err := s.SaveCustomerDocument(b, 1, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveCustomerDocument(a, 1, in); err == nil {
		t.Fatal("stale replacement accepted")
	}
	rows, err = s.ListCustomerDocuments(a, 1, 0, 8101, true)
	if err != nil || len(rows) != 0 {
		t.Fatalf("superseded reminder: %#v %v", rows, err)
	}
	if _, _, content, err := s.GetCustomerDocumentFile(a, 1, c.ID, first); err != nil || string(content) != "%PDF-1.4 test" {
		t.Fatal("old file lost", err)
	}
	in.ReplacesID = second
	in.ExpiresOn = time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	in.Content = []byte("new certificate")
	third, err := s.SaveCustomerDocument(a, 1, in)
	if err != nil {
		t.Fatal(err)
	}
	rows, err = s.ListCustomerDocuments(a, 1, 0, 8101, true)
	if err != nil || len(rows) != 1 || rows[0].ID != third || !rows[0].Unread {
		t.Fatalf("new reminder: %#v %v", rows, err)
	}
	rows, err = s.ListCustomerDocuments(a, 1, c.ID, 8101, false)
	if err != nil || len(rows) != 3 {
		t.Fatal("history", rows, err)
	}
	if err = s.DeactivateCustomerOwner(a, 1, c.ID, owner.ID, "", 8101, "A"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = s.GetCustomerDocumentFile(b, 1, c.ID, first); err == nil {
		t.Fatal("revoked owner downloaded history")
	}
	rows, err = s.ListCustomerDocuments(b, 1, 0, 8102, true)
	if err != nil || len(rows) != 0 {
		t.Fatal("revoked owner reminder", err)
	}
	if err = s.DeleteCustomerDocument(b, 1, c.ID, third); err == nil {
		t.Fatal("revoked owner deleted attachment")
	}
	if err = s.DeleteCustomerDocument(admin, 2, c.ID, third); err == nil {
		t.Fatal("cross tenant delete allowed")
	}
	// Any current owner may delete, including somebody who did not upload it.
	_, err = s.CreateCustomerOwner(a, 1, c.ID, CustomerOwnerInput{EmployeeID: 8103, EmployeeName: "C", ResponsibilityCode: "SALES", OperatorID: 8101})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteCustomerDocument(WithCustomerAccess(ctx, 8103), 1, c.ID, third); err != nil {
		t.Fatal(err)
	}
	rows, err = s.ListCustomerDocuments(a, 1, c.ID, 8101, false)
	if err != nil || len(rows) != 0 {
		t.Fatal("deleted attachment still listed", err)
	}
	rows, err = s.ListCustomerDocuments(a, 1, 0, 8101, true)
	if err != nil || len(rows) != 0 {
		t.Fatal("deleted attachment still reminds", err)
	}
	for _, id := range []int64{first, second, third} {
		if _, _, _, err = s.GetCustomerDocumentFile(admin, 1, c.ID, id); err == nil {
			t.Fatal("deleted revision downloadable")
		}
	}
	if _, err = s.SaveCustomerDocument(a, 1, in); err == nil {
		t.Fatal("deleted attachment revived by stale update")
	}

	rows, err = s.ListCustomerDocuments(admin, 2, 0, 8101, true)
	if err != nil || len(rows) != 0 {
		t.Fatal("cross-tenant reminders", err)
	}
}
