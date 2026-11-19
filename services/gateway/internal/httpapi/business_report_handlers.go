package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/shopspring/decimal"
)

const businessReportPermission = "report:business:read"

func (s *Server) businessReport(view string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		parse := func(key string) (int32, bool) {
			value := query.Get(key)
			if value == "" {
				return 0, true
			}
			n, err := strconv.ParseInt(value, 10, 32)
			return int32(n), err == nil && n > 0
		}
		year, validYear := parse("year")
		quarter, validQuarter := parse("quarter")
		page, validPage := parse("page")
		if !validYear || !validQuarter || !validPage {
			s.writeError(w, http.StatusBadRequest, "REPORT_QUERY_INVALID", "报表查询条件无效")
			return
		}
		req := &exv1.GetBusinessReportRequest{Year: year, Quarter: quarter, Page: page, View: view}
		if view == "contract" {
			id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
			if err != nil || id <= 0 {
				s.writeError(w, http.StatusBadRequest, "REPORT_CONTRACT_INVALID", "合同编号无效")
				return
			}
			req.ContractId = id
		}
		w.Header().Set("Cache-Control", "no-store")
		report, err := s.Contracts.GetBusinessReport(r.Context(), req)
		if err != nil {
			s.writeGRPCError(w, err)
			return
		}

		if view == "sales" {
			if err := s.reportInquiryCounts(r, report); err != nil {
				s.writeGRPCError(w, err)
				return
			}
		}
		if view == "contract" && len(report.GetContracts()) == 1 {
			s.reportContractCosts(r, report)
		}
		s.writeProto(w, report)
	}
}

func (s *Server) reportInquiryCounts(r *http.Request, report *exv1.GetBusinessReportResponse) error {
	inquiries, err := s.Sourcing.GetReportInquiries(r.Context(), &prv1.GetReportInquiriesRequest{
		StartDate: report.GetStartDate(), CutoffDate: report.GetCutoffDate(),
	})
	if err != nil {
		return err
	}
	staff := make(map[int64]*exv1.BusinessReportSales, len(report.Sales))
	for _, person := range report.Sales {
		staff[person.EmployeeId] = person
	}
	for _, count := range inquiries.GetEmployees() {
		person := staff[count.GetEmployeeId()]
		if person == nil {
			person = &exv1.BusinessReportSales{EmployeeId: count.GetEmployeeId(), Employee: count.GetEmployee()}
			report.Sales = append(report.Sales, person)
		} else if person.Employee == "" {
			person.Employee = count.GetEmployee()
		}
		person.InquiryCount = count.GetCount()
	}
	return nil
}

func (s *Server) reportContractCosts(r *http.Request, report *exv1.GetBusinessReportResponse) {
	contract := report.GetContracts()[0]
	data, err := s.Sourcing.GetReportContractCosts(r.Context(), &prv1.GetReportContractCostsRequest{
		ContractId: contract.GetContractId(), InternalContractNo: report.GetInternalContractNo(),
		CutoffDate: report.GetCutoffDate(), ItemQuantities: report.GetContractItemQuantities(), FreightBatches: report.GetFreightBatches(),
	})
	// These inputs identify the cross-service records and are internal to the
	// report calculation.
	report.InternalContractNo = ""
	report.ContractItemQuantities = nil
	report.FreightBatches = nil
	if err != nil {
		if s.Log != nil {
			s.Log.Warn("report costs unavailable", "error", err)
		}
		contract.CostUnavailable = true
		return
	}
	var total decimal.Decimal
	covered := data.GetCovered() && len(data.GetCosts()) > 0
	for _, row := range data.GetCosts() {
		contract.Costs = append(contract.Costs, &exv1.BusinessReportCost{
			Number: row.GetNumber(), Currency: row.GetCurrency(), Amount: row.GetAmount(), Kind: row.GetKind(),
		})
		amount, err := decimal.NewFromString(row.GetAmount())
		if err != nil || !amount.IsPositive() || row.GetCurrency() != contract.GetCurrency() {
			covered = false
			continue
		}
		total = total.Add(amount)
	}
	if revenue, err := decimal.NewFromString(contract.GetContractAmount()); covered && err == nil {
		contract.EstimatedMargin = revenue.Sub(total).StringFixed(2)
	}
}
