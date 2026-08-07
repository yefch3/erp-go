package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
)

// ---------------------------------------------------------------- auth

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	req := &iamv1.LoginRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	// Metered by the address the caller typed, which is the only identity
	// there is before anybody has proved anything. That it may name no account
	// at all is fine and in fact required: counting attempts on addresses that
	// do not exist is what keeps a locked-out response from being a way to ask
	// "does this person work here" — and company addresses are guessable, so
	// that matters more here than it did under usernames.
	name := req.GetEmail()
	if wait, blocked := s.Throttle.Blocked(r.Context(), throttleLogin, name); blocked {
		s.writeTooManyAttempts(w, wait)
		return
	}
	resp, err := s.IAM.Login(r.Context(), req)
	if err != nil {
		// Only a rejected credential counts. When iam is down every login in
		// the company fails, and charging those to the people trying would
		// mean an outage ends with everybody locked out for a quarter of an
		// hour on top of it — the recovery is worse than the fault. The
		// status code tells the two apart, so there is no reason to guess.
		if isRejectedCredential(err) {
			if wait, spent := s.Throttle.Failed(r.Context(), throttleLogin, name); spent {
				s.writeTooManyAttempts(w, wait)
				return
			}
		}
		s.writeGRPCError(w, err)
		return
	}
	s.Throttle.Passed(r.Context(), throttleLogin, name)
	s.writeProto(w, resp)
}

// writeTooManyAttempts is the one answer given to a caller who has run out of
// attempts, whichever route they ran out on.
//
// Retry-After so a well-behaved client can wait rather than poll, and a
// message that says only "later" — naming the account, the count or the reason
// would turn the lockout itself into a way to learn things.
func (s *Server) writeTooManyAttempts(w http.ResponseWriter, wait time.Duration) {
	secs := int(wait.Seconds())
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	s.writeError(w, http.StatusTooManyRequests, "GATEWAY_TOO_MANY_ATTEMPTS",
		"尝试次数过多，请稍后再试")
}

// ---------------------------------------------------------------- customers

func pageFromQuery(r *http.Request) *commonv1.PageRequest {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	return &commonv1.PageRequest{Page: int32(page), PageSize: int32(size)}
}

func idFromPath(r *http.Request) int64 {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id
}

func (s *Server) listCustomers(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.ListCustomers(r.Context(), &mdv1.ListCustomersRequest{
		Page:    pageFromQuery(r),
		Keyword: r.URL.Query().Get("keyword"),
		Status:  r.URL.Query().Get("status"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createCustomer(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.CreateCustomerRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Customers.CreateCustomer(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getCustomer(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.GetCustomer(r.Context(), &mdv1.GetCustomerRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateCustomer(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.UpdateCustomerRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Customers.UpdateCustomer(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) deactivateCustomer(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.DeactivateCustomer(r.Context(), &mdv1.DeactivateCustomerRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// ---------------------------------------------------------------- suppliers

func (s *Server) listSuppliers(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.ListSuppliers(r.Context(), &mdv1.ListSuppliersRequest{
		Page:    pageFromQuery(r),
		Keyword: r.URL.Query().Get("keyword"),
		Status:  r.URL.Query().Get("status"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createSupplier(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.CreateSupplierRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Suppliers.CreateSupplier(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// ---------------------------------------------------------------- options / numbering

func (s *Server) listOptions(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Options.ListOptions(r.Context(), &mdv1.ListOptionsRequest{
		Category: r.URL.Query().Get("category"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) nextNumber(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.NextNumberRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Numbering.NextNumber(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) activateCustomer(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.ActivateCustomer(r.Context(), &mdv1.ActivateCustomerRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
