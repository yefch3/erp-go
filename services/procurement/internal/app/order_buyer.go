package app

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 历史补录单的采购员：可以指定，可以换。
//
// 采购单按采购员决定谁看得见（order_scope.go）。历史补录的单以前一律记在录入
// 人名下、换不了——而补录常常是公司派一个人统一录，真正负责的是别的采购。
// 2026-11 的 #643：一位同事录了 29 张，「只看得到自己单子」的采购同事一张都
// 找不到，供应商和价格没人能补。
//
// 这里只换采购员这一件事：不动产品、价格、状态、付款，也不走审批——历史单
// 本来就不走审批。范围只到历史补录的单：正常流程里的单子，采购员同时是审批
// 的发起人、草稿的所有者，换人牵扯的东西多得多，不在这里。
//
// 谁能换：看得见这张单的人（「维护采购订单」这项权限由网关把着）。给出去不
// 泄露任何东西——他本来就看得见——所以**不要求对方在自己的可见范围内**：
// 否则「只看得到自己」的录入人谁也给不了，这个功能对最需要它的人等于没有。
// 代价是给出去之后他自己可能就看不到了，页面上会先说明。
//
// 换给谁：持有「维护采购订单」权限、登得上来的在职同事。名单问 IAM 要，它出
// 名单用的规则和真正把关时是同一条（另加「有账号且没被停用」），所以下拉里的
// 人选了都接得住。名单对有这项权限的人都一样，不按各自的可见范围裁——它只有
// 姓名和部门，而「能把单子交给谁」本来就不该取决于自己看得见谁的单。

// orderBuyerPermission 是「能不能当采购员」的标准：接手之后他得改得了这张单。
const orderBuyerPermission = "procurement:order:write"

const orderBuyerReasonMax = 200

// PermissionHolders lists the colleagues who hold a permission. IAM answers
// by the same rule it applies when one of them actually acts.
type PermissionHolders interface {
	Holders(ctx context.Context, code string) ([]EmployeeIdentity, error)
}

// OrderBuyerChange is the buyer an order ends up with. Changed is false when
// that was already the buyer: nothing was written, nothing was logged.
type OrderBuyerChange struct {
	BuyerID   int64
	BuyerName string
	Changed   bool
}

// ListOrderBuyerCandidates is what the buyer picker offers.
func (s *Service) ListOrderBuyerCandidates(ctx context.Context, tenantID int64, op Operator) ([]EmployeeIdentity, error) {
	if tenantID <= 0 || op.ID <= 0 {
		return nil, apierr.Permission("PO_OPERATOR_REQUIRED", "请重新登录")
	}
	return s.orderBuyerCandidates(ctx)
}

func (s *Service) orderBuyerCandidates(ctx context.Context) ([]EmployeeIdentity, error) {
	// 取不到名单要说出来：悄悄给一张空名单，页面上就是「没有人可选」，人会去
	// 查同事的权限，而问题其实在这头。
	if s.holders == nil {
		return nil, apierr.Internal("PO_BUYER_DIRECTORY_UNAVAILABLE", "暂时取不到可选的采购员，请稍后再试")
	}
	return s.holders.Holders(ctx, orderBuyerPermission)
}

// ChangeOrderBuyer hands a historical order to another buyer.
func (s *Service) ChangeOrderBuyer(ctx context.Context, tenantID, orderID, buyerID int64, reason string, op Operator) (OrderBuyerChange, error) {
	if tenantID <= 0 || op.ID <= 0 {
		return OrderBuyerChange{}, apierr.Permission("PO_OPERATOR_REQUIRED", "请重新登录")
	}
	if buyerID <= 0 {
		return OrderBuyerChange{}, apierr.Invalid("PO_BUYER_REQUIRED", "请选择采购员")
	}
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > orderBuyerReasonMax {
		return OrderBuyerChange{}, apierr.Invalid("PO_BUYER_REASON_TOO_LONG", "原因最多 200 字")
	}
	visible, err := s.visibleOrdersTo(ctx, op)
	if err != nil {
		return OrderBuyerChange{}, err
	}
	candidates, err := s.orderBuyerCandidates(ctx)
	if err != nil {
		return OrderBuyerChange{}, err
	}
	var target *EmployeeIdentity
	for i := range candidates {
		if candidates[i].ID == buyerID {
			target = &candidates[i]
			break
		}
	}

	var out OrderBuyerChange
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var oldID int64
		var oldName string
		var historical bool
		e := tx.QueryRow(ctx, `SELECT o.buyer_id,o.buyer_name,EXISTS(SELECT 1 FROM historical_purchase_orders h WHERE h.tenant_id=o.tenant_id AND h.po_id=o.id)
 FROM purchase_orders o WHERE o.tenant_id=$1 AND o.id=$2 FOR UPDATE`, tenantID, orderID).Scan(&oldID, &oldName, &historical)
		// 看不见和不存在给同一个回答（同 AuthorizeOrder）：确认别人名下有这张单，
		// 本身就是信息。可见性在锁住这一行之后才判——判完到改之间它不会再换人。
		if e == pgx.ErrNoRows || (e == nil && !ownerVisible(visible, oldID)) {
			return apierr.NotFound("PO_ORDER_NOT_FOUND", "采购单不存在")
		}
		if e != nil {
			return e
		}
		if !historical {
			return apierr.Conflict("PO_BUYER_HISTORICAL_ONLY", "只有历史补录的采购单可以更换采购员")
		}
		out = OrderBuyerChange{BuyerID: oldID, BuyerName: oldName}
		// 「换成现在这位」不是错：页面保存时不必先比一遍。也不看他还在不在
		// 名单里——人被收了权限，单子上写的仍然是他。
		if oldID == buyerID {
			return nil
		}
		if target == nil {
			return apierr.Invalid("PO_BUYER_NOT_ELIGIBLE", "所选同事已停用、没有登录账号，或没有维护采购订单的权限，不能担任采购员")
		}
		if _, e = tx.Exec(ctx, `UPDATE purchase_orders SET buyer_id=$3,buyer_name=$4,updated_at=now() WHERE tenant_id=$1 AND id=$2`,
			tenantID, orderID, target.ID, target.Name); e != nil {
			return e
		}
		// 历史单每一行都带一条「不进待采购」的支撑需求（见 SaveHistoricalOrder），
		// 它只为这一行存在，负责人就该是这张单的采购员。不跟着走的话，原来的人
		// 还能从需求那一头（质检来源按需求负责人放行）看到这张已经不归他的单。
		//
		// **不按「原负责人 = 原采购员」筛**：存量数据里，草稿被别人接着录过的，需求
		// 挂在最后保存的那个人名下（SaveHistoricalOrder 以前那样写），和单子上的
		// 采购员本来就不是同一个人。筛了，那几行就留在第三个人手里。认它的办法和
		// SaveHistoricalOrder 删它时用的是同一条：这张单的明细所指的、手工来源、
		// 已关闭的需求。
		if _, e = tx.Exec(ctx, `UPDATE purchase_requirements r SET owner_id=$3,owner_name=$4 FROM purchase_order_items i
 WHERE i.tenant_id=$1 AND i.po_id=$2 AND r.tenant_id=i.tenant_id AND r.id=i.requirement_id
 AND r.source='MANUAL' AND r.status='SUPERSEDED'`,
			tenantID, orderID, target.ID, target.Name); e != nil {
			return e
		}
		// 还开着的生产节点提醒是发给采购员的，跟着换；已关闭的是历史，不动。
		if _, e = tx.Exec(ctx, `UPDATE purchase_production_reminders SET buyer_id=$3,buyer_name=$4 WHERE tenant_id=$1 AND po_id=$2 AND status='OPEN'`,
			tenantID, orderID, target.ID, target.Name); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO purchase_order_buyer_changes(tenant_id,po_id,old_buyer_id,old_buyer_name,new_buyer_id,new_buyer_name,reason,changed_by_id,changed_by_name)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, tenantID, orderID, oldID, oldName, target.ID, target.Name, reason, op.ID, op.Name); e != nil {
			return e
		}
		out = OrderBuyerChange{BuyerID: target.ID, BuyerName: target.Name, Changed: true}
		return nil
	})
	if err != nil {
		return OrderBuyerChange{}, err
	}
	if out.Changed {
		s.nudge(ctx, tenantID)
	}
	return out, nil
}
