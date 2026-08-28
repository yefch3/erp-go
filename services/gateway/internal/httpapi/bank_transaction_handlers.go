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

// recordBankTransaction 手工登记一行流水。校验全在采购服务里（方向、金额、
// 日期、流水号去重、账户启用），这里只是转发——和 CSV 导入落的是同一张表、
// 同一套唯一约束，重复的流水号会被一句人话拒绝。
func (s *Server) recordBankTransaction(w http.ResponseWriter, r *http.Request) {
	req := &prv1.RecordBankTransactionRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Orders.RecordBankTransaction(r.Context(), req)
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
		Ownership: r.URL.Query().Get("ownership"),
		// ?ownership_pending=1 只出「还没人认领的」。单独一个开关而不是让
		// ownership="" 兼职：空串已经是「不筛」的意思了。
		OwnershipPending: r.URL.Query().Get("ownership_pending") == "1",
		// 认领状态："" 不筛 / OPEN 还没认领完 / CLAIMED 认领完了。
		// 付款对账的「待处理 / 已核销」两档走这里。
		ClaimStatus: r.URL.Query().Get("claim_status"),
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

// settleBankTransactionToSupplier 付款对账的直接核销：这条流水结算/退了
// 哪些发票或采购单。采购服务在一个事务里自动建影子付款单并核销；守门
// （归属、认领互斥、币种、净额天花板）全在那边。
func (s *Server) settleBankTransactionToSupplier(w http.ResponseWriter, r *http.Request) {
	req := &prv1.SettleBankTransactionToSupplierRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.TxnId = idFromPath(r)
	resp, err := s.Orders.SettleBankTransactionToSupplier(r.Context(), req)
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

// setBankTransactionOwnership 记下这笔银行流水是谁那条线上的。
//
// 客户 / 供应商 / 退税 / 不用核销 / 空=待处理。归属决定它接着能被谁核销，
// 所以它**不是** direction 的同义词——供应商退款是进账却归供应商。
func (s *Server) setBankTransactionOwnership(w http.ResponseWriter, r *http.Request) {
	req := &prv1.SetBankTransactionOwnershipRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.TxnId = idFromPath(r)
	resp, err := s.Orders.SetBankTransactionOwnership(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
