package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/masterref"
)

type ReferenceChecker func(context.Context, int64, string, []masterref.Candidate) (map[int64]string, error)

func (s *Service) UseReferenceChecker(check ReferenceChecker) { s.references = check }

type BulkDeleteInput struct {
	Entity, StartAt, EndAt string
	Execute                bool
	Selections             []BulkDeleteSelection
}
type BulkDeleteSelection struct {
	ID      int64
	Version string
}
type BulkDeleteRow struct {
	ID                                            int64
	Code, Name, CreatedAt, Version, BlockedReason string
	Labels                                        []string
}
type BulkDeleteResult struct {
	Rows         []BulkDeleteRow
	DeletedCount int32
}
type bulkSpec struct {
	table, key, code, name string
	children               []string
}

func bulkEntity(entity string) (bulkSpec, error) {
	switch entity {
	case "CUSTOMER":
		return bulkSpec{"customers", "customer_id", "code", "name", []string{"customer_custom_field_values", "customer_addresses", "customer_owners", "customer_contacts"}}, nil
	case "SUPPLIER":
		return bulkSpec{"suppliers", "supplier_id", "code", "coalesce(nullif(name_zh,''),nullif(name_en,''),name)", []string{"supplier_owners", "supplier_contacts"}}, nil
	case "PORT":
		return bulkSpec{"ports", "port_id", "unlocode", "coalesce(nullif(name_zh,''),name_en)", nil}, nil
	default:
		return bulkSpec{}, apierr.Invalid("MASTER_DELETE_ENTITY", "不支持的基础数据类型")
	}
}
func bulkWindow(in BulkDeleteInput) (time.Time, time.Time, error) {
	start, e1 := time.Parse(time.RFC3339, in.StartAt)
	end, e2 := time.Parse(time.RFC3339, in.EndAt)
	if e1 != nil || e2 != nil || !start.Before(end) {
		return start, end, apierr.Invalid("MASTER_DELETE_DATES", "请选择有效的创建日期范围")
	}
	return start, end, nil
}
func (s *Service) BulkDelete(ctx context.Context, tenant, operator int64, operatorName string, in BulkDeleteInput) (BulkDeleteResult, error) {
	out := BulkDeleteResult{Rows: []BulkDeleteRow{}}
	spec, err := bulkEntity(in.Entity)
	if err != nil {
		return out, err
	}
	start, end, err := bulkWindow(in)
	if err != nil {
		return out, err
	}
	if tenant <= 0 || operator <= 0 {
		return out, apierr.Permission("MASTER_DELETE_AUTH", "需要登录用户")
	}
	if in.Execute && (len(in.Selections) == 0 || len(in.Selections) > 1000) {
		return out, apierr.Invalid("MASTER_DELETE_SELECTION", "请选择 1 至 1000 条预览记录")
	}
	if s.references == nil {
		return out, apierr.Conflict("MASTER_DELETE_CHECK_UNAVAILABLE", "关联检查未配置，不能删除")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, "SET LOCAL lock_timeout='3s'")
	if err != nil {
		return out, err
	}
	selected := map[int64]string{}
	ids := []int64{}
	for _, item := range in.Selections {
		if item.ID <= 0 || item.Version == "" || selected[item.ID] != "" {
			return out, apierr.Invalid("MASTER_DELETE_SELECTION", "删除记录无效或重复")
		}
		selected[item.ID] = item.Version
		ids = append(ids, item.ID)
	}
	query := fmt.Sprintf(`SELECT id,%s,%s,created_at,md5(to_jsonb(t)::text),to_jsonb(t) FROM %s t WHERE tenant_id=$1 AND created_at >= $2 AND created_at < $3`, spec.code, spec.name, spec.table)
	args := []any{tenant, start, end}
	if in.Execute {
		query += " AND id=ANY($4::bigint[])"
		args = append(args, ids)
	}
	query += " ORDER BY id LIMIT 1001"
	if in.Execute {
		query += " FOR UPDATE"
	}
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var row BulkDeleteRow
		var created time.Time
		var raw []byte
		if err = rows.Scan(&row.ID, &row.Code, &row.Name, &created, &row.Version, &raw); err != nil {
			rows.Close()
			return out, err
		}
		row.CreatedAt = created.UTC().Format(time.RFC3339Nano)
		var value map[string]any
		if err = json.Unmarshal(raw, &value); err != nil {
			rows.Close()
			return out, err
		}
		row.Labels = []string{row.Name, row.Code}
		for _, key := range []string{"short_name", "english_name", "name_zh", "name_en"} {
			if label, ok := value[key].(string); ok && strings.TrimSpace(label) != "" {
				row.Labels = append(row.Labels, label)
			}
		}
		out.Rows = append(out.Rows, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(out.Rows) > 1000 {
		return BulkDeleteResult{}, apierr.Invalid("MASTER_DELETE_LIMIT", "匹配超过 1000 条，请缩小日期范围后分批操作")
	}
	if in.Execute && len(out.Rows) != len(selected) {
		return BulkDeleteResult{}, apierr.Conflict("MASTER_DELETE_STALE", "记录已变化，请重新预览")
	}
	candidates := []masterref.Candidate{}
	for _, row := range out.Rows {
		if in.Execute && selected[row.ID] != row.Version {
			return BulkDeleteResult{}, apierr.Conflict("MASTER_DELETE_STALE", "资料已修改，请重新预览")
		}
		candidates = append(candidates, masterref.Candidate{ID: row.ID, Labels: row.Labels})
	}
	skip := map[string]bool{"customers": true, "suppliers": true, "ports": true, "customer_change_logs": true, "supplier_change_logs": true, "port_change_logs": true}
	for _, table := range spec.children {
		skip[table] = true
	}
	local, err := masterref.Check(ctx, tx, tenant, in.Entity, candidates, skip)
	if err != nil {
		return out, err
	}
	// Credit assessments use a polymorphic party_id, rather than customer_id.
	if in.Entity != "PORT" {
		rr, e := tx.Query(ctx, `SELECT DISTINCT party_id FROM credit_ratings WHERE tenant_id=$1 AND party_type=$2 AND party_id=ANY($3::bigint[])`, tenant, in.Entity, func() []int64 {
			v := []int64{}
			for _, c := range candidates {
				v = append(v, c.ID)
			}
			return v
		}())
		if e != nil {
			return out, e
		}
		for rr.Next() {
			var id int64
			if e = rr.Scan(&id); e != nil {
				rr.Close()
				return out, e
			}
			local[id] = true
		}
		e = rr.Err()
		rr.Close()
		if e != nil {
			return out, e
		}
	}
	remote, err := s.references(ctx, tenant, in.Entity, candidates)
	if err != nil {
		return BulkDeleteResult{}, apierr.Conflict("MASTER_DELETE_CHECK_UNAVAILABLE", "业务关联检查未完成，请稍后重新预览；未删除任何资料")
	}
	blocked := false
	for i := range out.Rows {
		row := &out.Rows[i]
		if local[row.ID] {
			row.BlockedReason = "已有关联资料或业务记录"
		}
		if reason := remote[row.ID]; reason != "" {
			row.BlockedReason = reason
		}
		blocked = blocked || row.BlockedReason != ""
	}
	if !in.Execute {
		return out, nil
	}
	// A changed selection never partially executes: the caller must review again.
	if blocked {
		return out, apierr.Conflict("MASTER_DELETE_REFERENCED", "所选记录已有业务引用，请重新预览；未删除任何资料")
	}
	for _, row := range out.Rows {
		for _, table := range spec.children {
			_, err = tx.Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE tenant_id=$1 AND %s=$2", table, spec.key), tenant, row.ID)
			if err != nil {
				return out, err
			}
		}
		// Preserve the existing audit trail. No recycle bin or new audit subsystem.
		if in.Entity == "PORT" {
			_, err = tx.Exec(ctx, `INSERT INTO port_change_logs(tenant_id,port_id,action,before_data,operator_id,operator_name) SELECT tenant_id,id,'DELETE',to_jsonb(p),$3,$4 FROM ports p WHERE tenant_id=$1 AND id=$2`, tenant, row.ID, operator, operatorName)
		} else {
			prefix := strings.ToLower(in.Entity)
			_, err = tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s_change_logs(tenant_id,%s,action,section,summary,before_data,operator_id,operator_name) SELECT tenant_id,id,'DELETE','BASIC','按创建时间批量删除',to_jsonb(t),$3,$4 FROM %s t WHERE tenant_id=$1 AND id=$2`, prefix, spec.key, spec.table), tenant, row.ID, operator, operatorName)
		}
		if err != nil {
			return out, err
		}
		_, err = tx.Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE tenant_id=$1 AND id=$2", spec.table), tenant, row.ID)
		if err != nil {
			return out, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	out.DeletedCount = int32(len(out.Rows))
	return out, nil
}
