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

// 对账单那张纸：先要一个直传地址，浏览器把 PDF 直接传给对象存储，
// 传完再回来登记 key。文件本身一个字节都不经过网关。
func (s *Server) presignBankTransactionFile(w http.ResponseWriter, r *http.Request) {
	req := &prv1.PresignBankTransactionFileRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.TxnId = idFromPath(r)
	resp, err := s.Orders.PresignBankTransactionFile(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) attachBankTransactionFile(w http.ResponseWriter, r *http.Request) {
	req := &prv1.AttachBankTransactionFileRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.TxnId = idFromPath(r)
	resp, err := s.Orders.AttachBankTransactionFile(r.Context(), req)
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

// unmatchBankTransaction 解开一条历史匹配。
//
// 新建匹配的入口下线了，这一条留着——SetBankTransactionOwnership 遇到已匹配
// 的行会拒绝并让人「先取消匹配」，没有这条路那句话就是个做不到的指令。
func (s *Server) unmatchBankTransaction(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.UnmatchBankTransaction(r.Context(),
		&prv1.UnmatchBankTransactionRequest{TxnId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// deleteBankTransaction 归档一条流水。理由必填，在请求体里。
//
// 用 POST 而不是 DELETE：这一下要带着理由，而带 body 的 DELETE 在代理和
// 客户端那一层各家实现不一，有的直接把 body 丢掉——丢掉的后果是理由为空、
// 服务层拒绝，而人看到的是一句「请填写删除原因」，尽管他确实填了。
func (s *Server) deleteBankTransaction(w http.ResponseWriter, r *http.Request) {
	req := &prv1.DeleteBankTransactionRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.TxnId = idFromPath(r)
	resp, err := s.Orders.DeleteBankTransaction(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// restoreBankTransaction 把归档的那一行放回列表。
func (s *Server) restoreBankTransaction(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.RestoreBankTransaction(r.Context(),
		&prv1.RestoreBankTransactionRequest{TxnId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// updateBankTransaction 改一行流水。理由必填，在请求体里。
//
// PUT 而不是 POST /{id}/edit：这是「把整行替换成新的样子」，PUT 就是这个
// 意思。删除那边用 POST 是因为 DELETE 带 body 不可靠——PUT 带 body 没有
// 这个问题。
func (s *Server) updateBankTransaction(w http.ResponseWriter, r *http.Request) {
	req := &prv1.UpdateBankTransactionRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.TxnId = idFromPath(r)
	resp, err := s.Orders.UpdateBankTransaction(r.Context(), req)
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
