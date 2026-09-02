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
	senderID, ok := s.procurementSenderFor(w, r)
	if !ok {
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
	resp, err := s.Emails.SendProcurementRfq(r.Context(), &mailv1.SendProcurementRfqRequest{SenderEmployeeId: senderID,
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

// getSalesProcurementProgress 只返回销售协作所需的采购进度、经理已定向提交的
// 统一方案和已确认报价依据。未入选供应商、采购比较过程与成本拆分仍不进入响应。
func (s *Server) getSalesProcurementProgress(w http.ResponseWriter, r *http.Request) {
	caseID := idFromPath(r)
	caseResp, err := s.Sourcing.GetCase(r.Context(), &prv1.GetCaseRequest{Id: caseID})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	rfqResp, err := s.Sourcing.ListFactoryRfqs(r.Context(), &prv1.ListFactoryRfqsRequest{CaseId: caseID})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	costResp, err := s.Sourcing.ListCostScenarios(r.Context(), &prv1.ListCostScenariosRequest{CaseId: caseID})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	planResp, err := s.Sourcing.ListProcurementPlans(r.Context(), &prv1.ListProcurementPlansRequest{CaseId: caseID})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}

	s.writeJSON(w, salesProcurementProgressPayload(caseResp, rfqResp, costResp, planResp))
}

// salesProcurementProgressPayload 集中构造销售可见的采购进度摘要。
// 这里刻意采用字段白名单，新增采购字段时不会自动泄露给销售。
func salesProcurementProgressPayload(caseResp *prv1.GetCaseResponse, rfqResp *prv1.ListFactoryRfqsResponse, costResp *prv1.ListCostScenariosResponse, planResp *prv1.ListProcurementPlansResponse) map[string]any {
	quoted := 0
	for _, rfq := range rfqResp.GetFactoryRfqs() {
		if rfq.GetStatus() == "QUOTED" || rfq.GetStatus() == "PARTIALLY_QUOTED" || rfq.GetStatus() == "CLOSED" {
			quoted++
		}
	}
	confirmed := make([]map[string]any, 0)
	for _, scenario := range costResp.GetCostScenarios() {
		if scenario.GetStatus() != "CONFIRMED" || scenario.GetSubmittedToSalesAt() == "" {
			continue
		}
		// Sales needs the already-approved customer-facing unit price to build the
		// negotiation draft.  Keep this a strict allow-list: no supplier price,
		// landed cost, margin or charge allocation crosses the boundary.
		lines := make([]map[string]any, 0, len(scenario.GetLines()))
		for _, line := range scenario.GetLines() {
			lines = append(lines, map[string]any{
				"sourcingLineId":    line.GetSourcingLineId(),
				"customerUnitPrice": line.GetCustomerUnitPrice(),
			})
		}
		confirmed = append(confirmed, map[string]any{
			"id": scenario.GetId(), "scenarioNo": scenario.GetScenarioNo(),
			"versionNo": scenario.GetVersionNo(), "currency": scenario.GetCurrency(),
			"requirementVersionNo": scenario.GetRequirementVersionNo(),
			"submittedToSalesAt":   scenario.GetSubmittedToSalesAt(),
			"status":               "CONFIRMED",
			"customerTotal":        scenario.GetCustomerTotal(),
			"customerQuotationId":  scenario.GetCustomerQuotationId(),
			"lines":                lines,
		})
	}
	submittedPlans := make([]map[string]any, 0)
	for _, plan := range planResp.GetProcurementPlans() {
		if plan.GetStatus() == "SUBMITTED_TO_SALES" {
			submittedPlans = append(submittedPlans, salesProcurementPlanPayload(plan))
		}
	}
	return map[string]any{
		"status":   caseResp.GetSourcingCase().GetStatus(),
		"rfqCount": len(rfqResp.GetFactoryRfqs()), "quotedRfqCount": quoted,
		"confirmedCosts": confirmed, "procurementPlans": submittedPlans,
	}
}

// 这条接口使用 encoding/json 包装多个来源，不能直接把 protobuf 结构塞进 map：
// protobuf 的 Go JSON tag 是 snake_case，而前端统一消费 lowerCamelCase。显式白名单
// 同时解决字段命名和销售数据边界，避免出现“有两行方案但内容全空”的假成功。
func salesProcurementPlanPayload(plan *prv1.ProcurementPlan) map[string]any {
	items := make([]map[string]any, 0, len(plan.GetItems()))
	for _, item := range plan.GetItems() {
		items = append(items, map[string]any{
			"id": item.GetId(), "sourcingLineId": item.GetSourcingLineId(),
			"selectionType": item.GetSelectionType(), "priority": item.GetPriority(),
			"reason": item.GetReason(), "risk": item.GetRisk(),
			"supplierName": item.GetSupplierName(), "factoryName": item.GetFactoryName(),
			"buyerName": item.GetBuyerName(), "productName": item.GetProductName(),
			"currency": item.GetCurrency(), "unitPrice": item.GetUnitPrice(),
			"availableQty": item.GetAvailableQty(), "uomCode": item.GetUomCode(),
			"moq": item.GetMoq(), "leadTime": item.GetLeadTime(),
			"paymentTerms": item.GetPaymentTerms(), "incoterm": item.GetIncoterm(),
			"validUntil": item.GetValidUntil(), "quoteVersionNo": item.GetQuoteVersionNo(),
		})
	}
	return map[string]any{
		"id": plan.GetId(), "caseId": plan.GetCaseId(), "planNo": plan.GetPlanNo(),
		"versionNo": plan.GetVersionNo(), "requirementVersionNo": plan.GetRequirementVersionNo(),
		"status": plan.GetStatus(), "managerNote": plan.GetManagerNote(),
		"confirmedByName": plan.GetConfirmedByName(), "confirmedAt": plan.GetConfirmedAt(),
		"submittedToSalesByName": plan.GetSubmittedToSalesByName(),
		"submittedToSalesAt":     plan.GetSubmittedToSalesAt(),
		"targetSalesName":        plan.GetTargetSalesName(), "createdAt": plan.GetCreatedAt(),
		"items": items,
	}
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
	customer, contact, err := s.resolveActiveCustomerContact(r.Context(), req.GetCustomerId(), req.GetContactId())
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	// 客户和联系人快照都以主数据为准，不信任浏览器传来的姓名和邮箱。
	// 这样既不会把甲客户的联系人挂到乙客户，也不会保存已经过期的邮箱。
	req.CustomerName = customer.GetName()
	req.ContactName = contact.GetName()
	req.ContactEmail = contact.GetEmail()
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

func (s *Server) acceptSourcingCase(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.AcceptCase(r.Context(), &prv1.AcceptCaseRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listSourcingParticipants(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ListCaseParticipants(r.Context(), &prv1.ListCaseParticipantsRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) joinSourcingCase(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.JoinCase(r.Context(), &prv1.JoinCaseRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) requestPrimarySourcingCase(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.RequestPrimaryBuyer(r.Context(), &prv1.RequestPrimaryBuyerRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) assignPrimarySourcingCase(w http.ResponseWriter, r *http.Request) {
	req := &prv1.AssignPrimaryBuyerRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.AssignPrimaryBuyer(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) returnSourcingCase(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ReturnCaseRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.ReturnCase(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) withdrawSourcingCase(w http.ResponseWriter, r *http.Request) {
	req := &prv1.WithdrawCaseRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.WithdrawCase(r.Context(), req)
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
	// 生产工厂是可选信息。只有用户明确选择工厂时，才校验工厂归属、
	// 合作状态并回填工厂联系人；向贸易商或供应商总部询价时允许留空。
	if req.GetFactoryId() != 0 {
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

func (s *Server) listOverdueFactoryRFQs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.ParseInt(r.URL.Query().Get("limit"), 10, 32)
	resp, err := s.Sourcing.ListOverdueFactoryRfqs(r.Context(), &prv1.ListOverdueFactoryRfqsRequest{Limit: int32(limit)})
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

func (s *Server) listProcurementPlans(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ListProcurementPlans(r.Context(), &prv1.ListProcurementPlansRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createProcurementPlan(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateProcurementPlanRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.CreateProcurementPlan(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getProcurementPlan(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.GetProcurementPlan(r.Context(), &prv1.GetProcurementPlanRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) submitProcurementPlanToSales(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.SubmitProcurementPlanToSales(r.Context(), &prv1.SubmitProcurementPlanToSalesRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listProcurementReworks(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ListProcurementReworks(r.Context(), &prv1.ListProcurementReworksRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createProcurementRework(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateProcurementReworkRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.CreateProcurementRework(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) resolveProcurementRework(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ResolveProcurementReworkRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Sourcing.ResolveProcurementRework(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
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
	req := &prv1.ConfirmCostScenarioRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Sourcing.ConfirmCostScenario(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) submitCostToSales(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.SubmitCostToSales(r.Context(), &prv1.SubmitCostToSalesRequest{Id: idFromPath(r)})
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
			SourceCostScenarioLineId: line.GetCostScenarioLineId(), ProductName: line.GetProductName(),
			UomCode: line.GetUomCode(),
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
