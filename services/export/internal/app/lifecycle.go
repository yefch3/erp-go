package app

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/outbox"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

// transitions is the quotation state machine, written out rather than
// scattered through handlers: a status is only reachable from the states
// listed here.
var transitions = map[string][]string{
	"DRAFT":     {"SENT", "CANCELLED"},
	"SENT":      {"ACCEPTED", "REJECTED", "CANCELLED", "EXPIRED"},
	"ACCEPTED":  {},
	"REJECTED":  {},
	"EXPIRED":   {},
	"CANCELLED": {},
}

func allowed(from, to string) bool {
	for _, s := range transitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

// setStatus moves a quotation, refusing anything the state machine forbids.
func (s *Service) setStatus(ctx context.Context, tenantID, id, operatorID int64, to string) (string, error) {
	current, _, err := s.GetQuotation(ctx, tenantID, id)
	if err != nil {
		return "", err
	}
	if err := s.mayWrite(ctx, Operator{ID: operatorID}, current.SalesEmployeeID,
		"EX_QUOTE_NOT_OWNER", "只能操作自己负责的报价单"); err != nil {
		return "", err
	}
	if current.Status == to {
		// Repeating a transition is a double click, not an error worth
		// failing the user's action over.
		return current.Status, nil
	}
	if !allowed(current.Status, to) {
		return "", apierr.Conflict("EX_STATUS_TRANSITION", "当前状态不允许该操作").
			WithMeta("from", current.Status).WithMeta("to", to)
	}
	status := ""
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		var setErr error
		status, setErr = q.SetQuotationStatus(ctx, store.SetQuotationStatusParams{
			TenantID: tenantID, ID: id, NewStatus: to, UpdatedBy: operatorID,
		})
		if setErr != nil {
			return setErr
		}
		if to != "ACCEPTED" {
			return nil
		}
		// 客户接受报价是采购“待下单”的唯一入口。事件与状态在同一事务提交，
		// 避免状态已接受但采购任务丢失，或重复点击生成两次任务。
		payload, marshalErr := json.Marshal(map[string]any{
			"quotation_id":     id,
			"quotation_no":     current.QuoteNo,
			"customer_id":      current.CustomerID,
			"customer_name":    current.CustomerName,
			"cost_scenario_id": current.SourceCostScenarioID,
			"sourcing_case_id": current.SourceSourcingCaseID,
		})
		if marshalErr != nil {
			return marshalErr
		}
		return outbox.Append(ctx, tx, outbox.Event{
			TenantID: tenantID, AggregateType: "quotation",
			AggregateID: strconv.FormatInt(id, 10), EventType: "QuotationAccepted",
			Payload: payload,
		})
	})
	return status, err
}

// Send marks a quotation as issued to the customer. From here on the fx
// snapshot and the prices are a promise, so edits are refused.
func (s *Service) Send(ctx context.Context, tenantID, id, operatorID int64) (string, error) {
	return s.setStatus(ctx, tenantID, id, operatorID, "SENT")
}

// Respond records the customer's answer.
func (s *Service) Respond(ctx context.Context, tenantID, id, operatorID int64, status string) (string, error) {
	if status != "ACCEPTED" && status != "REJECTED" {
		return "", apierr.Invalid("EX_RESPONSE_INVALID", "客户答复只能是 ACCEPTED 或 REJECTED")
	}
	return s.setStatus(ctx, tenantID, id, operatorID, status)
}

func (s *Service) Cancel(ctx context.Context, tenantID, id, operatorID int64) (string, error) {
	return s.setStatus(ctx, tenantID, id, operatorID, "CANCELLED")
}
