package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

// 供应商对账：列出采购订单，员工手填核销数字、手动确认完成。
//
// 页签用 ?view=done 而不是 ?closed_only=1：前端地址栏和 API 用同一个词，
// 中间少一层翻译就少一个对不上的地方（客户对账那边同款）。

func (s *Server) listSupplierRecon(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("page_size"))
	resp, err := s.Orders.ListSupplierRecon(r.Context(), &prv1.ListSupplierReconRequest{
		Keyword:    q.Get("keyword"),
		ClosedOnly: q.Get("view") == "done",
		// 两个互斥的筛子，和客户对账页同一套参数名。
		OverdueOnly: q.Get("overdue") == "1",
		UnsetOnly:   q.Get("unset") == "1",
		Page:        int32(page),
		PageSize:    int32(size),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listPurchaseOrderPayments(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.ListPurchaseOrderPayments(r.Context(),
		&prv1.ListPurchaseOrderPaymentsRequest{PoId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) recordPurchaseOrderPayment(w http.ResponseWriter, r *http.Request) {
	req := &prv1.RecordPurchaseOrderPaymentRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.PoId = idFromPath(r)
	resp, err := s.Orders.RecordPurchaseOrderPayment(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createManualPayable(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateManualPayableRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Orders.CreateManualPayable(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateManualPayable(w http.ResponseWriter, r *http.Request) {
	req := &prv1.UpdateManualPayableRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.PoId = idFromPath(r)
	resp, err := s.Orders.UpdateManualPayable(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) reversePurchaseOrderPayment(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ReversePurchaseOrderPaymentRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.AllocationId, _ = strconv.ParseInt(chi.URLParam(r, "allocationId"), 10, 64)
	// 冲完之后回哪一行。挂发票的核销行本身不指向任何采购单，地址里这一段
	// 是唯一的来源。
	req.PoId = idFromPath(r)
	resp, err := s.Orders.ReversePurchaseOrderPayment(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) closePurchaseOrderPayment(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ClosePurchaseOrderPaymentRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.PoId = idFromPath(r)
	resp, err := s.Orders.ClosePurchaseOrderPayment(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) reopenPurchaseOrderPayment(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ReopenPurchaseOrderPaymentRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.PoId = idFromPath(r)
	resp, err := s.Orders.ReopenPurchaseOrderPayment(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// ── 挂在采购单上的凭证 ────────────────────────────────────
//
// 发票扫描件、水单、退款回执。供应商发票页下线之后，「留凭证」这件事搬到
// 对账页上——一张采购单可以有好几份。

func (s *Server) listReconFiles(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.ListReconFiles(r.Context(),
		&prv1.ListReconFilesRequest{PoId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) presignReconFile(w http.ResponseWriter, r *http.Request) {
	req := &prv1.PresignReconFileRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.PoId = idFromPath(r)
	resp, err := s.Orders.PresignReconFile(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) setPayableDueDate(w http.ResponseWriter, r *http.Request) {
	req := &prv1.SetPayableDueDateRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.PoId = idFromPath(r)
	resp, err := s.Orders.SetPayableDueDate(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) attachReconFile(w http.ResponseWriter, r *http.Request) {
	req := &prv1.AttachReconFileRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.PoId = idFromPath(r)
	resp, err := s.Orders.AttachReconFile(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) removeReconFile(w http.ResponseWriter, r *http.Request) {
	fileID, _ := strconv.ParseInt(chi.URLParam(r, "fileId"), 10, 64)
	resp, err := s.Orders.RemoveReconFile(r.Context(),
		&prv1.RemoveReconFileRequest{FileId: fileID})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
