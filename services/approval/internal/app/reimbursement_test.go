package app

import (
	"context"
	"github.com/sgao19/erp-go/services/approval/internal/store"
	"testing"
)

type reimbursementDirectory struct {
	leader   int64
	roles    map[string][]int64
	managers []int64
}

func (d reimbursementDirectory) RoleMembers(context.Context, int64) ([]int64, error) { return nil, nil }
func (d reimbursementDirectory) RoleMembersByCode(_ context.Context, code string) ([]int64, bool, error) {
	v, ok := d.roles[code]
	return v, ok, nil
}
func (d reimbursementDirectory) ManagersOf(context.Context, int64, int32) ([]int64, error) {
	return d.managers, nil
}
func (d reimbursementDirectory) DepartmentLeaderOf(context.Context, int64) (int64, error) {
	return d.leader, nil
}
func TestTravelReimbursementApproversExcludeClaimant(t *testing.T) {
	ctx := context.Background()
	s := &Service{dir: reimbursementDirectory{leader: 20, roles: map[string][]int64{"FINANCE_MANAGER": {30, 10}, "BOSS": {40}}}}
	ids, err := s.assigneesFor(ctx, store.ApprovalNode{ApproverType: "DEPARTMENT_LEADER"}, 10)
	if err != nil || len(ids) != 1 || ids[0] != 20 {
		t.Fatalf("department leader ids=%v err=%v", ids, err)
	}
	ids, err = s.assigneesFor(ctx, store.ApprovalNode{ApproverType: "FINANCE_MANAGER"}, 10)
	if err != nil || len(ids) != 1 || ids[0] != 30 {
		t.Fatalf("finance ids=%v err=%v", ids, err)
	}
}
func TestFinanceManagerOwnClaimFallsBackToBoss(t *testing.T) {
	s := &Service{dir: reimbursementDirectory{leader: 20, roles: map[string][]int64{"FINANCE_MANAGER": {10}, "BOSS": {40}}}}
	ids, err := s.assigneesFor(context.Background(), store.ApprovalNode{ApproverType: "FINANCE_MANAGER"}, 10)
	if err != nil || len(ids) != 1 || ids[0] != 40 {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
}
