package app

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"strings"
	"unicode/utf8"
)

func linkManualContract(ctx context.Context, tx pgx.Tx, tenant, id, contractID int64, number string, op Operator) error {
	if tenant <= 0 || op.ID <= 0 || contractID <= 0 || strings.TrimSpace(number) == "" || utf8.RuneCountInString(number) > 50 {
		return apierr.Invalid("SHIPPING_CONTRACT_LINK_INVALID", "请选择有效的销售合同")
	}
	var oldID int64
	var oldNo string
	err := tx.QueryRow(ctx, `SELECT m.linked_contract_id,h.contract_no FROM manual_shipping_orders m JOIN contract_shipping_handoffs h ON h.id=m.handoff_id AND h.tenant_id=m.tenant_id WHERE m.tenant_id=$1 AND m.handoff_id=$2 FOR UPDATE OF h,m`, tenant, id).Scan(&oldID, &oldNo)
	if err == pgx.ErrNoRows {
		return apierr.NotFound("MANUAL_ORDER_NOT_FOUND", "补录物流实单不存在")
	}
	if err != nil {
		return err
	}
	if oldID > 0 && oldID != contractID {
		return apierr.Conflict("SHIPPING_CONTRACT_ALREADY_LINKED", "该物流单已关联合同，不能直接更换为另一份合同")
	}
	_, err = tx.Exec(ctx, `UPDATE manual_shipping_orders SET linked_contract_id=$3,original_contract_no=CASE WHEN linked_contract_id=0 THEN $4 ELSE original_contract_no END,linked_by=CASE WHEN linked_contract_id=0 THEN $5 ELSE linked_by END,linked_at=coalesce(linked_at,now()) WHERE tenant_id=$1 AND handoff_id=$2`, tenant, id, contractID, oldNo, op.ID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE contract_shipping_handoffs SET contract_no=$3,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, id, number)
	return err
}

// References are resolved by Export through the trusted gateway; linking does
// not alter the handoff's independent execution identity or publish events.
func (s *Service) LinkManualShippingContract(ctx context.Context, tenant, id, contractID int64, number string, op Operator) (ContractHandoff, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return linkManualContract(ctx, tx, tenant, id, contractID, number, op) })
	if err != nil {
		return ContractHandoff{}, err
	}
	return s.GetContractHandoff(ctx, tenant, id)
}
