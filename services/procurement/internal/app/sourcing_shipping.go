package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

type SourcingShippingCollaboration struct {
	Request      store.SourcingShippingRequest
	Options      []SourcingShippingOption
	CargoItems   []store.ListSourcingShippingCargoItemsRow
	Participants []store.ListSourcingShippingParticipantsRow
	Plans        []ShippingPlanView
}

type SourcingShippingOption struct {
	Option store.ListSourcingShippingOptionsRow
	Lines  []store.ListSourcingShippingOptionLinesRow
}

type NewSourcingShippingOption struct {
	CaseID                                         int64
	CarrierForwarder, ServiceOptionName            string
	PortOfLoading, PortOfDischarge                 string
	QuotedAt, EstimatedDeparture, EstimatedArrival string
	ValidUntil, Note                               string
	Lines                                          []NewSourcingShippingOptionLine
}

type NewSourcingShippingOptionLine struct {
	SourcingLineID                                int64
	Currency, ChargeBasis, UnitRate, TotalFreight string
	Note                                          string
}

func (s *Service) GetSourcingShippingCollaboration(ctx context.Context, tenantID, caseID int64) (SourcingShippingCollaboration, error) {
	request, err := s.q.GetSourcingShippingRequest(ctx, store.GetSourcingShippingRequestParams{TenantID: tenantID, CaseID: caseID})
	if errors.Is(err, pgx.ErrNoRows) {
		return SourcingShippingCollaboration{}, apierr.NotFound("SC_SHIPPING_NOT_FOUND", "该询价案件尚未建立船运任务")
	}
	if err != nil {
		return SourcingShippingCollaboration{}, err
	}
	optionRows, err := s.q.ListSourcingShippingOptions(ctx, store.ListSourcingShippingOptionsParams{TenantID: tenantID, RequestID: request.ID})
	if err != nil {
		return SourcingShippingCollaboration{}, err
	}
	options := make([]SourcingShippingOption, 0, len(optionRows))
	for _, option := range optionRows {
		lines, lineErr := s.q.ListSourcingShippingOptionLines(ctx, store.ListSourcingShippingOptionLinesParams{TenantID: tenantID, OptionID: option.ID})
		if lineErr != nil {
			return SourcingShippingCollaboration{}, lineErr
		}
		options = append(options, SourcingShippingOption{Option: option, Lines: lines})
	}
	cargoItems, err := s.q.ListSourcingShippingCargoItems(ctx, store.ListSourcingShippingCargoItemsParams{TenantID: tenantID, CaseID: caseID})
	if err != nil {
		return SourcingShippingCollaboration{}, err
	}
	participants, err := s.q.ListSourcingShippingParticipants(ctx, store.ListSourcingShippingParticipantsParams{TenantID: tenantID, RequestID: request.ID})
	if err != nil {
		return SourcingShippingCollaboration{}, err
	}
	plans, err := s.ListSourcingShippingPlans(ctx, tenantID, caseID)
	return SourcingShippingCollaboration{Request: request, Options: options, CargoItems: cargoItems, Participants: participants, Plans: plans}, err
}

func (s *Service) ListSourcingShippingTasks(ctx context.Context, tenantID int64, status, keyword string, page, size int32) ([]store.ListSourcingShippingRequestsRow, int64, int32, int32, error) {
	page, size = normalizePage(page, size)
	rows, err := s.q.ListSourcingShippingRequests(ctx, store.ListSourcingShippingRequestsParams{
		TenantID: tenantID, Status: strings.ToUpper(strings.TrimSpace(status)), Keyword: strings.TrimSpace(keyword),
		RowOffset: (page - 1) * size, RowLimit: size,
	})
	if err != nil {
		return nil, 0, page, size, err
	}
	var total int64
	if len(rows) > 0 {
		total = rows[0].Total
	}
	return rows, total, page, size, nil
}

func (s *Service) StartSourcingShippingTask(ctx context.Context, tenantID, caseID int64, op Operator) (SourcingShippingCollaboration, error) {
	if _, err := s.JoinSourcingShippingTask(ctx, tenantID, caseID, op); err != nil {
		return SourcingShippingCollaboration{}, err
	}
	_ = s.q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: caseID,
		Section: "SHIPPING", Action: "SHIPPING_STARTED", Summary: "船运开始处理售前询价任务",
		BeforeJson: []byte(`{}`), AfterJson: []byte(`{"status":"IN_PROGRESS"}`), OperatorID: op.ID, OperatorName: op.Name})
	s.nudge(ctx, tenantID)
	return s.GetSourcingShippingCollaboration(ctx, tenantID, caseID)
}

func validOptionalDate(value string) bool {
	if strings.TrimSpace(value) == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

func (s *Service) AddSourcingShippingOption(ctx context.Context, tenantID int64, in NewSourcingShippingOption, op Operator) (SourcingShippingCollaboration, error) {
	in.CarrierForwarder = strings.TrimSpace(in.CarrierForwarder)
	in.ServiceOptionName = strings.TrimSpace(in.ServiceOptionName)
	in.Note = strings.TrimSpace(in.Note)
	if in.CaseID == 0 || in.CarrierForwarder == "" {
		return SourcingShippingCollaboration{}, apierr.Invalid("SC_SHIPPING_COMPANY_REQUIRED", "请填写船运公司或货代")
	}
	if !validOptionalDate(in.QuotedAt) || !validOptionalDate(in.EstimatedDeparture) || !validOptionalDate(in.EstimatedArrival) || !validOptionalDate(in.ValidUntil) {
		return SourcingShippingCollaboration{}, apierr.Invalid("SC_SHIPPING_TERMS_INVALID", "报价日期、预计开船日、预计到港日或有效期无效")
	}
	if in.EstimatedDeparture != "" && in.EstimatedArrival != "" && in.EstimatedArrival < in.EstimatedDeparture {
		return SourcingShippingCollaboration{}, apierr.Invalid("SC_SHIPPING_ARRIVAL_BEFORE_DEPARTURE", "预计到港日不能早于预计开船日")
	}
	if len(in.Lines) == 0 {
		return SourcingShippingCollaboration{}, apierr.Invalid("SC_SHIPPING_LINES_REQUIRED", "请至少为一种货物填写运费")
	}
	request, err := s.q.GetSourcingShippingRequest(ctx, store.GetSourcingShippingRequestParams{TenantID: tenantID, CaseID: in.CaseID})
	if errors.Is(err, pgx.ErrNoRows) {
		return SourcingShippingCollaboration{}, apierr.NotFound("SC_SHIPPING_NOT_FOUND", "该询价案件尚未建立船运任务")
	}
	if err != nil {
		return SourcingShippingCollaboration{}, err
	}
	if err := s.requireShippingParticipant(ctx, tenantID, request.ID, op.ID); err != nil {
		return SourcingShippingCollaboration{}, err
	}
	cargoItems, err := s.q.ListSourcingShippingCargoItems(ctx, store.ListSourcingShippingCargoItemsParams{TenantID: tenantID, CaseID: in.CaseID})
	if err != nil {
		return SourcingShippingCollaboration{}, err
	}
	cargoByID := make(map[int64]store.ListSourcingShippingCargoItemsRow, len(cargoItems))
	for _, item := range cargoItems {
		cargoByID[item.SourcingLineID] = item
	}
	type validatedLine struct {
		input NewSourcingShippingOptionLine
		cargo store.ListSourcingShippingCargoItemsRow
		rate  decimal.Decimal
	}
	validated := make([]validatedLine, 0, len(in.Lines))
	seen := map[int64]bool{}
	for _, line := range in.Lines {
		cargo, ok := cargoByID[line.SourcingLineID]
		line.Currency, line.ChargeBasis, line.Note = strings.ToUpper(strings.TrimSpace(line.Currency)), strings.ToUpper(strings.TrimSpace(line.ChargeBasis)), strings.TrimSpace(line.Note)
		rate, rateErr := decimal.NewFromString(strings.TrimSpace(line.UnitRate))
		total, totalErr := decimal.NewFromString(strings.TrimSpace(line.TotalFreight))
		if !ok || seen[line.SourcingLineID] {
			return SourcingShippingCollaboration{}, apierr.Invalid("SC_SHIPPING_LINE_INVALID", "货物报价行不属于当前询价案件或重复")
		}
		if len(line.Currency) != 3 || !validShippingChargeBasis(line.ChargeBasis) || rateErr != nil || rate.IsNegative() || totalErr != nil || total.IsNegative() {
			return SourcingShippingCollaboration{}, apierr.Invalid("SC_SHIPPING_LINE_PRICE_INVALID", "请为每种选中货物填写有效的币种、计费方式、运费单价和总运费")
		}
		seen[line.SourcingLineID] = true
		validated = append(validated, validatedLine{input: line, cargo: cargo, rate: rate})
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		optionID, createErr := q.CreateSourcingShippingOption(ctx, store.CreateSourcingShippingOptionParams{
			TenantID: tenantID, RequestID: request.ID, CarrierForwarder: in.CarrierForwarder,
			PortOfLoading: strings.TrimSpace(in.PortOfLoading), PortOfDischarge: strings.TrimSpace(in.PortOfDischarge),
			QuotedAt: in.QuotedAt, EstimatedDeparture: in.EstimatedDeparture, EstimatedArrival: in.EstimatedArrival,
			ValidUntil: in.ValidUntil, Note: in.Note, CreatedBy: op.ID, CreatedByName: op.Name, ServiceOptionName: in.ServiceOptionName,
		})
		if createErr != nil {
			return createErr
		}
		for _, line := range validated {
			specification := strings.Join(compactStrings([]string{line.cargo.MaterialStandard, line.cargo.Grade, line.cargo.Thickness, line.cargo.Width, line.cargo.LengthOrForm, line.cargo.SurfaceRequirement, line.cargo.Packaging}), " · ")
			if lineErr := q.CreateSourcingShippingOptionLine(ctx, store.CreateSourcingShippingOptionLineParams{TenantID: tenantID, OptionID: optionID, SourcingLineID: line.input.SourcingLineID, LineNo: line.cargo.LineNo, ProductSnapshot: line.cargo.Product, SpecificationSnapshot: specification, Quantity: line.cargo.Quantity, QuantityUnit: line.cargo.QuantityUnit, Currency: line.input.Currency, ChargeBasis: line.input.ChargeBasis, UnitRate: line.rate.String(), TotalFreight: strings.TrimSpace(line.input.TotalFreight), Note: line.input.Note}); lineErr != nil {
				return lineErr
			}
		}
		if markErr := q.MarkSourcingShippingLinked(ctx, store.MarkSourcingShippingLinkedParams{TenantID: tenantID, ID: request.ID}); markErr != nil {
			return markErr
		}
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: in.CaseID,
			Section: "SHIPPING", Action: "SHIPPING_OPTION_SUBMITTED", EntityID: optionID, Summary: "船运提交多货物售前报价方案",
			BeforeJson: []byte(`{}`), AfterJson: []byte(`{"status":"MANAGER_REVIEW"}`), Reason: in.Note, OperatorID: op.ID, OperatorName: op.Name})
	})
	if err != nil {
		return SourcingShippingCollaboration{}, err
	}
	s.nudge(ctx, tenantID)
	return s.GetSourcingShippingCollaboration(ctx, tenantID, in.CaseID)
}

func validShippingChargeBasis(value string) bool {
	switch value {
	case "PER_TON", "PER_CONTAINER", "PER_PIECE", "PER_SHIPMENT", "FIXED":
		return true
	}
	return false
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}
