package app

import (
	"context"
	"testing"
)

type draftRoleDirectory struct{ dueDirectoryStub }

func (draftRoleDirectory) IsSuperAdmin(_ context.Context, id int64) (bool, error) {
	return id == 7, nil
}

func TestContractDraftDelegation(t *testing.T) {
	svc := New(nil, Deps{Directory: draftRoleDirectory{}})
	var v ContractView
	v.Contract.EntrySource = "STANDARD"
	v.Contract.Status = "DRAFT"
	v.Contract.SalesEmployeeID = 3
	v.Version.Status = "DRAFT"
	ctx := context.Background()
	for _, id := range []int64{3, 7} {
		if err := svc.requireDraftActor(ctx, Operator{ID: id}, v); err != nil {
			t.Fatalf("draft actor %d: %v", id, err)
		}
	}
	if svc.requireDraftActor(ctx, Operator{ID: 8}, v) == nil {
		t.Fatal("ordinary colleague may edit")
	}
	if svc.mustOwnContract(ctx, Operator{ID: 7}, v) == nil {
		t.Fatal("delegation leaked to other contract actions")
	}
	v.Version.Status = "PENDING_APPROVAL"
	if svc.requireDraftActor(ctx, Operator{ID: 7}, v) == nil {
		t.Fatal("delegation applies to submitted version")
	}
}
