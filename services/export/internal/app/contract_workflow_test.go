package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

func TestWorkflowRejectsUntrustedIdentityBeforeDatabaseAccess(t *testing.T) {
	svc := New(nil, Deps{Scopes: d2OfferScopes{}})
	for _, tc := range []struct {
		name             string
		ctx              context.Context
		tenant, employee int64
	}{
		{"anonymous", context.Background(), 17, 1},
		{"missing tenant", grpcx.WithOperator(context.Background(), grpcx.Operator{EmployeeID: 1}), 17, 1},
		{"different tenant", grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: 18, EmployeeID: 1}), 17, 1},
		{"forged employee", grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: 17, EmployeeID: 1}), 17, 3},
		{"permission denied", grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: 17, EmployeeID: 2}), 17, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.ContractWorkflow(tc.ctx, tc.tenant, WorkflowCommand{Action: "history_list"}, Operator{ID: tc.employee}); err == nil {
				t.Fatal("untrusted identity accepted")
			}
		})
	}
}

func TestContractWorkflowTenantIsolationAndStateGuards(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	actor := func(ten, id int64) context.Context {
		return grpcx.WithOperator(ctx, grpcx.Operator{TenantID: ten, EmployeeID: id, Name: "Sales"})
	}
	defer func() {
		for _, table := range []string{"contract_history_drafts", "contract_workflow_actions", "contract_workflows", "contract_items", "contract_versions", "contracts", "outbox_events"} {
			_, _ = pool.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1 OR tenant_id=$2`, tenant, tenant+1)
		}
	}()
	svc := New(pool, Deps{Scopes: d2OfferScopes{}, Customers: dueCustomerStub{}, Products: dueProductStub{}, Rates: d2OfferRates{}, Numbering: &d2OfferNumber{}})
	op := Operator{ID: 1, Name: "Sales"}
	view, err := svc.CreateContract(actor(tenant, 1), tenant, DirectContractInput{CustomerID: 7, Currency: "USD", Terms: Terms{DeliveryDate: "2026-12-01"}, Items: []ItemInput{{ProductName: "Custom plate", UomCode: "KG", Qty: "10", UnitPrice: "2"}}}, op)
	if err != nil {
		t.Fatal(err)
	}
	id := view.Contract.ID
	for _, sql := range []string{
		`INSERT INTO contract_workflows(tenant_id,contract_id) VALUES($1,$2)`,
		`INSERT INTO contract_workflow_actions(tenant_id,contract_id,action,actor_id) VALUES($1,$2,'pause',1)`,
		`INSERT INTO contract_history_drafts(tenant_id,contract_id,owner_id,body) VALUES($1,$2,1,'{}')`,
	} {
		if _, err := pool.Exec(ctx, sql, tenant+1, id); err == nil {
			t.Fatal("cross-tenant foreign key accepted", sql)
		}
	}
	if _, err := svc.ContractWorkflow(actor(tenant+1, 1), tenant+1, WorkflowCommand{ID: id, Action: "get"}, op); err == nil {
		t.Fatal("read another tenant contract")
	}
	if _, _, err := svc.CheckContractExecution(actor(tenant+1, 1), tenant+1, id); err == nil {
		t.Fatal("read another tenant execution state")
	}
	if _, err := svc.ContractWorkflow(actor(tenant, 3), tenant, WorkflowCommand{ID: id, Action: "delete", Reason: "test", Revision: 1}, Operator{ID: 3}); err == nil {
		t.Fatal("nonowner modified contract")
	}
	if _, err := svc.ContractWorkflow(actor(tenant, 1), tenant, WorkflowCommand{ID: id, Action: "delete", Reason: "test", Revision: 2}, op); err == nil {
		t.Fatal("stale revision accepted")
	}
	for _, state := range []string{"DRAFT", "PAUSED", "TERMINATING", "TERMINATED", "COMPLETED", "CANCELLED", "DELETED"} {
		if _, err := pool.Exec(ctx, `UPDATE contracts SET status=$3,condition_confirmed_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, id, state); err != nil {
			t.Fatal(err)
		}
		if allowed, _, err := svc.CheckContractExecution(actor(tenant, 1), tenant, id); err != nil || allowed {
			t.Fatalf("state %s allowed=%v err=%v", state, allowed, err)
		}
	}
	_, err = pool.Exec(ctx, `UPDATE contracts SET status='EXECUTING',condition_confirmed_at=NULL WHERE tenant_id=$1 AND id=$2`, tenant, id)
	if err != nil {
		t.Fatal(err)
	}
	if allowed, _, err := svc.CheckContractExecution(actor(tenant, 1), tenant, id); err != nil || allowed {
		t.Fatal("unreleased allowed", err)
	}
	_, err = pool.Exec(ctx, `UPDATE contracts SET condition_confirmed_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, id)
	if err != nil {
		t.Fatal(err)
	}
	if allowed, _, err := svc.CheckContractExecution(actor(tenant, 1), tenant, id); err != nil || !allowed {
		t.Fatal("released denied", err)
	}
	saved, err := svc.ContractWorkflow(actor(tenant, 1), tenant, WorkflowCommand{Action: "history_save", Data: json.RawMessage(`{"customerId":"7"}`)}, op)
	if err != nil {
		t.Fatal(err)
	}
	var draftID int64
	fmt.Sscan(saved.(map[string]any)["id"].(string), &draftID)
	for _, pair := range [][2]int64{{tenant + 1, 1}, {tenant, 3}} {
		if _, err := svc.ContractWorkflow(actor(pair[0], pair[1]), pair[0], WorkflowCommand{ID: draftID, Action: "history_delete", Revision: 1}, Operator{ID: pair[1]}); err == nil {
			t.Fatal("deleted another tenant/owner draft", pair)
		}
	}
	if _, err := svc.ContractWorkflow(actor(tenant, 1), tenant, WorkflowCommand{ID: draftID, Action: "history_delete", Revision: 1}, op); err != nil {
		t.Fatal(err)
	}
	saved, err = svc.ContractWorkflow(actor(tenant, 1), tenant, WorkflowCommand{Action: "history_save", Data: json.RawMessage(`{"customerId":"7"}`)}, op)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Sscan(saved.(map[string]any)["id"].(string), &draftID)
	if _, err := pool.Exec(ctx, `UPDATE contract_history_drafts SET contract_id=$3,revision=revision+1 WHERE tenant_id=$1 AND id=$2`, tenant, draftID, id); err != nil {
		t.Fatal(err)
	}
	recovered, err := svc.ContractWorkflow(actor(tenant, 1), tenant, WorkflowCommand{ID: draftID, Action: "history_save", Revision: 1, Data: json.RawMessage(`{"customerId":"7"}`)}, op)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.(map[string]any)["contractId"] != fmt.Sprint(id) {
		t.Fatal("import retry did not recover existing contract", recovered)
	}

}

type workflowApprovalRetry struct {
	calls int
	key   string
	t     *testing.T
}

func (a *workflowApprovalRetry) Submit(_ context.Context, in ApprovalSubmission) (int64, error) {
	a.calls++
	var summary map[string]any
	if err := json.Unmarshal([]byte(in.Summary), &summary); err != nil {
		a.t.Fatal(err)
	}
	key := summary["approval_request_key"].(string)
	if a.key != "" && a.key != key {
		a.t.Fatal("retry changed request key")
	}
	a.key = key
	if a.calls == 1 {
		return 0, fmt.Errorf("simulated response loss")
	}
	return 51, nil
}

func TestTerminationRetryAndDecisionProof(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set")
	}
	tenant := time.Now().UnixNano()
	ctx := grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: tenant, EmployeeID: 1})
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	approval := &workflowApprovalRetry{t: t}
	svc := New(pool, Deps{Scopes: d2OfferScopes{}, Customers: dueCustomerStub{}, Products: dueProductStub{}, Rates: d2OfferRates{}, Numbering: &d2OfferNumber{}, Approvals: approval})
	op := Operator{ID: 1}
	view, err := svc.CreateContract(ctx, tenant, DirectContractInput{CustomerID: 7, Currency: "USD", Items: []ItemInput{{ProductName: "plate", Qty: "1", UnitPrice: "1", UomCode: "KG"}}}, op)
	if err != nil {
		t.Fatal(err)
	}
	id := view.Contract.ID
	defer func() {
		for _, table := range []string{"contract_workflow_actions", "contract_workflows", "contract_items", "contract_versions", "contracts", "outbox_events"} {
			_, _ = pool.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1`, tenant)
		}
	}()
	if _, err := pool.Exec(ctx, `UPDATE contracts SET status='EXECUTING',current_version_id=$3,signed_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, id, view.Version.ID); err != nil {
		t.Fatal(err)
	}
	cmd := WorkflowCommand{ID: id, Action: "terminate", Reason: "customer cancellation", Revision: 1}
	if _, err := svc.ContractWorkflow(ctx, tenant, cmd, op); err == nil {
		t.Fatal("expected simulated failure")
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT status FROM contracts WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&state); err != nil || state != "TERMINATING" {
		t.Fatal("failed submission allowed fulfillment", state, err)
	}
	if _, err := svc.ContractWorkflow(ctx, tenant, cmd, op); err != nil {
		t.Fatal("retry", err)
	}
	claim := func(context.Context, pgx.Tx) error { return nil }
	for _, proof := range []ApprovalProof{{InstanceID: 99, RequestKey: approval.key}, {InstanceID: 51, RequestKey: "terminate:wrong"}} {
		if state, err := svc.ApplyApprovalDecision(ctx, tenant, id, "APPROVED", claim, proof); err != nil || state != "TERMINATING" {
			t.Fatal("mismatched proof changed contract", state, err)
		}
	}
	if state, err := svc.ApplyApprovalDecision(ctx, tenant, id, "APPROVED", claim, ApprovalProof{InstanceID: 51, RequestKey: approval.key}); err != nil || state != "TERMINATED" {
		t.Fatal("approval", state, err)
	}
	if state, err := svc.ApplyApprovalDecision(ctx, tenant, id, "REJECTED", claim, ApprovalProof{InstanceID: 51, RequestKey: approval.key}); err != nil || state != "TERMINATED" {
		t.Fatal("late callback revived contract", state, err)
	}
}
