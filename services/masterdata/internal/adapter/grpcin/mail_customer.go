package grpcin

import (
	"context"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/masterdata/internal/app"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

func (h *Handler) MatchMailCustomer(ctx context.Context, r *mdv1.MatchMailCustomerRequest) (*mdv1.MatchMailCustomerResponse, error) {
	v, err := h.svc.MatchMailCustomer(ctx, grpcx.TenantID(ctx), r.CompanyName, r.Email)
	if err != nil {
		return nil, err
	}
	convert := func(in []app.MailCustomerMatch) []*mdv1.MailCustomerMatch {
		out := make([]*mdv1.MailCustomerMatch, 0, len(in))
		for _, c := range in {
			m := &mdv1.MailCustomerMatch{Id: c.ID, Code: c.Code, Name: c.Name, Status: c.Status, ExactName: c.ExactName}
			for _, ct := range c.Contacts {
				m.Contacts = append(m.Contacts, contactToProto(ct))
			}
			out = append(out, m)
		}
		return out
	}
	return &mdv1.MatchMailCustomerResponse{Customers: convert(v.Customers), Suppliers: convert(v.Suppliers)}, nil
}
func mailLinkProto(v app.MailCustomerLink) *mdv1.GetMailCustomerLinkResponse {
	if v.Customer.ID == 0 {
		return &mdv1.GetMailCustomerLinkResponse{}
	}
	return &mdv1.GetMailCustomerLinkResponse{Customer: customerToProto(v.Customer, []store.CustomerContact{v.Contact}, nil), Contact: contactToProto(v.Contact), Email: v.Email}
}
func (h *Handler) GetMailCustomerLink(ctx context.Context, r *mdv1.GetMailCustomerLinkRequest) (*mdv1.GetMailCustomerLinkResponse, error) {
	v, err := h.svc.GetMailCustomerLink(ctx, grpcx.TenantID(ctx), r.InboundId)
	if err != nil {
		return nil, err
	}
	return mailLinkProto(v), nil
}
func (h *Handler) SaveMailCustomer(ctx context.Context, r *mdv1.SaveMailCustomerRequest) (*mdv1.SaveMailCustomerResponse, error) {
	v, err := h.svc.SaveMailCustomer(ctx, grpcx.TenantID(ctx), app.SaveMailCustomerInput{InboundID: r.InboundId, CustomerID: r.CustomerId, ContactID: r.ContactId, Action: r.Action, CompanyName: r.CompanyName, ContactName: r.ContactName, Email: r.Email, Phone: r.Phone, Website: r.Website, Address: r.Address, Remark: r.Remark, ConfirmSameName: r.ConfirmSameName, ConfirmSupplier: r.ConfirmSupplier, OperatorID: operatorID(ctx), OperatorName: operatorName(ctx)})
	if err != nil {
		return nil, err
	}
	link := mailLinkProto(v)
	return &mdv1.SaveMailCustomerResponse{Customer: link.Customer, Contact: link.Contact, Email: link.Email}, nil
}
