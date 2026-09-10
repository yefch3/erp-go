package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"

	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.ListOrders(r.Context(), &prv1.ListOrdersRequest{
		Page:   pageFromQuery(r),
		Status: r.URL.Query().Get("status"), Keyword: r.URL.Query().Get("keyword"),
		Unsent: r.URL.Query().Get("unsent") == "1",
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getOrder(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	resp, err := s.Orders.GetOrder(r.Context(), &prv1.GetOrderRequest{Id: id})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateOrderRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	if _, err := s.resolveActiveSupplier(r.Context(), req.GetSupplierId()); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	resp, err := s.Orders.CreateOrder(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateOrder(w http.ResponseWriter, r *http.Request) {
	req := &prv1.UpdateOrderRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	if _, err := s.resolveActiveSupplier(r.Context(), req.GetSupplierId()); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	req.Id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	resp, err := s.Orders.UpdateOrder(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) submitOrder(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	resp, err := s.Orders.SubmitOrder(r.Context(), &prv1.SubmitOrderRequest{Id: id})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) presignOrderContract(w http.ResponseWriter, r *http.Request) {
	req := &prv1.PresignOrderContractUploadRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Orders.PresignOrderContractUpload(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) saveOrderContract(w http.ResponseWriter, r *http.Request) {
	req := &prv1.SaveOrderContractRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Orders.SaveOrderContract(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) verifyOrderContract(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.VerifyOrderContract(r.Context(), &prv1.VerifyOrderContractRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) requestOrderPayment(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.RequestOrderPayment(r.Context(), &prv1.RequestOrderPaymentRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) cancelOrder(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CancelOrderRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	resp, err := s.Orders.CancelOrder(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) receiveOrder(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ReceiveOrderRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	resp, err := s.Orders.ReceiveOrder(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getOrderDocuments(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.GetOrderDocuments(r.Context(), &prv1.GetOrderDocumentsRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	format := r.URL.Query().Get("format")
	name, contentType, data := resp.GetXlsxFileName(), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", resp.GetXlsxData()
	if format == "pdf" {
		name, contentType, data = resp.GetPdfFileName(), "application/pdf", resp.GetPdfData()
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) sendPurchaseOrder(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RecipientEmail string `json:"recipient_email"`
		Subject        string `json:"subject"`
		Body           string `json:"body"`
		SenderMode     string `json:"sender_mode"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "BAD_JSON", "请求内容无效")
		return
	}
	poID := idFromPath(r)
	orderResp, err := s.Orders.GetOrder(r.Context(), &prv1.GetOrderRequest{Id: poID})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	order := orderResp.GetOrder()
	supplier, err := s.resolveActiveSupplier(r.Context(), order.GetSupplierId())
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if input.RecipientEmail == "" {
		input.RecipientEmail = supplier.GetContactEmail()
	}
	op, _ := grpcx.OperatorFromContext(r.Context())
	senderID, senderName := s.ProcurementMailSenderID, "采购公共邮箱"
	if input.SenderMode == "ME" {
		senderID, senderName = op.EmployeeID, op.Name
	} else if senderID > 0 && !s.senderBelongsToCaller(r.Context(), senderID) {
		// 采购公共邮箱配的是别家公司的员工。这里有「用我的邮箱」这条退路，
		// 所以话要把退路指出来。
		s.writeError(w, http.StatusConflict, "NT_PROCUREMENT_SENDER_NOT_IN_TENANT",
			"采购公共发件邮箱配置的是其他公司的员工，请改用我的邮箱发送")
		return
	}
	if senderID <= 0 {
		s.writeError(w, http.StatusConflict, "NT_PROCUREMENT_SENDER_NOT_CONFIGURED", "尚未配置采购公共发件邮箱，可选择使用我的邮箱")
		return
	}
	documents, err := s.Orders.GetOrderDocuments(r.Context(), &prv1.GetOrderDocumentsRequest{Id: poID})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	attempt, err := s.Orders.BeginOrderSend(r.Context(), &prv1.BeginOrderSendRequest{Id: poID, RecipientEmail: input.RecipientEmail, SenderEmployeeId: senderID, SenderName: senderName})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if input.Subject == "" {
		input.Subject = fmt.Sprintf("Purchase Order %s", order.GetPoNo())
	}
	if input.Body == "" {
		input.Body = fmt.Sprintf("Dear %s,\n\nPlease find our approved purchase order %s attached in Excel and PDF format. Please confirm quantity, price and delivery date.\n\nThank you.", order.GetSupplierName(), order.GetPoNo())
	}
	mailResp, sendErr := s.Emails.SendProcurementOrder(r.Context(), &mailv1.SendProcurementOrderRequest{SenderEmployeeId: senderID, RecipientName: order.GetSupplierName(), RecipientEmail: input.RecipientEmail, Subject: input.Subject, Body: input.Body, Attachments: []*mailv1.ProcurementOrderAttachment{{FileName: documents.GetXlsxFileName(), FileData: documents.GetXlsxData(), ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"}, {FileName: documents.GetPdfFileName(), FileData: documents.GetPdfData(), ContentType: "application/pdf"}}})
	attachmentNames := []string{documents.GetXlsxFileName(), documents.GetPdfFileName()}
	if sendErr != nil {
		_, completeErr := s.Orders.CompleteOrderSend(r.Context(), &prv1.CompleteOrderSendRequest{Id: poID, AttemptId: attempt.GetAttemptId(), ErrorMessage: grpcMessage(sendErr), AttachmentNames: attachmentNames, TemplateVersion: documents.GetTemplateVersion()})
		if completeErr != nil {
			s.Log.Error("record purchase order send failure", "po_id", poID, "err", completeErr)
		}
		s.writeGRPCError(w, sendErr)
		return
	}
	complete, err := s.Orders.CompleteOrderSend(r.Context(), &prv1.CompleteOrderSendRequest{Id: poID, AttemptId: attempt.GetAttemptId(), Success: true, CampaignId: mailResp.GetCampaignId(), CampaignNo: mailResp.GetCampaignNo(), AttachmentNames: attachmentNames, TemplateVersion: documents.GetTemplateVersion()})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, complete)
}

func (s *Server) getOrderExecution(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.GetOrderExecution(r.Context(), &prv1.GetOrderExecutionRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) recordSupplierConfirmation(w http.ResponseWriter, r *http.Request) {
	req := &prv1.RecordSupplierConfirmationRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Orders.RecordSupplierConfirmation(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) saveProductionMilestone(w http.ResponseWriter, r *http.Request) {
	req := &prv1.SaveProductionMilestoneRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Orders.SaveProductionMilestone(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) reportReceiptException(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ReportReceiptExceptionRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Orders.ReportReceiptException(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) resolveReceiptException(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ResolveReceiptExceptionRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	req.ExceptionId, _ = strconv.ParseInt(chi.URLParam(r, "exceptionId"), 10, 64)
	resp, err := s.Orders.ResolveReceiptException(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) recordInspection(w http.ResponseWriter, r *http.Request) {
	req := &prv1.RecordInspectionRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Orders.RecordInspection(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) resolveInspection(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ResolveInspectionRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	req.InspectionId, _ = strconv.ParseInt(chi.URLParam(r, "inspectionId"), 10, 64)
	resp, err := s.Orders.ResolveInspection(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) closePurchaseOrder(w http.ResponseWriter, r *http.Request) {
	// 货没到齐时，前端会带上「剩下的怎么办」和原因；收齐的单两个都是空的。
	req := &prv1.CloseOrderRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Orders.CloseOrder(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
