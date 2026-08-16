package app

import (
	"context"
	"testing"
)

type recordingSourcingScopes struct {
	employeeID int64
	module     string
}

func (s *recordingSourcingScopes) VisibleEmployees(_ context.Context, employeeID int64, module string) (Visibility, error) {
	s.employeeID = employeeID
	s.module = module
	return Visibility{EmployeeIDs: []int64{employeeID}, ScopeType: "SELF"}, nil
}

func TestVisibleSourcingToUsesDedicatedIAMModule(t *testing.T) {
	scopes := &recordingSourcingScopes{}
	svc := &Service{scopes: scopes}
	visible, err := svc.visibleSourcingTo(context.Background(), Operator{ID: 42})
	if err != nil {
		t.Fatal(err)
	}
	if scopes.employeeID != 42 || scopes.module != "procurement_sourcing" {
		t.Fatalf("IAM request = employee %d module %q", scopes.employeeID, scopes.module)
	}
	if visible.ScopeType != "SELF" || len(visible.EmployeeIDs) != 1 || visible.EmployeeIDs[0] != 42 {
		t.Fatalf("visibility = %#v", visible)
	}
}

func TestAllowedSourcingOwner(t *testing.T) {
	tests := []struct {
		name    string
		visible Visibility
		ownerID int64
		want    bool
	}{
		{name: "all", visible: Visibility{All: true}, ownerID: 99, want: true},
		{name: "listed", visible: Visibility{EmployeeIDs: []int64{7, 8}}, ownerID: 8, want: true},
		{name: "outside", visible: Visibility{EmployeeIDs: []int64{7, 8}}, ownerID: 9, want: false},
		{name: "empty", visible: Visibility{}, ownerID: 7, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := allowedSourcingOwner(tt.visible, tt.ownerID); got != tt.want {
				t.Fatalf("allowedSourcingOwner() = %v, want %v", got, tt.want)
			}
		})
	}
}
