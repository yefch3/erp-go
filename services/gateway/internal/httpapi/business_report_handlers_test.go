package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/authtoken"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"google.golang.org/grpc"
)

type reportTestAccess struct{ iamv1.AccessServiceClient }

func (reportTestAccess) CheckPermission(ctx context.Context, req *iamv1.CheckPermissionRequest, _ ...grpc.CallOption) (*iamv1.CheckPermissionResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	return &iamv1.CheckPermissionResponse{Allowed: op.TenantID == 21 && req.EmployeeId == 7 && req.PermissionCode == businessReportPermission}, nil
}

type reportTestContracts struct {
	exv1.ContractServiceClient
	calls  int
	tenant int64
}

func (s *reportTestContracts) GetBusinessReport(ctx context.Context, _ *exv1.GetBusinessReportRequest, _ ...grpc.CallOption) (*exv1.GetBusinessReportResponse, error) {
	s.calls++
	s.tenant = grpcx.TenantID(ctx)
	return &exv1.GetBusinessReportResponse{}, nil
}

type reportTestSourcing struct{ prv1.SourcingServiceClient }

func (reportTestSourcing) GetReportInquiries(context.Context, *prv1.GetReportInquiriesRequest, ...grpc.CallOption) (*prv1.GetReportInquiriesResponse, error) {
	return &prv1.GetReportInquiriesResponse{}, nil
}

func TestBusinessReportRoutesEnforcePermissionAndSessionTenant(t *testing.T) {
	const secret = "business-report-test-key"
	contracts := &reportTestContracts{}
	server := (&Server{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), JWTSecret: secret, Access: reportTestAccess{}, Contracts: contracts, Sourcing: reportTestSourcing{}}).Router()
	for _, path := range []string{"quarter", "overdue", "sales", "contracts", "contracts/42"} {
		t.Run(path, func(t *testing.T) {
			for _, actor := range []struct {
				tenant, employee int64
				status           int
			}{
				{0, 0, http.StatusUnauthorized},
				{21, 8, http.StatusForbidden},
				{22, 7, http.StatusForbidden},
				{21, 7, http.StatusOK},
			} {
				callsBefore := contracts.calls
				req := httptest.NewRequest(http.MethodGet, "/api/business-reports/"+path+"?tenant_id=22&year=2025&quarter=2", nil)
				if actor.employee > 0 {
					token, err := authtoken.Issue(secret, time.Hour, actor.tenant, actor.employee, "tester", "tester@example.test")
					if err != nil {
						t.Fatal(err)
					}
					req.Header.Set("Authorization", "Bearer "+token)
				}
				rec := httptest.NewRecorder()
				server.ServeHTTP(rec, req)
				if rec.Code != actor.status {
					t.Fatalf("tenant %d actor %d: got %d, want %d: %s", actor.tenant, actor.employee, rec.Code, actor.status, rec.Body.String())
				}
				if actor.status == http.StatusOK {
					if contracts.calls != callsBefore+1 || contracts.tenant != actor.tenant {
						t.Fatal("report did not use the authenticated tenant")
					}
				} else if contracts.calls != callsBefore {
					t.Fatal("unauthorized request reached export")
				}
			}
		})
	}
}

type reportCostFixture struct {
	prv1.SourcingServiceClient
	currency string
	covered  bool
}

func (s reportCostFixture) GetReportContractCosts(context.Context, *prv1.GetReportContractCostsRequest, ...grpc.CallOption) (*prv1.GetReportContractCostsResponse, error) {
	return &prv1.GetReportContractCostsResponse{Covered: s.covered, Costs: []*prv1.ReportCostDocument{
		{Number: "PO-1", Currency: "USD", Amount: "40.10", Kind: "PROCUREMENT"},
		{Number: "LOG-1", Currency: s.currency, Amount: "12.25", Kind: "LOGISTICS"},
	}}, nil
}

func TestBusinessReportMarginRequiresCoveredSameCurrencyCosts(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/business-reports/contracts/10", nil)
	for _, tt := range []struct {
		currency string
		covered  bool
		want     string
	}{
		{"USD", true, "47.65"}, {"USD", false, ""}, {"CNY", true, ""},
	} {
		server := &Server{Sourcing: reportCostFixture{currency: tt.currency, covered: tt.covered}}
		c := &exv1.BusinessReportContract{ContractId: 10, Currency: "USD", ContractAmount: "100"}
		report := &exv1.GetBusinessReportResponse{Contracts: []*exv1.BusinessReportContract{c}, InternalContractNo: "CT-1"}
		server.reportContractCosts(req, report)
		if c.EstimatedMargin != tt.want || len(c.Costs) != 2 || report.InternalContractNo != "" {
			t.Fatalf("case %+v: %v", tt, report)
		}
	}
}
