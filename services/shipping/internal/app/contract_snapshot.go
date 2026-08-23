package app

import (
	"context"

	"github.com/sgao19/erp-go/services/shipping/internal/store"
)

// ContractSnapshot 是一张合同最新一班船的状态。
//
// 一张合同可能分几批走，一览表上只放最新那一班——「这单到哪了」问的是
// 当下。LegCount 一并给出，好让页面能说「共 3 班」而不是假装只有一班。
type ContractSnapshot struct {
	ContractNo string
	ScheduleID int64
	ScheduleNo string
	VesselName string
	VoyageNo   string
	Status     string
	ETD        string
	ATD        string
	ETA        string
	ATA        string
	DelayDays  int32
	LegCount   int32
}

// ContractShippingSnapshot 一次答完一页合同的船期状态（D2）。
//
// 按合同号而不是合同 id 关联：shipping_schedules.contract_id 可空，真正
// 建了索引也真正填得住的是 contract_no。
//
// **不设数据范围**，理由同采购那边：一行里有的列有数、有的列空着，读起来
// 是「还没订舱」而不是「你没权限看」。船期自己的围栏管的是「谁能改这班
// 船」，这里只读状态，点进船期页要改的时候围栏照常生效。
func (s *Service) ContractShippingSnapshot(ctx context.Context, tenantID int64, contractNos []string) (map[string]ContractSnapshot, error) {
	if len(contractNos) == 0 {
		return map[string]ContractSnapshot{}, nil
	}
	rows, err := s.q.ContractShippingSnapshot(ctx, store.ContractShippingSnapshotParams{
		TenantID: tenantID, ContractNos: contractNos,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]ContractSnapshot, len(rows))
	for _, r := range rows {
		out[r.ContractNo] = ContractSnapshot{
			ContractNo: r.ContractNo, ScheduleID: r.ScheduleID, ScheduleNo: r.ScheduleNo,
			VesselName: r.VesselName, VoyageNo: r.VoyageNo, Status: r.Status,
			ETD: r.Etd, ATD: r.Atd, ETA: r.Eta, ATA: r.Ata,
			DelayDays: r.DelayDays, LegCount: r.LegCount,
		}
	}
	return out, nil
}
