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
	// Metered by where the request came from, not by the address it names.
	//
	// The per-account counter lives in iam, which owns the account; this one
	// answers the question no per-account counter can — somebody trying a
	// thousand different addresses once each leaves every account's counter
	// sitting at one, and nothing ever fires. Metering the source is the only
	// thing that sees the shape of that.
	//
	// It also means a locked-out answer no longer depends on which address was
	// typed, so this route stopped being a way to ask "does this person work
	// here" by watching who gets throttled.
	who := clientAddr(r, s.TrustProxyHeaders)
	if wait, blocked := s.Throttle.Blocked(r.Context(), throttleLogin, who); blocked {
		s.writeTooManyAttempts(w, wait)
		return
	}
	resp, err := s.IAM.Login(r.Context(), req)
	if err != nil {
		// Only a rejected credential counts. When iam is down every login in
		// the company fails, and charging those to the people trying would
		// mean an outage ends with a whole office locked out on top of it —
		// the recovery is worse than the fault. The status code tells the two
		// apart, so there is no reason to guess.
		//
		// iam's own "too many attempts" is ResourceExhausted rather than
		// Unauthenticated precisely so it lands here as not-charged: being
		// told to wait must not spend the budget that waiting restores.
		if isRejectedCredential(err) {
			if wait, spent := s.Throttle.Failed(r.Context(), throttleLogin, who); spent {
				s.writeTooManyAttempts(w, wait)
				return
			}
		}
		s.writeGRPCError(w, err)
		return
	}
	// Cleared on success, so one person signing in resets the shared office
	// budget. That is the right trade at this key: a source with real traffic
	// on it is not the source spraying.
	s.Throttle.Passed(r.Context(), throttleLogin, who)
	// The token leaves in the cookie and only in the cookie. It used to ride
	// in the body too, and the body is exactly what a script that hooked
	// fetch can read — which would put the theft path back one layer up from
	// the localStorage it was just removed from.
	s.setSessionCookies(w, resp.GetAccessToken())
	resp.AccessToken = ""
	s.writeProto(w, resp)
}

// logout clears the browser's cookies. What it guarantees is exactly what
// deleting the token from localStorage guaranteed before — this browser
// forgets the session; the token itself runs out on its own clock. Ending a
// session server-side is /employees/{id}/revoke-sessions, unchanged.
//
// Unauthenticated on purpose: signing out with an expired token must work,
// or the one person who needs the button cannot press it.
func (s *Server) logout(w http.ResponseWriter, _ *http.Request) {
	s.clearSessionCookies(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"success":true}`))
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
		Page:            pageFromQuery(r),
		Keyword:         r.URL.Query().Get("keyword"),
		Status:          r.URL.Query().Get("status"),
		CountryCode:     r.URL.Query().Get("country_code"),
		CustomerType:    r.URL.Query().Get("customer_type"),
		BusinessStatus:  r.URL.Query().Get("business_status"),
		Tag:             r.URL.Query().Get("tag"),
		OwnerEmployeeId: int64FromQuery(r, "owner_employee_id"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listCustomerCountryGroups(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.ListCustomerCountries(r.Context(), &mdv1.ListCustomerCountriesRequest{
		Status: r.URL.Query().Get("status"),
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
	req := &mdv1.DeactivateCustomerRequest{Id: idFromPath(r), Reason: r.URL.Query().Get("reason")}
	if r.ContentLength > 0 && !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Customers.DeactivateCustomer(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// ---------------------------------------------------------------- suppliers

func (s *Server) listSuppliers(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.ListSuppliers(r.Context(), &mdv1.ListSuppliersRequest{
		Page:         pageFromQuery(r),
		Keyword:      r.URL.Query().Get("keyword"),
		Status:       r.URL.Query().Get("status"),
		BusinessType: r.URL.Query().Get("business_type"),
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
	req := &mdv1.ActivateCustomerRequest{Id: idFromPath(r), Reason: r.URL.Query().Get("reason")}
	if r.ContentLength > 0 && !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Customers.ActivateCustomer(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getCustomerDeactivationImpact(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.GetCustomerDeactivationImpact(r.Context(), &mdv1.GetCustomerDeactivationImpactRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateCustomerProfile(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.UpdateCustomerProfileRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Customers.UpdateCustomerProfile(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func addressIDFromPath(r *http.Request) int64 {
	id, _ := strconv.ParseInt(chi.URLParam(r, "addressId"), 10, 64)
	return id
}

func (s *Server) listCustomerAddresses(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.ListCustomerAddresses(r.Context(), &mdv1.ListCustomerAddressesRequest{
		CustomerId: idFromPath(r), Status: r.URL.Query().Get("status"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createCustomerAddress(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.CreateCustomerAddressRequest{CustomerId: idFromPath(r)}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CustomerId = idFromPath(r)
	resp, err := s.Customers.CreateCustomerAddress(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateCustomerAddress(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.UpdateCustomerAddressRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CustomerId = idFromPath(r)
	req.Id = addressIDFromPath(r)
	resp, err := s.Customers.UpdateCustomerAddress(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) deactivateCustomerAddress(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.DeactivateCustomerAddress(r.Context(), &mdv1.DeactivateCustomerAddressRequest{
		CustomerId: idFromPath(r), Id: addressIDFromPath(r),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
