package app

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sgao19/erp-go/pkg/apierr"
	"os"
	"testing"
	"time"
)

type d7ApprovalStub struct{ involved []int64 }

func (d *d7ApprovalStub) Submit(context.Context, ApprovalSubmission) (int64, error) { return 7001, nil }
func (d *d7ApprovalStub) Involved(context.Context, int64, string) ([]int64, error) {
	return d.involved, nil
}

type d7PeopleStub struct{}

func (d7PeopleStub) Employee(context.Context, int64) (EmployeeIdentity, error) {
	return EmployeeIdentity{DepartmentName: "销售部"}, nil
}

type d7FilesStub struct{}

func (d7FilesStub) PresignPut(context.Context, string) (string, int32, error) {
	return "https://put", 900, nil
}
func (d7FilesStub) PresignGet(context.Context, string) (string, error) { return "https://get", nil }
func (d7FilesStub) Put(context.Context, string, []byte, string) error  { return nil }
func (d7FilesStub) Remove(context.Context, string) error               { return nil }
func TestTravelReimbursementApprovalPaymentLifecycle(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	defer pool.Exec(ctx, "DELETE FROM travel_reimbursement_history WHERE tenant_id=$1", tenant)
	defer pool.Exec(ctx, "DELETE FROM travel_reimbursement_files WHERE tenant_id=$1", tenant)
	defer pool.Exec(ctx, "DELETE FROM travel_reimbursements WHERE tenant_id=$1", tenant)
	approvals := &d7ApprovalStub{}
	svc := New(pool, Deps{Approvals: approvals, People: d7PeopleStub{}, Files: d7FilesStub{}})
	owner := Operator{ID: 71, Name: "申请人"}
	in := TravelReimbursementInput{TripStart: "2026-09-10", TripEnd: "2026-09-12", Origin: "上海", Destination: "北京", Purpose: "拜访客户", Amount: "1250.50", Currency: "cny", PaymentAccount: "招商银行"}
	claim, err := svc.CreateTravelReimbursement(ctx, tenant, in, owner)
	if err != nil {
		t.Fatal(err)
	}
	if claim.Status != "DRAFT" || claim.DepartmentName != "销售部" {
		t.Fatalf("created=%+v", claim)
	}
	if _, err = svc.SubmitTravelReimbursement(ctx, tenant, claim.ID, owner); apierr.CodeFromError(err) != "PR_TRAVEL_DOCUMENT_REQUIRED" {
		t.Fatalf("missing document err=%v", err)
	}
	key := fmt.Sprintf("travel-reimbursements/%d/%d/invoice.pdf", tenant, claim.ID)
	if _, err = svc.AttachTravelFile(ctx, tenant, claim.ID, key, "invoice.pdf", "INVOICE", owner); err != nil {
		t.Fatal(err)
	}
	claim, err = svc.SubmitTravelReimbursement(ctx, tenant, claim.ID, owner)
	if err != nil {
		t.Fatal(err)
	}
	if claim.Status != "PENDING_DEPARTMENT_CONFIRMATION" || claim.ApprovalInstanceID != 7001 {
		t.Fatalf("submitted=%+v", claim)
	}
	if _, err = svc.ApplyTravelApproval(ctx, tenant, claim.ID, 7001, "ADVANCED", "属实", 81); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ApplyTravelApproval(ctx, tenant, claim.ID, 7001, "APPROVED", "同意", 91); err != nil {
		t.Fatal(err)
	}
	claim, err = svc.MarkTravelPaid(ctx, tenant, claim.ID, "2026-09-13", "公司账户", "BANK-1", Operator{ID: 92, Name: "财务"})
	if err != nil {
		t.Fatal(err)
	}
	if claim.Status != "PAID" || claim.PaymentReference != "BANK-1" {
		t.Fatalf("paid=%+v", claim)
	}
	if _, err = svc.AttachTravelFile(ctx, tenant, claim.ID, key, "changed.pdf", "INVOICE", owner); apierr.CodeFromError(err) != "PR_TRAVEL_DOCUMENT_LOCKED" {
		t.Fatalf("paid document mutation err=%v", err)
	}
	claim, err = svc.ReverseTravelPayment(ctx, tenant, claim.ID, "转账退回", Operator{ID: 92, Name: "财务"})
	if err != nil {
		t.Fatal(err)
	}
	if claim.Status != "PENDING_PAYMENT" || len(claim.History) < 7 {
		t.Fatalf("reversed=%+v", claim)
	}
}
