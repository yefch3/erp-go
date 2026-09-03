package httpapi

import (
	"net/http"
	"strconv"

	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
)

func (s *Server) listContracts(w http.ResponseWriter, r *http.Request) {
	customerID, _ := strconv.ParseInt(r.URL.Query().Get("customer_id"), 10, 64)
	resp, err := s.Contracts.ListContracts(r.Context(), &exv1.ListContractsRequest{
		Page:       pageFromQuery(r),
		Keyword:    r.URL.Query().Get("keyword"),
		CustomerId: customerID,
		Status:     r.URL.Query().Get("status"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getContract(w http.ResponseWriter, r *http.Request) {
	versionID, _ := strconv.ParseInt(r.URL.Query().Get("version_id"), 10, 64)
	resp, err := s.Contracts.GetContract(r.Context(), &exv1.GetContractRequest{
		Id: idFromPath(r), VersionId: versionID,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createContract(w http.ResponseWriter, r *http.Request) {
	req := &exv1.CreateContractFromQuotationRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	quotation, err := s.Quotations.GetQuotation(r.Context(), &exv1.GetQuotationRequest{Id: req.GetQuotationId()})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if _, err := s.resolveActiveCustomer(r.Context(), quotation.GetQuotation().GetCustomerId()); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	resp, err := s.Contracts.CreateContractFromQuotation(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// createDirectContract writes up a contract with no quotation behind it. A
// separate route rather than a branch inside createContract: the two carry
// different bodies, and one endpoint that silently means two things is the
// kind of thing that gets called wrongly at three in the morning.
func (s *Server) createDirectContract(w http.ResponseWriter, r *http.Request) {
	req := &exv1.CreateContractRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	if _, err := s.resolveActiveCustomer(r.Context(), req.GetCustomerId()); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	resp, err := s.Contracts.CreateContract(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) importExistingContract(w http.ResponseWriter, r *http.Request) {
	req := &exv1.ImportExistingContractRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	if _, err := s.resolveActiveCustomer(r.Context(), req.GetCustomerId()); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	resp, err := s.Contracts.ImportExistingContract(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateContract(w http.ResponseWriter, r *http.Request) {
	req := &exv1.UpdateContractRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Contracts.UpdateContract(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) submitContract(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Contracts.SubmitContract(r.Context(), &exv1.SubmitContractRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) changeContract(w http.ResponseWriter, r *http.Request) {
	req := &exv1.ChangeContractRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Contracts.ChangeContract(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) signContract(w http.ResponseWriter, r *http.Request) {
	req := &exv1.SignContractRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Contracts.SignContract(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) cancelContract(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Contracts.CancelContract(r.Context(), &exv1.CancelContractRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listContractFiles(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Contracts.ListContractFiles(r.Context(),
		&exv1.ListContractFilesRequest{ContractId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) presignContractFile(w http.ResponseWriter, r *http.Request) {
	req := &exv1.PresignContractFileRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.ContractId = idFromPath(r)
	resp, err := s.Contracts.PresignContractFile(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) registerContractFile(w http.ResponseWriter, r *http.Request) {
	req := &exv1.RegisterContractFileRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.ContractId = idFromPath(r)
	resp, err := s.Contracts.RegisterContractFile(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) removeContractFile(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Contracts.RemoveContractFile(r.Context(),
		&exv1.RemoveContractFileRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// ---------------------------------------------------------------- ownership

func (s *Server) transferOwnership(w http.ResponseWriter, r *http.Request) {
	req := &exv1.TransferOwnershipRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Contracts.TransferOwnership(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listOwnershipTransfers(w http.ResponseWriter, r *http.Request) {
	bizID, _ := strconv.ParseInt(r.URL.Query().Get("biz_id"), 10, 64)
	resp, err := s.Contracts.ListOwnershipTransfers(r.Context(), &exv1.ListOwnershipTransfersRequest{
		BizType: r.URL.Query().Get("biz_type"), BizId: bizID,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
