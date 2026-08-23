package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
)

// 信用评级（E3）。
//
// 客户和供应商各有自己的路由，但落到同一个服务：权限跟着各自的主数据走
// ——能看客户才看得见客户的评级，能改供应商才评得了供应商。共用一条路由
// 加一个 party_type 参数会让权限判断变成读参数，那是把围栏交给调用方。

func (s *Server) listCustomerCreditRatings(w http.ResponseWriter, r *http.Request) {
	s.listCreditRatings(w, r, "CUSTOMER")
}

func (s *Server) listSupplierCreditRatings(w http.ResponseWriter, r *http.Request) {
	s.listCreditRatings(w, r, "SUPPLIER")
}

func (s *Server) rateCustomerCredit(w http.ResponseWriter, r *http.Request) {
	s.rateCredit(w, r, "CUSTOMER")
}

func (s *Server) rateSupplierCredit(w http.ResponseWriter, r *http.Request) {
	s.rateCredit(w, r, "SUPPLIER")
}

func (s *Server) listCreditRatings(w http.ResponseWriter, r *http.Request, partyType string) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeError(w, http.StatusBadRequest, "MD_CREDIT_PARTY_REQUIRED", "缺少对象标识")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	resp, err := s.CreditRatings.ListCreditRatings(r.Context(), &mdv1.ListCreditRatingsRequest{
		PartyType: partyType, PartyId: id, Limit: int32(limit),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) rateCredit(w http.ResponseWriter, r *http.Request, partyType string) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeError(w, http.StatusBadRequest, "MD_CREDIT_PARTY_REQUIRED", "缺少对象标识")
		return
	}
	var body mdv1.RateCreditRequest
	if !s.decodeBody(w, r, &body) {
		return
	}
	// 路由决定评的是谁：请求体里传来的 party_type / party_id 一律不作数，
	// 否则「客户写权限」就能拿去改供应商的评级。
	body.PartyType, body.PartyId = partyType, id
	resp, err := s.CreditRatings.RateCredit(r.Context(), &body)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
