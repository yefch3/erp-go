package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/apierr"
)

func TestDeleteTravelReimbursement(t *testing.T) {
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
	defer func() {
		for _, table := range []string{"travel_reimbursement_history", "travel_reimbursement_files", "travel_reimbursements"} {
			if _, err := pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenant); err != nil {
				t.Error(err)
			}
		}
	}()

	approvals := &d7ApprovalStub{}
	svc := New(pool, Deps{Approvals: approvals, Files: d7FilesStub{}})
	owner, other := Operator{ID: 71, Name: "申请人"}, Operator{ID: 72, Name: "其他员工"}
	input := TravelReimbursementInput{
		TripStart: "2026-09-10", TripEnd: "2026-09-12", Origin: "上海", Destination: "北京",
		Purpose: "拜访客户", Amount: "1250.50", Currency: "CNY", PaymentAccount: "招商银行",
	}
	createWithFile := func() int64 {
		t.Helper()
		claim, err := svc.CreateTravelReimbursement(ctx, tenant, input, owner)
		if err != nil {
			t.Fatal(err)
		}
		key := fmt.Sprintf("travel-reimbursements/%d/%d/invoice.pdf", tenant, claim.ID)
		if _, err := svc.AttachTravelFile(ctx, tenant, claim.ID, key, "invoice.pdf", "INVOICE", owner); err != nil {
			t.Fatal(err)
		}
		return claim.ID
	}
	checkCode := func(err error, code string) {
		t.Helper()
		if apierr.CodeFromError(err) != code {
			t.Fatalf("err=%v, want %s", err, code)
		}
	}

	draft := createWithFile()
	checkCode(svc.DeleteTravelReimbursement(ctx, tenant, draft, other), "PR_TRAVEL_NOT_VISIBLE")
	checkCode(svc.DeleteTravelReimbursement(ctx, tenant+1, draft, owner), "PR_TRAVEL_NOT_FOUND")
	if err := svc.DeleteTravelReimbursement(ctx, tenant, draft, owner); err != nil {
		t.Fatal(err)
	}
	checkCode(svc.DeleteTravelReimbursement(ctx, tenant, draft, owner), "PR_TRAVEL_NOT_FOUND")
	_, err = svc.GetTravelReimbursement(ctx, tenant, draft, other.ID, true)
	checkCode(err, "PR_TRAVEL_NOT_FOUND")
	_, err = svc.SubmitTravelReimbursement(ctx, tenant, draft, owner)
	checkCode(err, "PR_TRAVEL_NOT_FOUND")
	_, err = svc.UpdateTravelReimbursement(ctx, tenant, draft, input, owner)
	checkCode(err, "PR_TRAVEL_NOT_FOUND")
	_, err = svc.AttachTravelFile(ctx, tenant, draft, fmt.Sprintf("travel-reimbursements/%d/%d/new.pdf", tenant, draft), "new.pdf", "INVOICE", owner)
	checkCode(err, "PR_TRAVEL_NOT_FOUND")

	rejected := createWithFile()
	if _, err = svc.SubmitTravelReimbursement(ctx, tenant, rejected, owner); err != nil {
		t.Fatal(err)
	}
	checkCode(svc.DeleteTravelReimbursement(ctx, tenant, rejected, owner), "PR_TRAVEL_DELETE_LOCKED")
	if _, err = svc.ApplyTravelApproval(ctx, tenant, rejected, 7001, "RETURNED", "请核对", other.ID); err != nil {
		t.Fatal(err)
	}
	if err = svc.DeleteTravelReimbursement(ctx, tenant, rejected, owner); err != nil {
		t.Fatal(err)
	}
	if status, err := svc.ApplyTravelApproval(ctx, tenant, rejected, 7001, "RETURNED", "请核对", other.ID); status != "DELETED" || err != nil {
		t.Fatalf("late approval event got status=%q err=%v", status, err)
	}

	approvals.involved = []int64{rejected}
	for _, viewer := range []struct {
		id  int64
		all bool
	}{{owner.ID, false}, {other.ID, false}, {other.ID, true}} {
		list, err := svc.ListTravelReimbursements(ctx, tenant, viewer.id, viewer.all, "")
		if err != nil || len(list) != 0 {
			t.Fatalf("viewer=%+v: deleted claims in list=%+v, err=%v", viewer, list, err)
		}
	}

	for _, record := range []struct {
		id     int64
		status string
	}{{draft, "DRAFT"}, {rejected, "REJECTED"}} {
		var files, deletions int
		err = pool.QueryRow(ctx, `
			SELECT
			  (SELECT count(*) FROM travel_reimbursement_files WHERE tenant_id=$1 AND reimbursement_id=$2 AND removed_at IS NULL),
			  (SELECT count(*) FROM travel_reimbursement_history WHERE tenant_id=$1 AND reimbursement_id=$2
			   AND action='DELETED' AND from_status=$3 AND to_status='DELETED' AND actor_id=$4)
		`, tenant, record.id, record.status, owner.ID).Scan(&files, &deletions)
		if err != nil || files != 1 || deletions != 1 {
			t.Fatalf("claim=%d files=%d deletions=%d err=%v", record.id, files, deletions, err)
		}
	}

	// Pause the approval RPC to verify deletion cannot race past an in-flight submit.
	racingClaim := createWithFile()
	raceCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	blocking := blockingTravelApproval{started: make(chan struct{}), proceed: make(chan struct{})}
	racingService := New(pool, Deps{Approvals: blocking})
	submitted := make(chan error, 1)
	go func() {
		_, err := racingService.SubmitTravelReimbursement(raceCtx, tenant, racingClaim, owner)
		submitted <- err
	}()
	select {
	case <-blocking.started:
	case <-raceCtx.Done():
		t.Fatal("submit did not reach approval service")
	}
	var lockedID int64
	err = pool.QueryRow(raceCtx, `SELECT id FROM travel_reimbursements WHERE tenant_id=$1 AND id=$2 FOR UPDATE NOWAIT`, tenant, racingClaim).Scan(&lockedID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("expected the approval submission to hold a row lock, got %v", err)
	}
	deleted := make(chan error, 1)
	go func() {
		deleted <- svc.DeleteTravelReimbursement(raceCtx, tenant, racingClaim, owner)
	}()
	close(blocking.proceed)
	if err := <-submitted; err != nil {
		t.Fatal(err)
	}
	checkCode(<-deleted, "PR_TRAVEL_DELETE_LOCKED")
}

type blockingTravelApproval struct {
	started chan struct{}
	proceed chan struct{}
}

func (a blockingTravelApproval) Submit(ctx context.Context, _ ApprovalSubmission) (int64, error) {
	close(a.started)
	select {
	case <-a.proceed:
		return 7002, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}
