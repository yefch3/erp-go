package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sgao19/erp-go/pkg/apierr"
)

type expenseApprovalStub struct {
	d7ApprovalStub
	submissions []ApprovalSubmission
}

type directExpenseApprovalStub struct{ expenseApprovalStub }

func (d *directExpenseApprovalStub) SubmitReimbursement(ctx context.Context, in ApprovalSubmission) (int64, bool, error) {
	id, err := d.Submit(ctx, in)
	return id, true, err
}

func TestSubmitExistingExpenseDraftDirectlyToFinance(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tenant := time.Now().UnixNano()
	t.Cleanup(func() {
		for _, table := range []string{"travel_reimbursement_history", "travel_reimbursement_files", "travel_reimbursements"} {
			if _, err := pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenant); err != nil {
				t.Error(err)
			}
		}
	})
	svc := New(pool, Deps{Approvals: &directExpenseApprovalStub{}, People: d7PeopleStub{}, Files: d7FilesStub{}})
	for index, department := range []string{"海运部", "采购部", "质检部"} {
		t.Run(department, func(t *testing.T) {
			owner := Operator{ID: int64(index + 1), Name: department + "测试员工"}
			draft, err := svc.CreateTravelReimbursement(ctx, tenant, TravelReimbursementInput{
				ExpenseType: "OTHER", ExpenseDate: "2026-11-17", Purpose: department + "测试报销",
				Amount: "100", Currency: "CNY", PaymentAccount: "测试账户",
			}, owner)
			if err != nil {
				t.Fatal(err)
			}
			key := fmt.Sprintf("travel-reimbursements/%d/%d/expense.pdf", tenant, draft.ID)
			if _, err := svc.AttachTravelFile(ctx, tenant, draft.ID, key, "expense.pdf", "INVOICE", owner); err != nil {
				t.Fatal(err)
			}
			claim, err := svc.SubmitTravelReimbursement(ctx, tenant, draft.ID, owner)
			if err != nil || claim.ClaimNo != draft.ClaimNo || claim.Status != "PENDING_FINANCE_APPROVAL" || len(claim.Files) != 1 {
				t.Fatalf("submitted claim=%+v err=%v", claim, err)
			}
			next, err := svc.ApplyTravelApproval(ctx, tenant, claim.ID, claim.ApprovalInstanceID, "APPROVED", "", 100)
			if err != nil || next != "PENDING_PAYMENT" {
				t.Fatalf("finance decision: status=%q err=%v", next, err)
			}
		})
	}
}

func (d *expenseApprovalStub) Submit(_ context.Context, in ApprovalSubmission) (int64, error) {
	d.submissions = append(d.submissions, in)
	return int64(7100 + len(d.submissions)), nil
}

func TestExpenseReimbursementLifecycle(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tenant := time.Now().UnixNano()
	owner := Operator{ID: 64201, Name: "Expense applicant"}
	t.Cleanup(func() {
		for _, table := range []string{"travel_reimbursement_history", "travel_reimbursement_files", "travel_reimbursements"} {
			if _, err := pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenant); err != nil {
				t.Error(err)
			}
		}
	})
	approvals := &expenseApprovalStub{}
	svc := New(pool, Deps{Approvals: approvals, People: d7PeopleStub{}, Files: d7FilesStub{}})
	for _, kind := range []string{"OFFICE_SUPPLIES", "OTHER"} {
		t.Run(kind, func(t *testing.T) {
			in := TravelReimbursementInput{ExpenseType: kind, ExpenseDate: "2026-11-17", Purpose: "Printer paper", Amount: "168.80", Currency: "CNY", PaymentAccount: "Test only", Note: "independent expense"}
			claim, err := svc.CreateTravelReimbursement(ctx, tenant, in, owner)
			if err != nil {
				t.Fatal(err)
			}
			if claim.ExpenseType != kind || claim.ExpenseDate != in.ExpenseDate || claim.TripStart != "" || claim.TripEnd != "" || claim.Origin != "" || claim.Destination != "" || claim.Note != in.Note {
				t.Fatalf("created=%+v", claim)
			}
			var noTrip bool
			if err := pool.QueryRow(ctx, "SELECT trip_start IS NULL AND trip_end IS NULL FROM travel_reimbursements WHERE tenant_id=$1 AND id=$2", tenant, claim.ID).Scan(&noTrip); err != nil || !noTrip {
				t.Fatalf("dates not null: %v %v", noTrip, err)
			}
			if _, err = svc.SubmitTravelReimbursement(ctx, tenant, claim.ID, owner); apierr.CodeFromError(err) != "PR_TRAVEL_DOCUMENT_REQUIRED" {
				t.Fatalf("missing evidence=%v", err)
			}
			key := fmt.Sprintf("travel-reimbursements/%d/%d/expense.pdf", tenant, claim.ID)
			if _, err = svc.AttachTravelFile(ctx, tenant, claim.ID, key, "expense.pdf", "INVOICE", owner); err != nil {
				t.Fatal(err)
			}
			rows, err := svc.ListTravelReimbursements(ctx, tenant, owner.ID, false, "")
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, row := range rows {
				if row.ID == claim.ID {
					found = row.ExpenseType == kind && row.ExpenseDate == in.ExpenseDate && len(row.Files) == 1
				}
			}
			if !found {
				t.Fatal("list lost expense data")
			}
			if _, err = svc.UpdateTravelReimbursement(ctx, tenant, claim.ID, in, Operator{ID: 64202}); err == nil {
				t.Fatal("other owner edited claim")
			}
			if _, err = svc.GetTravelReimbursement(ctx, tenant+1, claim.ID, owner.ID, true); err == nil {
				t.Fatal("cross tenant read")
			}
			claim, err = svc.SubmitTravelReimbursement(ctx, tenant, claim.ID, owner)
			if err != nil {
				t.Fatal(err)
			}
			submitted := approvals.submissions[len(approvals.submissions)-1]
			var summary map[string]string
			if err = json.Unmarshal([]byte(submitted.Summary), &summary); err != nil {
				t.Fatal(err)
			}
			if submitted.BizType != BizTypeTravelReimbursement || summary["expense_type"] != kind || summary["expense_date"] != in.ExpenseDate {
				t.Fatalf("approval=%+v", submitted)
			}
			if _, err = svc.UpdateTravelReimbursement(ctx, tenant, claim.ID, in, owner); apierr.CodeFromError(err) != "PR_TRAVEL_EDIT_LOCKED" {
				t.Fatalf("edit pending=%v", err)
			}
			if _, err = svc.ApplyTravelApproval(ctx, tenant, claim.ID, claim.ApprovalInstanceID, "RETURNED", "Need receipt details", 64202); err != nil {
				t.Fatal(err)
			}
			in.Purpose = "Paper and stationery"
			claim, err = svc.UpdateTravelReimbursement(ctx, tenant, claim.ID, in, owner)
			if err != nil {
				t.Fatal(err)
			}
			if claim.Status != "DRAFT" || claim.ExpenseType != kind || len(claim.Files) != 1 {
				t.Fatalf("edited=%+v", claim)
			}
			claim, err = svc.SubmitTravelReimbursement(ctx, tenant, claim.ID, owner)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = svc.ApplyTravelApproval(ctx, tenant, claim.ID, claim.ApprovalInstanceID, "ADVANCED", "", 64202); err != nil {
				t.Fatal(err)
			}
			if _, err = svc.ApplyTravelApproval(ctx, tenant, claim.ID, claim.ApprovalInstanceID, "APPROVED", "", 64203); err != nil {
				t.Fatal(err)
			}
			// Switching category after approval invalidates approval, retaining evidence.
			in.ExpenseType = "TRAVEL"
			in.TripStart = "2026-11-16"
			in.TripEnd = "2026-11-17"
			in.Origin = "Shanghai"
			in.Destination = "Beijing"
			oldApproval := claim.ApprovalInstanceID
			claim, err = svc.UpdateTravelReimbursement(ctx, tenant, claim.ID, in, owner)
			if err != nil {
				t.Fatal(err)
			}
			if claim.Status != "DRAFT" || claim.ApprovalInstanceID != 0 || claim.ExpenseType != "TRAVEL" || claim.ExpenseDate != "" || len(claim.Files) != 1 {
				t.Fatalf("switch=%+v", claim)
			}
			if _, err = svc.MarkTravelPaid(ctx, tenant, claim.ID, "2026-11-17", "Test only", "", owner); err == nil {
				t.Fatal("unapproved claim paid")
			}
			if _, err = svc.ApplyTravelApproval(ctx, tenant, claim.ID, oldApproval, "APPROVED", "stale", 64203); err == nil {
				t.Fatal("stale approval applied")
			}
			in.ExpenseType = kind
			in.ExpenseDate = "2026-11-17"
			claim, err = svc.UpdateTravelReimbursement(ctx, tenant, claim.ID, in, owner)
			if err != nil {
				t.Fatal(err)
			}
			if claim.TripStart != "" || claim.Origin != "" {
				t.Fatalf("stale hidden travel fields persisted: %+v", claim)
			}
			claim, err = svc.SubmitTravelReimbursement(ctx, tenant, claim.ID, owner)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = svc.ApplyTravelApproval(ctx, tenant, claim.ID, claim.ApprovalInstanceID, "ADVANCED", "", 64202); err != nil {
				t.Fatal(err)
			}
			if _, err = svc.ApplyTravelApproval(ctx, tenant, claim.ID, claim.ApprovalInstanceID, "APPROVED", "", 64203); err != nil {
				t.Fatal(err)
			}
			claim, err = svc.MarkTravelPaid(ctx, tenant, claim.ID, "2026-11-17", "Test only", "NO-BANK-TRANSFER", Operator{ID: 64203, Name: "Test finance"})
			if err != nil {
				t.Fatal(err)
			}
			if claim.Status != "PAID" || claim.ExpenseType != kind || claim.ExpenseDate != in.ExpenseDate || claim.Amount != "168.80" {
				t.Fatalf("paid=%+v", claim)
			}
			if _, err = svc.UpdateTravelReimbursement(ctx, tenant, claim.ID, in, owner); apierr.CodeFromError(err) != "PR_TRAVEL_EDIT_LOCKED" {
				t.Fatalf("paid edit=%v", err)
			}
			claim, err = svc.ReverseTravelPayment(ctx, tenant, claim.ID, "Synthetic reversal", Operator{ID: 64203})
			if err != nil || claim.Status != "PENDING_PAYMENT" || claim.ExpenseType != kind {
				t.Fatalf("reverse=%+v %v", claim, err)
			}
		})
	}
}
