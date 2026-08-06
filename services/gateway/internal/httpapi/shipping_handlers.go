package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
)

// resolveShippingMasterdata makes the selected ids authoritative while the
// copied names remain immutable snapshots on the shipping record.
func (s *Server) resolveShippingMasterdata(r *http.Request, in *shippingv1.ScheduleInput) error {
	if in == nil {
		return nil
	}
	if in.GetCustomerId() > 0 {
		resp, err := s.Customers.GetCustomer(r.Context(), &mdv1.GetCustomerRequest{Id: in.GetCustomerId()})
		if err != nil {
			return err
		}
		in.CustomerName = resp.GetCustomer().GetName()
	}
	if in.GetCarrierId() > 0 {
		resp, err := s.Suppliers.GetSupplier(r.Context(), &mdv1.GetSupplierRequest{Id: in.GetCarrierId()})
		if err != nil {
			return err
		}
		in.CarrierForwarder = resp.GetSupplier().GetName()
	}
	return nil
}

func (s *Server) getShippingStatus(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipping.GetModuleStatus(r.Context(), &shippingv1.GetModuleStatusRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listShippingSchedules(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	resp, err := s.Shipping.ListSchedules(r.Context(), &shippingv1.ListSchedulesRequest{
		Keyword: q.Get("keyword"), Status: q.Get("status"), PortOfLoading: q.Get("port_of_loading"), PortOfDischarge: q.Get("port_of_discharge"),
		EtdFrom: q.Get("etd_from"), EtdTo: q.Get("etd_to"), EtaFrom: q.Get("eta_from"), EtaTo: q.Get("eta_to"), Page: pageFromQuery(r),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getShippingSchedule(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipping.GetSchedule(r.Context(), &shippingv1.GetScheduleRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createShippingSchedule(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.CreateScheduleRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	if err := s.resolveShippingMasterdata(r, req.GetSchedule()); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	resp, err := s.Shipping.CreateSchedule(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateShippingSchedule(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.UpdateScheduleRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	if err := s.resolveShippingMasterdata(r, req.GetSchedule()); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Shipping.UpdateSchedule(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateShippingScheduleStatus(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.UpdateScheduleStatusRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Shipping.UpdateScheduleStatus(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) cancelShippingSchedule(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.CancelScheduleRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Shipping.CancelSchedule(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getShippingStatistics(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipping.GetShippingStatistics(r.Context(), &shippingv1.GetShippingStatisticsRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) addShippingRouteNode(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.AddRouteNodeRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Shipping.AddRouteNode(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) removeShippingRouteNode(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.RemoveRouteNodeRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	req.RouteNodeId, _ = strconv.ParseInt(chi.URLParam(r, "nodeID"), 10, 64)
	resp, err := s.Shipping.RemoveRouteNode(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) reorderShippingRoute(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.ReorderRouteRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Shipping.ReorderRoute(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateShippingProgress(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.UpdateProgressRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Shipping.UpdateProgress(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func shippingDocumentID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(chi.URLParam(r, "documentID"), 10, 64)
	return id
}

func (s *Server) listShippingDocuments(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipping.ListDocuments(r.Context(), &shippingv1.ListDocumentsRequest{ScheduleId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) presignShippingDocument(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.PresignDocumentUploadRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.ScheduleId = idFromPath(r)
	resp, err := s.Shipping.PresignDocumentUpload(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) registerShippingDocument(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.RegisterDocumentRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.ScheduleId = idFromPath(r)
	resp, err := s.Shipping.RegisterDocument(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) shippingDocumentAccess(w http.ResponseWriter, r *http.Request, mode string) {
	resp, err := s.Shipping.GetDocumentAccess(r.Context(), &shippingv1.GetDocumentAccessRequest{
		ScheduleId: idFromPath(r), DocumentId: shippingDocumentID(r), Mode: mode,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) previewShippingDocument(w http.ResponseWriter, r *http.Request) {
	s.shippingDocumentAccess(w, r, "PREVIEW")
}

func (s *Server) downloadShippingDocument(w http.ResponseWriter, r *http.Request) {
	s.shippingDocumentAccess(w, r, "DOWNLOAD")
}

func (s *Server) invalidateShippingDocument(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.InvalidateDocumentRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.ScheduleId = idFromPath(r)
	req.DocumentId = shippingDocumentID(r)
	resp, err := s.Shipping.InvalidateDocument(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
