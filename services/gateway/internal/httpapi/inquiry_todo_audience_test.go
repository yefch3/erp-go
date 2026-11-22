package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	"google.golang.org/grpc"
)

type inquiryTodoRoleDirectory struct {
	iamv1.AccessServiceClient
	roles   []*iamv1.Role
	members map[int64][]*iamv1.RoleMember
}

func (d inquiryTodoRoleDirectory) ListRoles(context.Context, *iamv1.ListRolesRequest, ...grpc.CallOption) (*iamv1.ListRolesResponse, error) {
	return &iamv1.ListRolesResponse{Roles: d.roles}, nil
}

func (d inquiryTodoRoleDirectory) ListRoleMembers(_ context.Context, request *iamv1.ListRoleMembersRequest, _ ...grpc.CallOption) (*iamv1.ListRoleMembersResponse, error) {
	return &iamv1.ListRoleMembersResponse{Members: d.members[request.GetRoleId()]}, nil
}

func TestInquiryTodoAudienceUsesCallerRoleMembership(t *testing.T) {
	const buyer, buyerManager, sales, salesManager, boss, superAdmin, both int64 = 11, 12, 13, 14, 15, 16, 17
	s := &Server{Access: inquiryTodoRoleDirectory{
		roles: []*iamv1.Role{
			{Id: 1, Code: "BUYER"}, {Id: 2, Code: "PROCUREMENT_MANAGER"},
			{Id: 3, Code: "SALES"}, {Id: 4, Code: "SALES_MANAGER"},
			{Id: 5, Code: "BOSS", Name: "总经理"}, {Id: 6, Code: "SUPER_ADMIN"},
		},
		members: map[int64][]*iamv1.RoleMember{
			1: {{EmployeeId: buyer}}, 2: {{EmployeeId: buyerManager}, {EmployeeId: both}},
			3: {{EmployeeId: sales}}, 4: {{EmployeeId: salesManager}},
			5: {{EmployeeId: boss}, {EmployeeId: both}}, 6: {{EmployeeId: superAdmin}},
		},
	}}
	for _, tc := range []struct {
		name    string
		id      int64
		include bool
	}{
		{"buyer", buyer, true}, {"procurement manager", buyerManager, true},
		{"sales", sales, true}, {"sales manager", salesManager, true},
		{"boss", boss, false}, {"super admin", superAdmin, false}, {"boss and procurement manager", both, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			request := httptest.NewRequest("GET", "/api/me/inquiry-todo-audience?employeeId=11", nil)
			s.myInquiryTodoAudience(rec, request.WithContext(as(4, tc.id)))
			var response struct {
				Data struct {
					IncludeAutomaticQuotes *bool `json:"includeAutomaticQuotes"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			got := response.Data.IncludeAutomaticQuotes
			if rec.Code != 200 || got == nil || *got != tc.include {
				t.Fatalf("want include=%t for current caller, got status=%d body=%s", tc.include, rec.Code, rec.Body.String())
			}
		})
	}
}
