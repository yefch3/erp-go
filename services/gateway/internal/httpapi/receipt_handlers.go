package httpapi

import (
	"net/http"
	"strconv"

	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
)

func (s *Server) listBankAccounts(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Receipts.ListBankAccounts(r.Context(), &exv1.ListBankAccountsRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createBankAccount(w http.ResponseWriter, r *http.Request) {
	req := &exv1.CreateBankAccountRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Receipts.CreateBankAccount(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) listReceivableReminders(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.ParseInt(r.URL.Query().Get("limit"), 10, 32)
	resp, err := s.Receipts.ListReceivableReminders(r.Context(), &exv1.ListReceivableRemindersRequest{
		UnreadOnly: r.URL.Query().Get("unread") == "1",
		Limit:      int32(limit),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) markReceivableRemindersRead(w http.ResponseWriter, r *http.Request) {
	req := &exv1.MarkReceivableRemindersReadRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Receipts.MarkReceivableRemindersRead(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// 应收到期清单（E1）。围栏在服务端（按销售负责人），这里只转参数。
func (s *Server) listReceivableDue(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	resp, err := s.Receipts.ListReceivableDue(r.Context(), &exv1.ListReceivableDueRequest{
		Page:        pageFromQuery(r),
		OverdueOnly: q.Get("overdue") == "1",
		UnsetOnly:   q.Get("unset") == "1",
		ClosedOnly:  q.Get("closed") == "1",
		Keyword:     q.Get("keyword"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// recordContractReceipt 在待核销页上记一笔钱：选一张合同，金额和到账日期
// 由员工手填。**不连银行流水**——那本账只用来存银行给的 statement。
func (s *Server) recordContractReceipt(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Amount     string `json:"amount"`
		IsRefund   bool   `json:"isRefund"`
		ReceivedAt string `json:"receivedAt"`
		Note       string `json:"note"`
	}
	if !s.decodeJSON(w, r, &body) {
		return
	}
	resp, err := s.Receipts.RecordContractReceipt(r.Context(), &exv1.RecordContractReceiptRequest{
		ContractId: idFromPath(r), Amount: body.Amount, IsRefund: body.IsRefund,
		ReceivedAt: body.ReceivedAt, Note: body.Note,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createManualReceivable(w http.ResponseWriter, r *http.Request) {
	req := &exv1.CreateManualReceivableRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Receipts.CreateManualReceivable(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateManualReceivable(w http.ResponseWriter, r *http.Request) {
	req := &exv1.UpdateManualReceivableRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.ContractId = idFromPath(r)
	resp, err := s.Receipts.UpdateManualReceivable(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// reverseContractReceipt 冲销一笔记错的收款。写反向记录，不删原记录。
func (s *Server) reverseContractReceipt(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	if !s.decodeJSON(w, r, &body) {
		return
	}
	resp, err := s.Receipts.ReverseContractReceipt(r.Context(), &exv1.ReverseContractReceiptRequest{
		EntryId: idFromPath(r), Reason: body.Reason,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) closeReceivable(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Category string `json:"category"`
		Note     string `json:"note"`
	}
	if !s.decodeJSON(w, r, &body) {
		return
	}
	resp, err := s.Receipts.CloseReceivable(r.Context(), &exv1.CloseReceivableRequest{
		ContractId: idFromPath(r), Category: body.Category, Note: body.Note,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) setReceivableDueDate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DueDate string `json:"dueDate"`
		Reason  string `json:"reason"`
	}
	if !s.decodeJSON(w, r, &body) {
		return
	}
	resp, err := s.Receipts.SetReceivableDueDate(r.Context(), &exv1.SetReceivableDueDateRequest{
		ContractId: idFromPath(r), DueDate: body.DueDate, Reason: body.Reason,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) confirmContractExecutionCondition(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ConditionType string `json:"conditionType"`
		Note          string `json:"note"`
	}
	if !s.decodeJSON(w, r, &body) {
		return
	}
	resp, err := s.Receipts.ConfirmContractExecutionCondition(r.Context(), &exv1.ConfirmContractExecutionConditionRequest{
		ContractId: idFromPath(r), ConditionType: body.ConditionType, Note: body.Note,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) reopenReceivable(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	if !s.decodeJSON(w, r, &body) {
		return
	}
	resp, err := s.Receipts.ReopenReceivable(r.Context(), &exv1.ReopenReceivableRequest{
		ContractId: idFromPath(r), Reason: body.Reason,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) getContractReceipts(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Receipts.GetContractReceipts(r.Context(),
		&exv1.GetContractReceiptsRequest{ContractId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
