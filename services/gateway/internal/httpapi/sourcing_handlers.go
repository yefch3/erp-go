package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"

	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	pdv1 "github.com/sgao19/erp-go/gen/go/erp/product/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

func (s *Server) listSourcingCases(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ListCases(r.Context(), &prv1.ListCasesRequest{
		Page: pageFromQuery(r), Status: r.URL.Query().Get("status"), Keyword: r.URL.Query().Get("keyword"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getFactoryRFQWorkbook(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.GetFactoryRfqWorkbook(r.Context(), &prv1.GetFactoryRfqWorkbookRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(resp.GetFileName()))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp.GetFileData())
}

func (s *Server) importSupplierQuoteWorkbook(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ImportSupplierQuoteWorkbookRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.FactoryRfqId = idFromPath(r)
	resp, err := s.Sourcing.ImportSupplierQuoteWorkbook(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) sendFactoryRFQ(w http.ResponseWriter, r *http.Request) {
	if s.ProcurementMailSenderID <= 0 {
		s.writeError(w, http.StatusConflict, "NT_PROCUREMENT_SENDER_NOT_CONFIGURED", "尚未配置采购公共发件邮箱")
		return
	}
	var input struct {
		RecipientEmail string `json:"recipient_email"`
		Subject        string `json:"subject"`
		Body           string `json:"body"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "BAD_JSON", "请求内容无效")
		return
	}
	book, err := s.Sourcing.GetFactoryRfqWorkbook(r.Context(), &prv1.GetFactoryRfqWorkbookRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if input.RecipientEmail == "" {
		input.RecipientEmail = book.GetContactEmail()
	}
	if input.Subject == "" {
		input.Subject = fmt.Sprintf("Request for quotation %s", book.GetRfqNo())
	}
	if input.Body == "" {
		input.Body = fmt.Sprintf("Dear %s,\n\nPlease complete the attached quotation workbook for %s and return it without changing RFQ No, Line ID, Quantity or Unit.\n\nThank you.", book.GetSupplierName(), book.GetRfqNo())
	}
	resp, err := s.Emails.SendProcurementRfq(r.Context(), &mailv1.SendProcurementRfqRequest{SenderEmployeeId: s.ProcurementMailSenderID,
		RecipientName: book.GetSupplierName(), RecipientEmail: input.RecipientEmail, Subject: input.Subject, Body: input.Body,
		FileName: book.GetFileName(), FileData: book.GetFileData()})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if _, err := s.Sourcing.MarkFactoryRfqSent(r.Context(), &prv1.MarkFactoryRfqSentRequest{Id: idFromPath(r)}); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getSourcingCase(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.GetCase(r.Context(), &prv1.GetCaseRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listSourcingCaseChanges(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ListCaseChanges(r.Context(), &prv1.ListCaseChangesRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createSourcingCase(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateCaseRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	if _, err := s.resolveActiveCustomer(r.Context(), req.GetCustomerId()); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	resp, err := s.Sourcing.CreateCase(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) addSourcingLine(w http.ResponseWriter, r *http.Request) {
	req := &prv1.AddLineRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.AddLine(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) confirmSourcingLines(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ConfirmLinesRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.ConfirmLines(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) reviewSourcingLine(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ReviewLineRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	req.LineId, _ = strconv.ParseInt(chi.URLParam(r, "lineId"), 10, 64)
	if req.GetDecision() == "CONFIRMED" {
		product, err := s.Catalog.GetProduct(r.Context(), &pdv1.GetProductRequest{Id: req.GetProductId()})
		if err != nil {
			s.writeGRPCError(w, err)
			return
		}
		if product.GetProduct().GetStatus() != "ACTIVE" {
			s.writeError(w, http.StatusConflict, "SC_PRODUCT_INACTIVE", "所选产品已停用")
			return
		}
		req.UomId = product.GetProduct().GetBaseUomId()
		if req.GetSkuId() != 0 {
			found := false
			for _, sku := range product.GetSkus() {
				if sku.GetId() == req.GetSkuId() && sku.GetStatus() == "ACTIVE" {
					found = true
					break
				}
			}
			if !found {
				s.writeError(w, http.StatusBadRequest, "SC_SKU_INVALID", "所选 SKU 不属于当前产品或已停用")
				return
			}
		}
	}
	resp, err := s.Sourcing.ReviewLine(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createFactoryRFQ(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateFactoryRfqRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	if _, err := s.resolveActiveSupplier(r.Context(), req.GetSupplierId()); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	factoryResp, err := s.Suppliers.GetFactory(r.Context(), &mdv1.GetFactoryRequest{Id: req.GetFactoryId()})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	factory := factoryResp.GetFactory()
	if factory.GetStatus() != "COOPERATING" || factory.GetSupplierId() != req.GetSupplierId() {
		s.writeError(w, http.StatusConflict, "SC_FACTORY_NOT_COOPERATING", "请选择当前供应商下合作中的工厂")
		return
	}
	req.FactoryCode = factory.GetCode()
	req.FactoryName = factory.GetNameZh()
	if req.FactoryName == "" {
		req.FactoryName = factory.GetNameEn()
	}
	if req.GetContactEmail() == "" {
		contacts, contactErr := s.Suppliers.ListFactoryContacts(r.Context(), &mdv1.ListFactoryContactsRequest{FactoryId: factory.GetId()})
		if contactErr == nil {
			for _, contact := range contacts.GetContacts() {
				if contact.GetIsPrimary() && contact.GetStatus() == "ACTIVE" {
					req.ContactEmail = contact.GetEmail()
					break
				}
			}
		}
	}
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.CreateFactoryRfq(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateFactoryRFQ(w http.ResponseWriter, r *http.Request) {
	req := &prv1.UpdateFactoryRfqRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Sourcing.UpdateFactoryRfq(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listFactoryRFQs(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ListFactoryRfqs(r.Context(), &prv1.ListFactoryRfqsRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createSupplierQuote(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateSupplierQuoteRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.FactoryRfqId = idFromPath(r)
	resp, err := s.Sourcing.CreateSupplierQuote(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listSupplierQuoteComparison(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ListSupplierQuoteComparison(r.Context(), &prv1.ListSupplierQuoteComparisonRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	allowed, err := s.hasPermission(r, "procurement:sourcing:price")
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if !allowed {
		redactSupplierQuotePrices(resp)
	}
	s.writeProto(w, resp)
}

func (s *Server) listCostScenarios(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ListCostScenarios(r.Context(), &prv1.ListCostScenariosRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	allowed, err := s.hasPermission(r, "procurement:sourcing:price")
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if !allowed {
		redactCostScenarioList(resp)
	}
	s.writeProto(w, resp)
}

func (s *Server) createCostScenario(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateCostScenarioRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.CreateCostScenario(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	allowed, err := s.hasPermission(r, "procurement:sourcing:price")
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if !allowed {
		redactCostScenario(resp.GetCostScenario())
	}
	s.writeProto(w, resp)
}

func (s *Server) hasPermission(r *http.Request, code string) (bool, error) {
	op, ok := grpcx.OperatorFromContext(r.Context())
	if !ok || op.EmployeeID == 0 {
		return false, nil
	}
	resp, err := s.Access.CheckPermission(r.Context(), &iamv1.CheckPermissionRequest{
		EmployeeId: op.EmployeeID, PermissionCode: code,
	})
	if err != nil {
		return false, err
	}
	return resp.GetAllowed(), nil
}

func redactSupplierQuotePrices(resp *prv1.ListSupplierQuoteComparisonResponse) {
	for _, line := range resp.GetLines() {
		line.UnitPrice = ""
		line.Amount = ""
	}
}

func redactCostScenarioList(resp *prv1.ListCostScenariosResponse) {
	for _, scenario := range resp.GetCostScenarios() {
		redactCostScenario(scenario)
	}
}

func redactCostScenario(scenario *prv1.CostScenario) {
	if scenario == nil {
		return
	}
	scenario.MarginType = ""
	scenario.MarginValue = ""
	scenario.FxRate = ""
	scenario.FxRateAt = ""
	scenario.FxSource = ""
	scenario.ProductTotal = ""
	scenario.ChargeTotal = ""
	scenario.LandedTotal = ""
	scenario.MarginTotal = ""
	scenario.Charges = nil
	for _, line := range scenario.GetLines() {
		line.SupplierQuoteLineId = 0
		line.SupplierName = ""
		line.SourceCurrency = ""
		line.SourceUnitPrice = ""
		line.SourceFxRate = ""
		line.TargetFxRate = ""
		line.ProductCost = ""
		line.AllocatedCharge = ""
		line.LandedCost = ""
		line.MarginAmount = ""
	}
}

func (s *Server) getCostScenario(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.GetCostScenario(r.Context(), &prv1.GetCostScenarioRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) confirmCostScenario(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ConfirmCostScenario(r.Context(), &prv1.ConfirmCostScenarioRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createCustomerQuotationFromCost(w http.ResponseWriter, r *http.Request) {
	scenarioID := idFromPath(r)
	var input struct {
		CustomerID int64 `json:"customer_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		s.writeError(w, http.StatusBadRequest, "BAD_JSON", "请求内容无效")
		return
	}
	draft, err := s.Sourcing.PrepareCustomerQuotation(r.Context(), &prv1.PrepareCustomerQuotationRequest{Id: scenarioID})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if draft.GetExistingQuotationId() != 0 {
		s.writeProto(w, &prv1.LinkCustomerQuotationResponse{QuotationId: draft.GetExistingQuotationId(), QuoteNo: draft.GetExistingQuoteNo()})
		return
	}
	items := make([]*exv1.ItemInput, 0, len(draft.GetLines()))
	for _, line := range draft.GetLines() {
		items = append(items, &exv1.ItemInput{
			ProductId: line.GetProductId(), SkuId: line.GetSkuId(), Spec: line.GetSpec(),
			Qty: line.GetQty(), UnitPrice: line.GetUnitPrice(), Remark: line.GetRemark(),
			SourceCostScenarioLineId: line.GetCostScenarioLineId(),
		})
	}
	customerID := draft.GetCustomerId()
	if input.CustomerID != 0 {
		customerID = input.CustomerID
	}
	created, err := s.Quotations.CreateQuotation(r.Context(), &exv1.CreateQuotationRequest{
		CustomerId: customerID, Currency: draft.GetCurrency(), Incoterm: draft.GetIncoterm(),
		PortOfLoading: draft.GetPortOfLoading(), PortOfDischarge: draft.GetPortOfDischarge(),
		PaymentMethod: draft.GetPaymentMethod(), Remark: "Generated from " + draft.GetCostScenarioNo(), Items: items,
		SourceCostScenarioId: draft.GetCostScenarioId(), SourceCostScenarioNo: draft.GetCostScenarioNo(),
		SourceSourcingCaseId: draft.GetSourcingCaseId(),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	quotation := created.GetQuotation()
	linked, err := s.Sourcing.LinkCustomerQuotation(r.Context(), &prv1.LinkCustomerQuotationRequest{
		Id: scenarioID, QuotationId: quotation.GetId(), QuoteNo: quotation.GetQuoteNo(),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, linked)
}
