package grpcin

import (
	"context"

	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/masterdata/internal/app"
)

// 信用评级（E3）。客户和供应商共用一套接口——问的问题不同，但「谁在什么
// 时候依据什么评了什么」是同一个形状。
func (h *Handler) RateCredit(ctx context.Context, req *mdv1.RateCreditRequest) (*mdv1.RateCreditResponse, error) {
	rating, err := h.svc.RateCredit(ctx, grpcx.TenantID(ctx), app.CreditRatingInput{
		PartyType: req.GetPartyType(), PartyID: req.GetPartyId(),
		Grade: req.GetGrade(), Basis: req.GetBasis(), Evidence: req.GetEvidence(),
		OperatorID: operatorID(ctx), OperatorName: operatorName(ctx),
	})
	if err != nil {
		return nil, err
	}
	return &mdv1.RateCreditResponse{Rating: creditRatingToProto(rating)}, nil
}

func (h *Handler) ListCreditRatings(ctx context.Context, req *mdv1.ListCreditRatingsRequest) (*mdv1.ListCreditRatingsResponse, error) {
	rows, err := h.svc.ListCreditRatings(ctx, grpcx.TenantID(ctx),
		req.GetPartyType(), req.GetPartyId(), req.GetLimit())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.CreditRating, 0, len(rows))
	for _, r := range rows {
		out = append(out, creditRatingToProto(r))
	}
	return &mdv1.ListCreditRatingsResponse{Ratings: out}, nil
}

func creditRatingToProto(r app.CreditRating) *mdv1.CreditRating {
	return &mdv1.CreditRating{
		Id: r.ID, Grade: r.Grade, PreviousGrade: r.PreviousGrade, Basis: r.Basis,
		Evidence: r.Evidence, RatedBy: r.RatedBy, RatedByName: r.RatedByName,
		RatedAt: r.RatedAt,
	}
}
