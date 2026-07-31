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

func (s *Server) listTransactions(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Receipts.ListTransactions(r.Context(), &exv1.ListTransactionsRequest{
		Page:        pageFromQuery(r),
		Disposition: r.URL.Query().Get("disposition"),
		Direction:   r.URL.Query().Get("direction"),
		Keyword:     r.URL.Query().Get("keyword"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getTransaction(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Receipts.GetTransaction(r.Context(), &exv1.GetTransactionRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) recordTransaction(w http.ResponseWriter, r *http.Request) {
	req := &exv1.RecordTransactionRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Receipts.RecordTransaction(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) allocateReceipt(w http.ResponseWriter, r *http.Request) {
	req := &exv1.AllocateReceiptRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.TransactionId = idFromPath(r)
	resp, err := s.Receipts.AllocateReceipt(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) reverseAllocation(w http.ResponseWriter, r *http.Request) {
	req := &exv1.ReverseAllocationRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.AllocationId = idFromPath(r)
	resp, err := s.Receipts.ReverseAllocation(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) markTransactionIrrelevant(w http.ResponseWriter, r *http.Request) {
	req := &exv1.MarkIrrelevantRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.TransactionId = idFromPath(r)
	resp, err := s.Receipts.MarkIrrelevant(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) reopenTransaction(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Receipts.ReopenTransaction(r.Context(),
		&exv1.ReopenTransactionRequest{TransactionId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listOpenReceivables(w http.ResponseWriter, r *http.Request) {
	customerID, _ := strconv.ParseInt(r.URL.Query().Get("customer_id"), 10, 64)
	resp, err := s.Receipts.ListOpenReceivables(r.Context(), &exv1.ListOpenReceivablesRequest{
		Currency:   r.URL.Query().Get("currency"),
		CustomerId: customerID,
		Keyword:    r.URL.Query().Get("keyword"),
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
