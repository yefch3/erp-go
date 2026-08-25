package app

import (
	"context"
	"fmt"

	"github.com/sgao19/erp-go/services/inventory/internal/store"
)

// 第一家公司安装时得到了一个「主仓库」（00001_stock.sql，tenant_id 写死 1），
// 之后开的公司一个都没有。
//
// 这一处比编码规则、审批流、下拉字典都轻，原因值得写下来：**采购单默认是
// 「直发」，直发不形成公司库存，收货也不发库存事件**（procurement 00022 与
// order.go 里的 directDelivery）。所以一家不囤货的贸易公司——拿到订单直接跟
// 工厂采购、货从工厂发到港口——从头到尾都不需要仓库，今天就能跑通。
//
// 需要仓库的只有一种情况：有人在采购单上选了「入库后发货」。那时下单页会明确
// 要求选仓库（PO_WAREHOUSE_REQUIRED），话说得清楚、时机也对。
//
// 所以补种放在**读仓库列表**的时候，而不是开户的时候：不囤货的公司永远不会
// 因为开了个户，就凭空多出一个它根本没有的仓库；真要用仓库的人，打开下拉框
// 就有一个能用的，和第一家公司一样。
var defaultWarehouses = []struct {
	Code string
	Name string
	Type string
}{
	{"WH01", "主仓库", "NORMAL"},
}

// seedDefaultWarehouses 给「一个仓库都没有」的公司补上默认仓库。
//
// 只在一条都没有时补。仓库可以被停用，停用是有人做过的决定——把一个特意关掉
// 的仓库变回来，比一开始就没有更糟：货会被收进一个公司认为已经不用了的地方。
func (s *Service) seedDefaultWarehouses(ctx context.Context, tenantID int64) error {
	n, err := s.q.CountWarehouses(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("inventory: count warehouses: %w", err)
	}
	if n > 0 {
		return nil
	}
	for _, w := range defaultWarehouses {
		if _, err := s.q.InsertWarehouseIfAbsent(ctx, store.InsertWarehouseIfAbsentParams{
			TenantID: tenantID, Code: w.Code, Name: w.Name, WhType: w.Type,
		}); err != nil {
			return fmt.Errorf("inventory: seed warehouse %s: %w", w.Code, err)
		}
	}
	return nil
}
