package httpapi

import (
	"net/http"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

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
		// ?deleted=1 只出已删除的那些。和上面几个筛选项不同，它是个**开关**：
		// 已删除是和日常列表并列的一个入口，不是在同一张表上多勾一个框。
		Deleted: r.URL.Query().Get("deleted") == "1",
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// listBankTransactionChanges 这一行被改过什么。
func (s *Server) listBankTransactionChanges(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.ListBankTransactionChanges(r.Context(),
		&prv1.ListBankTransactionChangesRequest{TxnId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
