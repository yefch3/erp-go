package httpapi

import (
	"net/http"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

// Bank statement rows: imported from the CSV the bank portal exports,
// matched against supplier payments. The CSV travels as base64 inside the
// JSON body — protojson reads bytes fields that way natively, and bank
// statements are small enough that nobody needs multipart.

func (s *Server) importBankStatement(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ImportBankStatementRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Orders.ImportBankStatement(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listBankTransactions(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.ListBankTransactions(r.Context(), &prv1.ListBankTransactionsRequest{
		Page:      pageFromQuery(r),
		Status:    r.URL.Query().Get("status"),
		Direction: r.URL.Query().Get("direction"),
		Keyword:   r.URL.Query().Get("keyword"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) matchBankTransaction(w http.ResponseWriter, r *http.Request) {
	req := &prv1.MatchBankTransactionRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.TxnId = idFromPath(r)
	resp, err := s.Orders.MatchBankTransaction(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) unmatchBankTransaction(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.UnmatchBankTransaction(r.Context(), &prv1.UnmatchBankTransactionRequest{TxnId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
