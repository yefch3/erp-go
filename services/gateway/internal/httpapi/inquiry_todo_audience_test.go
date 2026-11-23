package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	"google.golang.org/grpc"
)

type todoReceptionAccessStub struct {
	iamv1.AccessServiceClient
	lookedUp []int64
}

func (s *todoReceptionAccessStub) GetTodoReception(_ context.Context, req *iamv1.GetTodoReceptionRequest, _ ...grpc.CallOption) (*iamv1.GetTodoReceptionResponse, error) {
	s.lookedUp = append(s.lookedUp, req.GetEmployeeId())
	return &iamv1.GetTodoReceptionResponse{Reception: &iamv1.TodoReception{
		ApprovalsOnly: req.GetEmployeeId() == 15,
	}}, nil
}

func TestMyTodoReceptionUsesCurrentEmployee(t *testing.T) {
	access := &todoReceptionAccessStub{}
	server := &Server{Access: access}
	request := httptest.NewRequest("GET", "/api/me/todo-reception?employeeId=11", nil)
	rec := httptest.NewRecorder()
	server.myTodoReception(rec, request.WithContext(as(4, 15)))
	if rec.Code != 200 || len(access.lookedUp) != 1 || access.lookedUp[0] != 15 || !strings.Contains(rec.Body.String(), `"approvalsOnly":true`) {
		t.Fatalf("expected logged-in employee setting: %d %s (lookups %v)", rec.Code, rec.Body.String(), access.lookedUp)
	}
}

func TestAssignedTodoReceptionPreviewOnlyFlagsApprovalsOnly(t *testing.T) {
	access := &todoReceptionAccessStub{}
	server := &Server{Access: access}
	request := httptest.NewRequest("POST", "/api/execution-assignments/todo-reception", strings.NewReader(`{"employeeIds":["11","15","15"]}`))
	rec := httptest.NewRecorder()
	server.checkAssignmentTodoReception(rec, request.WithContext(as(4, 9)))
	var response struct {
		Data struct{ ApprovalsOnly map[string]bool }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || len(access.lookedUp) != 2 || len(response.Data.ApprovalsOnly) != 2 ||
		!response.Data.ApprovalsOnly["15"] || response.Data.ApprovalsOnly["11"] {
		t.Fatalf("unexpected assignment preview: %d %s (lookups %v)", rec.Code, rec.Body.String(), access.lookedUp)
	}
}
