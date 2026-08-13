package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.ListOrders(r.Context(), &prv1.ListOrdersRequest{
		Page:   pageFromQuery(r),
		Status: r.URL.Query().Get("status"), Keyword: r.URL.Query().Get("keyword"),
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
