package app

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/approval/internal/store"
)

// NodeInput is one step as the designer describes it.
type NodeInput struct {
	Name         string
	ApproverType string
	// Role id, employee id, or how far up the reporting line for MANAGER.
	ApproverRef int64
	ApproveMode string
}

var approverTypes = map[string]bool{"ROLE": true, "EMPLOYEE": true, "MANAGER": true}
var approveModes = map[string]bool{"ANY": true, "ALL": true}

func (s *Service) ListDefinitions(ctx context.Context, tenantID int64, bizType string) ([]store.ListDefinitionsRow, error) {
	rows, err := s.q.ListDefinitions(ctx, tenantID)
	if err != nil || bizType == "" {
		return rows, err
	}
	out := rows[:0]
	for _, r := range rows {
		if r.BizType == bizType {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *Service) GetDefinition(ctx context.Context, tenantID, id int64) (store.GetDefinitionRow, []store.ApprovalNode, error) {
	def, err := s.q.GetDefinition(ctx, store.GetDefinitionParams{TenantID: tenantID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.GetDefinitionRow{}, nil, apierr.NotFound("AP_DEFINITION_NOT_FOUND", "审批流不存在")
		}
		return store.GetDefinitionRow{}, nil, err
	}
	nodes, err := s.q.ListNodes(ctx, store.ListNodesParams{TenantID: tenantID, DefinitionID: def.ID})
	return def, nodes, err
}

// SaveDefinition writes a new version and retires the previous one.
//
// Editing the active definition in place would change the rules under
// instances that are halfway through it: an approver could find the step they
// are looking at has moved or vanished. A new version leaves them alone,
// because every instance records the definition id it started with.
func (s *Service) SaveDefinition(ctx context.Context, tenantID int64, bizType, name, minAmount string, nodes []NodeInput, actorID int64) (store.GetDefinitionRow, []store.ApprovalNode, error) {
	if bizType == "CONTRACT" && (len(nodes) != 1 || nodes[0].ApproverType != "MANAGER" || nodes[0].ApproverRef != 1 || nodes[0].ApproveMode != "ANY") {
		return store.GetDefinitionRow{}, nil, apierr.Invalid("AP_CONTRACT_SINGLE_CONFIRMATION", "外销合同只需要一次上级确认")
	}
	floor, err := normalizeAmount(minAmount)
	if err != nil {
		return store.GetDefinitionRow{}, nil, err
	}
	if bizType == "" {
		return store.GetDefinitionRow{}, nil, apierr.Invalid("AP_BIZ_TYPE_REQUIRED", "单据类型必填")
	}
	if len(nodes) == 0 {
		return store.GetDefinitionRow{}, nil, apierr.Invalid("AP_DEFINITION_EMPTY", "审批流至少要有一个节点")
	}
	for i, n := range nodes {
		if err := validateNode(i, n); err != nil {
			return store.GetDefinitionRow{}, nil, err
		}
	}

	version, err := s.q.NextDefinitionVersion(ctx, store.NextDefinitionVersionParams{
		TenantID: tenantID, BizType: bizType, MinAmount: floor,
	})
	if err != nil {
		return store.GetDefinitionRow{}, nil, err
	}

	var def store.GetDefinitionRow
	var created []store.ApprovalNode
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		row, err := q.CreateDefinition(ctx, store.CreateDefinitionParams{
			TenantID: tenantID, BizType: bizType,
			Name: orDefault(name, bizType+" 审批"), Version: version,
			MinAmount: floor, CreatedBy: actorID,
		})
		if err != nil {
			return err
		}
		def = store.GetDefinitionRow(row)
		if err := q.DeactivateOtherDefinitions(ctx, store.DeactivateOtherDefinitionsParams{
			TenantID: tenantID, BizType: bizType, MinAmount: floor, KeepID: def.ID,
		}); err != nil {
			return err
		}
		for i, n := range nodes {
			node, err := q.CreateNode(ctx, store.CreateNodeParams{
				TenantID: tenantID, DefinitionID: def.ID, Seq: int32(i + 1),
				Name: n.Name, ApproverType: n.ApproverType,
				ApproverRef: n.ApproverRef, ApproveMode: n.ApproveMode,
			})
			if err != nil {
				return err
			}
			created = append(created, node)
		}
		return nil
	})
	if err != nil {
		return store.GetDefinitionRow{}, nil, err
	}
	return def, created, nil
}

func validateNode(i int, n NodeInput) error {
	pos := itoa(i + 1)
	if n.Name == "" {
		return apierr.Invalid("AP_NODE_NAME_REQUIRED", "节点名称必填").WithMeta("seq", pos)
	}
	if !approverTypes[n.ApproverType] {
		return apierr.Invalid("AP_APPROVER_TYPE_INVALID", "不支持的审批人类型").
			WithMeta("seq", pos).WithMeta("approver_type", n.ApproverType)
	}
	if !approveModes[n.ApproveMode] {
		return apierr.Invalid("AP_APPROVE_MODE_INVALID", "审批模式只能是或签或会签").
			WithMeta("seq", pos)
	}
	// MANAGER counts levels and defaults to 1; the other two point at a row
	// that has to exist, so zero is a configuration mistake worth catching
	// here rather than at submit time.
	if n.ApproverType != "MANAGER" && n.ApproverRef == 0 {
		return apierr.Invalid("AP_APPROVER_REF_REQUIRED", "请选择审批人").WithMeta("seq", pos)
	}
	return nil
}

// DeleteBand retires an amount band. The base band stays: an amount of zero
// has to match something, and leaving nothing to match would make every
// submission fail.
func (s *Service) DeleteBand(ctx context.Context, tenantID int64, bizType, minAmount string) (bool, error) {
	floor, err := normalizeAmount(minAmount)
	if err != nil {
		return false, err
	}
	if floor == "0" {
		return false, apierr.Invalid("AP_BASE_BAND_PROTECTED", "起始区间不能删除")
	}
	rows, err := s.q.DeleteBand(ctx, store.DeleteBandParams{
		TenantID: tenantID, BizType: bizType, MinAmount: floor,
	})
	return rows > 0, err
}

// normalizeAmount turns whatever the caller typed into a decimal string the
// database can compare. Empty means the base band.
func normalizeAmount(v string) (string, error) {
	if v == "" {
		return "0", nil
	}
	d, err := decimal.NewFromString(v)
	if err != nil || d.IsNegative() {
		return "", apierr.Invalid("AP_AMOUNT_INVALID", "金额区间必须是不小于 0 的数字").
			WithMeta("amount", v)
	}
	return d.StringFixed(2), nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
