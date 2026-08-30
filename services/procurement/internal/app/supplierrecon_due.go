package app

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

// 事后改一张采购单的应付到期日。
//
// 建单时填的日子会变：谈判改了付款条件，或者一开始就填错。到期日既然是
// 单据的一部分，就得能改——而采购单本身**已下单之后没有任何编辑入口**
// （编辑表单只对草稿和被驳回的单开放），所以这条路必须自己开一个门，
// 开在对账页上，跟「冲销」「撤销完成」并排。
//
// 改动一律留痕。理由是它直接决定这张单算不算逾期：一个人把日子往后推
// 一个月，页面上的「已逾期」就少一条，而这件事在别处一点痕迹都没有。
// 所以理由必填，旧值新值一起记进 purchase_order_due_changes。

// SetPOPayableDue 改到期日，返回改完之后的整行（页面直接换掉那一行）。
//
// dueDate 传空串表示「清空，回到没填」——填错了要能撤回，这也是一次
// 正当的改动，同样要写理由、同样留痕。
func (s *Service) SetPOPayableDue(ctx context.Context, tenantID, poID int64,
	dueDate, reason string, op Operator) (SupplierReconRow, error) {
	if err := s.requireFullScope(ctx, op); err != nil {
		return SupplierReconRow{}, err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return SupplierReconRow{}, apierr.Invalid("PR_DUE_REASON_REQUIRED",
			"改应付到期日必须写原因——这个日子决定这张单算不算逾期")
	}
	dueDate = strings.TrimSpace(dueDate)
	if err := validBusinessDate(dueDate, "PR_DUE_DATE_INVALID", "应付到期日"); err != nil {
		return SupplierReconRow{}, err
	}
	// 先读旧值。留痕要记「从哪天改到哪天」，只记新值的话，事后看不出这次
	// 改动到底把逾期推走了多远。顺带确认这张单存在且属于这个租户。
	before, err := s.reconRowOf(ctx, tenantID, poID)
	if err != nil {
		return SupplierReconRow{}, err
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.SetPurchaseOrderPayableDue(ctx, store.SetPurchaseOrderPayableDueParams{
			TenantID: tenantID, ID: poID, DueDate: dueDate,
		}); err != nil {
			return err
		}
		return q.RecordPayableDueChange(ctx, store.RecordPayableDueChangeParams{
			TenantID: tenantID, PoID: poID,
			OldDueDate: before.DueDate, NewDueDate: dueDate, Reason: reason,
			ChangedByID: op.ID, ChangedByName: op.Name,
		})
	})
	if err != nil {
		return SupplierReconRow{}, err
	}
	s.nudge(ctx, tenantID)
	return s.reconRowOf(ctx, tenantID, poID)
}
