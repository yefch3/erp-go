package app

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

// 改一份合同的应收到期日。
//
// 到期日现在是合同自己的字段，建合同的人填。填完之后还会变：谈判改了付款
// 条件，或者一开始就填错。合同的常规编辑口只对草稿开放，而且只放销售属主
// 过（mustOwnContract），所以这条路自己开一个门，开在客户对账页上，和
// 「冲销」「撤销完成」并排——那几个动作也是租户级 + export:receipt:write，
// 这里保持一致。
//
// 改动一律留痕。它直接决定这份合同算不算逾期：把日子往后推一个月，页面上
// 的「已逾期」就少一条，而这件事在别处一点痕迹都没有。

// SetContractReceivableDue 改到期日；dueDate 传空串表示清空，回到「没填」。
func (s *Service) SetContractReceivableDue(ctx context.Context, tenantID, contractID int64,
	dueDate, reason string, op Operator) (string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", apierr.Invalid("EX_DUE_REASON_REQUIRED",
			"改应收到期日必须写原因——这个日子决定这份合同算不算逾期")
	}
	dueDate = strings.TrimSpace(dueDate)
	if err := validBusinessDate(dueDate, "EX_DUE_DATE_INVALID", "应收到期日"); err != nil {
		return "", err
	}
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		// 旧值在同一个事务里读，留痕要记「从哪天改到哪天」；顺带确认这份
		// 合同存在且属于这个租户。
		old, err := q.ContractReceivableDue(ctx, store.ContractReceivableDueParams{
			TenantID: tenantID, ID: contractID,
		})
		if err == pgx.ErrNoRows {
			return apierr.NotFound("EX_CONTRACT_NOT_FOUND", "合同不存在")
		}
		if err != nil {
			return err
		}
		if old == dueDate {
			// 没变就什么都不做：留痕里塞一条「从 X 改成 X」，和清掉一轮
			// 催收提醒，都是白费。
			return nil
		}
		if err := q.SetContractReceivableDue(ctx, store.SetContractReceivableDueParams{
			TenantID: tenantID, ID: contractID, DueDate: dueDate,
		}); err != nil {
			return err
		}
		if err := q.RecordContractDueChange(ctx, store.RecordContractDueChangeParams{
			TenantID: tenantID, ContractID: contractID,
			OldDueDate: old, NewDueDate: dueDate, Reason: reason,
			ChangedByID: op.ID, ChangedByName: op.Name,
		}); err != nil {
			return err
		}
		// 催收的幂等键里带着到期日，日子一换，同一档提醒就成了「新的一条」，
		// 扫描器下一轮会把 SOON / DUE / OVERDUE 全部重发。把未读的先清掉，
		// 让下一轮按新日子重新决定该不该提醒。
		_, err = q.ClearContractReminders(ctx, store.ClearContractRemindersParams{
			TenantID: tenantID, ContractID: contractID,
		})
		return err
	})
	if err != nil {
		return "", err
	}
	return dueDate, nil
}
