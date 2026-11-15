package app

import (
	"context"
	"fmt"
	"strings"
)

// ScheduleIdentities enriches already-authorized schedule/reminder rows in one
// tenant-scoped query. A batch is never inferred from the contract or defaulted
// to 1: legacy schedules without an unambiguous handoff retain batch 0.
func (s *Service) ScheduleIdentities(ctx context.Context, tenantID int64, ids []int64) (map[int64]ScheduleIdentity, error) {
	out := make(map[int64]ScheduleIdentity, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT s.id,s.schedule_no,coalesce(nullif(s.contract_no,''),h.contract_no,''),coalesce(h.batch_no,0),coalesce(m.order_no,''),coalesce(s.contract_handoff_id,0)
 FROM shipping_schedules s
 LEFT JOIN LATERAL (
   SELECT min(h.contract_no) AS contract_no,min(h.batch_no) AS batch_no
   FROM contract_shipping_handoffs h
   WHERE h.tenant_id=s.tenant_id AND
     (h.id=nullif(s.contract_handoff_id,0) OR
       (nullif(s.contract_handoff_id,0) IS NULL AND h.schedule_id=s.id))
   HAVING count(*)=1
 ) h ON true
 LEFT JOIN manual_shipping_orders m ON m.tenant_id=s.tenant_id AND m.handoff_id=s.contract_handoff_id
 WHERE s.tenant_id=$1 AND s.id=ANY($2::bigint[])`, tenantID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var v ScheduleIdentity
		if err = rows.Scan(&id, &v.ScheduleNo, &v.ContractNo, &v.BatchNo, &v.ManualOrderNo, &v.HandoffID); err != nil {
			return nil, err
		}
		out[id] = v
	}
	return out, rows.Err()
}

type ScheduleIdentity struct {
	ScheduleNo, ContractNo, ManualOrderNo string
	HandoffID                             int64
	BatchNo                               int32
}

func (v ScheduleIdentity) Label() string {
	if v.ManualOrderNo != "" {
		return v.ManualOrderNo + " · 历史补录"
	}
	label := strings.TrimSpace(v.ContractNo)
	if label == "" {
		label = "未关联合同"
	}
	if v.BatchNo > 0 {
		return fmt.Sprintf("%s · 第 %d 批", label, v.BatchNo)
	}
	return label + " · 批次未关联"
}

// Read-time formatting also covers existing reminders; persisted reminder
// content, read state and business records remain untouched.
func (v ScheduleIdentity) ReminderContent(content string) string {
	parts := []string{v.Label()}
	for _, part := range strings.Split(content, "；") {
		part = strings.TrimSpace(part)
		if part == "" || strings.HasPrefix(part, "船期编号：") || strings.HasPrefix(part, "合同编号：") || strings.HasPrefix(part, "运输批次：") {
			continue
		}
		if v.ScheduleNo != "" {
			part = strings.ReplaceAll(part, v.ScheduleNo, v.Label())
		}
		parts = append(parts, part)
	}
	if v.ScheduleNo != "" {
		parts = append(parts, "船期编号："+v.ScheduleNo)
	}
	return strings.Join(parts, "；")
}
