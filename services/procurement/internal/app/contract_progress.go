package app

import (
	"context"

	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

// ContractProgress 是一张合同的采购办到哪了，按项数说。
//
// 按项数而不是数量：一张合同上 100 吨钢卷和 50 件配件加不起来；折成金额
// 又会把采购成本混进一张讲营收的表。「共 3 项，3 项订齐，2 项到齐」单位
// 无关，也正是采购员口头汇报的说法。
type ContractProgress struct {
	ContractID    int64
	TotalLines    int32
	OrderedLines  int32
	ReceivedLines int32
}

// ContractProcurementProgress 一次答完一页合同的采购进度（D2）。
//
// 批量而不是逐行：一览表一页 20 张合同，逐行问就是 20 次跨服务调用。
//
// **不设数据范围**：调用方是网关的一览表，那一行能不能看已经由出口的合同
// 围栏判过了。这里再判一次「这个人是不是该需求的属主」，结果会是同一行里
// 采购列空着而出运列有数——读起来就是「没采购」，而真相是「你没权限看」。
// 一个给出错误答案的表格比没有这张表格更糟。安全性靠只给数字来保证：项数
// 里没有供应商，也没有采购价。
func (s *Service) ContractProcurementProgress(ctx context.Context, tenantID int64, contractIDs []int64) (map[int64]ContractProgress, error) {
	if len(contractIDs) == 0 {
		return map[int64]ContractProgress{}, nil
	}
	rows, err := s.q.ContractProcurementProgress(ctx, store.ContractProcurementProgressParams{
		TenantID: tenantID, ContractIds: contractIDs,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[int64]ContractProgress, len(rows))
	for _, r := range rows {
		out[r.ContractID] = ContractProgress{
			ContractID: r.ContractID, TotalLines: r.TotalLines,
			OrderedLines: r.OrderedLines, ReceivedLines: r.ReceivedLines,
		}
	}
	return out, nil
}
