package app

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/outbox"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

const (
	ConditionPrepaymentReceived   = "PREPAYMENT_RECEIVED"
	ConditionLetterCreditReceived = "LETTER_OF_CREDIT_RECEIVED"
	ConditionNoPrepaymentRequired = "NO_PREPAYMENT_REQUIRED"
	ConditionSpecialApproval      = "SPECIAL_APPROVAL"
)

var executionConditionTypes = map[string]bool{
	ConditionPrepaymentReceived: true, ConditionLetterCreditReceived: true,
	ConditionNoPrepaymentRequired: true, ConditionSpecialApproval: true,
}

type ExecutionConditionConfirmation struct {
	Status, ConditionType, ConfirmedAt, ConfirmedByName, Note string
}

// ConfirmContractExecutionCondition is Finance's single release gate. The
// confirmation and ContractEffective outbox row commit together, so downstream
// tasks can neither appear before the decision nor be lost after it.
func (s *Service) ConfirmContractExecutionCondition(ctx context.Context, tenantID, contractID int64, conditionType, note string, op Operator) (ExecutionConditionConfirmation, error) {
	conditionType = strings.ToUpper(strings.TrimSpace(conditionType))
	note = strings.TrimSpace(note)
	if !executionConditionTypes[conditionType] {
		return ExecutionConditionConfirmation{}, apierr.Invalid("EX_EXECUTION_CONDITION_INVALID", "请选择有效的执行条件")
	}
	view, err := s.GetContract(ctx, tenantID, contractID, 0)
	if err != nil {
		return ExecutionConditionConfirmation{}, err
	}
	if view.Contract.Status != "EXECUTING" {
		return ExecutionConditionConfirmation{}, apierr.Conflict("EX_CONTRACT_NOT_EXECUTING", "合同尚未开始执行，不能确认执行条件")
	}
	// Finance-only opening rows and historical takeover contracts belong in
	// receivables, but must not fabricate a new purchasing/logistics run.
	releaseDownstream := view.Contract.CustomerID != 0 && view.Contract.EntrySource != "EXISTING_CONTRACT"
	var payload []byte
	if releaseDownstream {
		event, eventErr := s.hydrateEffectiveEvent(ctx, tenantID, view)
		if eventErr != nil {
			return ExecutionConditionConfirmation{}, eventErr
		}
		payload, err = json.Marshal(event)
		if err != nil {
			return ExecutionConditionConfirmation{}, err
		}
	}

	result := ExecutionConditionConfirmation{}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var status, existingType, confirmedAt, confirmedBy, existingNote string
		var currentVersionID int64
		err := tx.QueryRow(ctx, `SELECT status,current_version_id,execution_condition_type,
			coalesce(condition_confirmed_at::text,''),condition_confirmed_by_name,condition_confirmation_note
			FROM contracts WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, contractID).
			Scan(&status, &currentVersionID, &existingType, &confirmedAt, &confirmedBy, &existingNote)
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("EX_CONTRACT_NOT_FOUND", "合同不存在")
		}
		if err != nil {
			return err
		}
		if confirmedAt != "" {
			result = ExecutionConditionConfirmation{Status: "READY", ConditionType: existingType, ConfirmedAt: confirmedAt, ConfirmedByName: confirmedBy, Note: existingNote}
			return nil
		}
		if status != "EXECUTING" || currentVersionID != view.Version.ID {
			return apierr.Conflict("EX_CONTRACT_RELEASE_CHANGED", "合同状态或版本已变化，请刷新后重试")
		}
		err = tx.QueryRow(ctx, `UPDATE contracts SET execution_condition_type=$3,
			condition_confirmed_at=now(),condition_confirmation_note=$4,
			condition_confirmed_by=$5,condition_confirmed_by_name=$6,updated_at=now(),updated_by=$5
			WHERE tenant_id=$1 AND id=$2
			RETURNING condition_confirmed_at::text`, tenantID, contractID, conditionType, note, op.ID, op.Name).
			Scan(&result.ConfirmedAt)
		if err != nil {
			return err
		}
		result.Status, result.ConditionType, result.ConfirmedByName, result.Note = "READY", conditionType, op.Name, note
		if !releaseDownstream {
			return nil
		}
		return outbox.Append(ctx, tx, outbox.Event{TenantID: tenantID, AggregateType: "contract",
			AggregateID: strconv.FormatInt(contractID, 10), EventType: "ContractEffective", Payload: payload})
	})
	return result, err
}
