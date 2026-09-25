// Package masterref checks business-owned databases before master records are removed.
package masterref

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

type Candidate struct {
	ID     int64
	Labels []string
}
type DB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}
type Handler struct {
	commonv1.UnimplementedMasterDataReferenceServiceServer
	Pool *pgxpool.Pool
}

func (h *Handler) CheckMasterDataReferences(ctx context.Context, in *commonv1.CheckMasterDataReferencesRequest) (*commonv1.CheckMasterDataReferencesResponse, error) {
	op, ok := grpcx.OperatorFromContext(ctx)
	if !ok || op.TenantID <= 0 || op.EmployeeID <= 0 {
		return nil, apierr.Permission("MASTER_DELETE_AUTH", "需要登录用户")
	}
	candidates := []Candidate{}
	for _, c := range in.Candidates {
		candidates = append(candidates, Candidate{ID: c.Id, Labels: c.Labels})
	}
	used, err := Check(ctx, h.Pool, op.TenantID, in.Entity, candidates, nil)
	if err != nil {
		return nil, err
	}
	out := &commonv1.CheckMasterDataReferencesResponse{}
	for _, c := range in.Candidates {
		if used[c.Id] {
			out.ReferencedIds = append(out.ReferencedIds, c.Id)
		}
	}
	return out, nil
}
func keys(entity string) ([]string, []string, error) {
	switch entity {
	case "CUSTOMER":
		return []string{"customer_id", "customerId"}, []string{"customer", "customer_name", "customerName"}, nil
	case "SUPPLIER":
		return []string{"supplier_id", "supplierId", "carrier_id", "carrierId", "forwarder_id", "forwarderId", "actual_carrier_id", "actualCarrierId", "final_forwarder_id", "finalForwarderId"}, []string{"supplier", "supplier_name", "supplierName", "company", "carrier", "carrier_name", "carrierName", "forwarder_name", "forwarderName", "actual_carrier_name", "actualCarrierName", "final_forwarder_name", "finalForwarderName"}, nil
	case "PORT":
		return []string{"port_id", "portId", "loading_port_id", "loadingPortId", "discharge_port_id", "dischargePortId", "delivery_port_id", "deliveryPortId", "destination_port_id", "destinationPortId"}, []string{"port", "loadingPort", "dischargePort", "destinationPort", "deliveryPort", "loading_port_name", "discharge_port_name", "loading_port", "destination_port", "delivery_port", "unlocode", "port_name", "delivery_port_name", "origin_port", "originPort"}, nil
	default:
		return nil, nil, apierr.Invalid("MASTER_DELETE_ENTITY", "不支持的基础数据类型")
	}
}

// Check includes all statuses: even cancelled/historical business records retain
// their identity references. Every query is tenant-scoped. Errors fail closed.
// Discover columns from the database rather than assuming another service's schema.
func Check(ctx context.Context, db DB, tenant int64, entity string, candidates []Candidate, skip map[string]bool) (map[int64]bool, error) {
	idKeys, nameKeys, err := keys(entity)
	if err != nil {
		return nil, err
	}
	if tenant <= 0 || len(candidates) > 1000 {
		return nil, apierr.Invalid("MASTER_DELETE_LIMIT", "最多预览 1000 条，请缩小日期范围")
	}
	used := map[int64]bool{}
	if len(candidates) == 0 {
		return used, nil
	}
	ids := []int64{}
	labels := []string{}
	labelIDs := []int64{}
	for _, c := range candidates {
		if c.ID <= 0 {
			return nil, apierr.Invalid("MASTER_DELETE_ID", "无效记录")
		}
		ids = append(ids, c.ID)
		for _, label := range c.Labels {
			if strings.TrimSpace(label) != "" {
				labels = append(labels, strings.TrimSpace(label))
				labelIDs = append(labelIDs, c.ID)
			}
		}
	}
	rows, err := db.Query(ctx, `SELECT c.table_name,c.column_name,c.data_type FROM information_schema.columns c
 JOIN information_schema.tables t ON t.table_schema=c.table_schema AND t.table_name=c.table_name AND t.table_type='BASE TABLE'
 WHERE c.table_schema='public' AND EXISTS(SELECT 1 FROM information_schema.columns x WHERE x.table_schema=c.table_schema AND x.table_name=c.table_name AND x.column_name='tenant_id')
 AND (c.column_name=ANY($1::text[]) OR c.column_name=ANY($2::text[]) OR c.data_type IN ('json','jsonb')) ORDER BY c.table_name,c.column_name`, idKeys, nameKeys)
	if err != nil {
		return nil, err
	}
	type col struct{ table, name, kind string }
	cols := []col{}
	for rows.Next() {
		var c col
		if err = rows.Scan(&c.table, &c.name, &c.kind); err != nil {
			rows.Close()
			return nil, err
		}
		if !skip[c.table] {
			cols = append(cols, c)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, c := range cols {
		table := pgx.Identifier{"public", c.table}.Sanitize()
		column := pgx.Identifier{c.name}.Sanitize()
		var query string
		var args []any
		if c.kind == "jsonb" || c.kind == "json" {
			// Recursive key inspection covers inquiry/quote bodies and immutable snapshots.
			// JSON numeric IDs and string IDs are both accepted; free-text parties/ports
			// are checked conservatively against their stored names and codes.
			query = fmt.Sprintf(`WITH pairs AS (SELECT p.key,p.value FROM %s b CROSS JOIN LATERAL jsonb_path_query(b.%s::jsonb,'strict $.** ? (@.type() == "object")') obj CROSS JOIN LATERAL jsonb_each(obj) p WHERE b.tenant_id=$1)
 SELECT DISTINCT id FROM unnest($2::bigint[]) id WHERE EXISTS(SELECT 1 FROM pairs WHERE key=ANY($3::text[]) AND value #>> '{}' = id::text)
 UNION SELECT DISTINCT l.id FROM unnest($4::bigint[],$5::text[]) l(id,label) WHERE EXISTS(SELECT 1 FROM pairs WHERE key=ANY($6::text[]) AND btrim(value #>> '{}')=l.label)`, table, column)
			args = []any{tenant, ids, idKeys, labelIDs, labels, nameKeys}
		} else {
			isID := false
			for _, key := range idKeys {
				if key == c.name {
					isID = true
				}
			}
			if isID {
				query = fmt.Sprintf(`SELECT DISTINCT x.id FROM unnest($2::bigint[]) x(id) JOIN %s b ON b.%s::text=x.id::text WHERE b.tenant_id=$1`, table, column)
				args = []any{tenant, ids}
			} else {
				query = fmt.Sprintf(`SELECT DISTINCT x.id FROM unnest($2::bigint[],$3::text[]) x(id,label) JOIN %s b ON btrim(b.%s::text)=x.label WHERE b.tenant_id=$1`, table, column)
				args = []any{tenant, labelIDs, labels}
			}
		}
		hits, e := db.Query(ctx, query, args...)
		if e != nil {
			return nil, fmt.Errorf("reference check %s: %w", c.table, e)
		}
		for hits.Next() {
			var id int64
			if e = hits.Scan(&id); e != nil {
				hits.Close()
				return nil, e
			}
			used[id] = true
		}
		e = hits.Err()
		hits.Close()
		if e != nil {
			return nil, e
		}
	}
	return used, nil
}
