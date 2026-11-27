package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

type ExecutionDraftInput struct {
	AssignmentID               int64
	Price, Date, Terms, Remark string
	FileIDs                    []int64
}

type ExecutionDraftLine struct {
	ExecutionDraftInput
	RequirementID, QuoteID, BuyerID, SupplierID              int64
	BuyerName, SupplierName, Currency, ContractNo            string
	ProductName, Specification, Quantity, UomCode, UpdatedAt string
}

type ExecutionDraftGroup struct {
	BuyerID, SupplierID, OrderID                               int64
	BuyerName, SupplierName, Currency, Date, Terms, ContractNo string
	Lines                                                      []ExecutionDraftLine
}

func (s *Service) manageExecutionDraftBatch(ctx context.Context, tenant int64, op Operator, in ExecutionAssignmentInput, canActForOthers bool) (ExecutionAssignmentResult, error) {
	batchKey := strings.TrimSpace(in.BatchKey)
	if batchKey == "" || len(in.DraftLines) == 0 {
		return ExecutionAssignmentResult{}, apierr.Invalid("ASSIGNMENT_BATCH_REQUIRED", "请选择要生成草稿的实单询价")
	}
	requirements, _, _, err := s.ListExecutionBatchRequirements(ctx, tenant, op, "", batchKey, "", 1, 1)
	if err != nil {
		return ExecutionAssignmentResult{}, err
	}
	ids := make([]int64, 0, len(requirements))
	for _, row := range requirements {
		ids = append(ids, row.ID)
	}
	var result ExecutionAssignmentResult
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		// Lock demands first, in the same order for every batch, before locking tasks.
		if _, err := tx.Exec(ctx, `SELECT id FROM purchase_requirements WHERE tenant_id=$1 AND id=ANY($2::bigint[]) ORDER BY id FOR UPDATE`, tenant, ids); err != nil {
			return err
		}
		tasks, err := executionDraftTasks(ctx, tx, tenant, op.ID, ids, canActForOthers)
		if err != nil {
			return err
		}
		tasks, err = applyExecutionDraftInputs(tasks, in.DraftLines)
		if err != nil {
			return err
		}
		quantities := make(map[int64]decimal.Decimal)
		for _, task := range tasks {
			quantities[task.RequirementID] = quantities[task.RequirementID].Add(decimal.RequireFromString(task.Quantity))
			var count int
			if len(task.FileIDs) == 0 {
				continue
			}
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM purchase_execution_assignment_files WHERE tenant_id=$1 AND assignment_id=$2 AND id=ANY($3::bigint[])`,
				tenant, task.AssignmentID, task.FileIDs).Scan(&count); err != nil {
				return err
			}
			if count != len(task.FileIDs) {
				return apierr.Invalid("ASSIGNMENT_FILES_INVALID", "所选资料不属于此议价任务")
			}
		}
		for requirementID, quantity := range quantities {
			if err := checkExecutionDraftQuantity(ctx, tx, tenant, requirementID, quantity); err != nil {
				return err
			}
		}
		groups := groupExecutionDraftLines(tasks)
		body, err := json.Marshal(groups)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(body)
		token := hex.EncodeToString(hash[:])
		if in.Action == "draft_batch" {
			if token != in.DraftPreviewToken {
				return apierr.Conflict("ASSIGNMENT_BATCH_CHANGED", "议价任务或分组已变化，请重新预览")
			}
			for i := range groups {
				orderID, err := s.createExecutionDraftGroup(ctx, tx, tenant, op, groups[i])
				if err != nil {
					return err
				}
				groups[i].OrderID = orderID
			}
		}
		result.DraftGroups, result.DraftPreviewToken = groups, token
		return nil
	})
	if err != nil {
		return ExecutionAssignmentResult{}, err
	}
	if in.Action == "draft_batch" {
		s.nudge(ctx, tenant)
	}
	return result, nil
}

func executionDraftTasks(ctx context.Context, tx pgx.Tx, tenant, buyerID int64, requirements []int64, canActForOthers bool) ([]ExecutionDraftLine, error) {
	rows, err := tx.Query(ctx, `SELECT a.id,a.requirement_id,a.quote_id,a.buyer_id,q.supplier_id,a.buyer_name,
		q.supplier_name,q.currency,r.contract_no,r.product_name,r.spec,a.quantity::text,r.uom_code,a.updated_at::text
		FROM purchase_execution_assignments a
		JOIN purchase_execution_supplier_quotes q ON q.tenant_id=a.tenant_id AND q.id=a.quote_id AND q.requirement_id=a.requirement_id
		JOIN purchase_requirements r ON r.tenant_id=a.tenant_id AND r.id=a.requirement_id
		WHERE a.tenant_id=$1 AND a.requirement_id=ANY($2::bigint[]) AND `+executionRFQOpenSQL+`
		  AND a.po_id IS NULL AND NOT a.cancelled AND ($3 OR a.buyer_id=$4)
		ORDER BY a.requirement_id,a.id FOR UPDATE OF a`, tenant, requirements, canActForOthers, buyerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []ExecutionDraftLine
	for rows.Next() {
		var task ExecutionDraftLine
		if err := rows.Scan(&task.AssignmentID, &task.RequirementID, &task.QuoteID, &task.BuyerID, &task.SupplierID, &task.BuyerName,
			&task.SupplierName, &task.Currency, &task.ContractNo, &task.ProductName, &task.Specification, &task.Quantity, &task.UomCode, &task.UpdatedAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func applyExecutionDraftInputs(tasks []ExecutionDraftLine, drafts []ExecutionDraftInput) ([]ExecutionDraftLine, error) {
	changed := apierr.Conflict("ASSIGNMENT_BATCH_CHANGED", "待生成的议价任务已变化，请刷新实单后重试")
	if len(tasks) != len(drafts) {
		return nil, changed
	}
	byID := make(map[int64]ExecutionDraftInput, len(drafts))
	for _, draft := range drafts {
		byID[draft.AssignmentID] = draft
	}
	for index := range tasks {
		task := &tasks[index]
		draft, ok := byID[task.AssignmentID]
		if !ok {
			return nil, changed
		}
		price, err := executionQuantity(draft.Price, "请填写有效的议价单价")
		if err != nil {
			return nil, err
		}
		if _, err := time.Parse("2006-01-02", draft.Date); err != nil {
			return nil, apierr.Invalid("ASSIGNMENT_DATE_INVALID", "请填写有效的交货日期")
		}
		draft.Terms, draft.Remark = strings.TrimSpace(draft.Terms), strings.TrimSpace(draft.Remark)
		if draft.Terms == "" {
			return nil, apierr.Invalid("ASSIGNMENT_TERMS_REQUIRED", "请填写付款条件")
		}
		draft.Price = price.String()
		draft.FileIDs = append([]int64(nil), draft.FileIDs...)
		slices.Sort(draft.FileIDs)
		draft.FileIDs = slices.Compact(draft.FileIDs)
		task.ExecutionDraftInput = draft
	}
	return tasks, nil
}

func groupExecutionDraftLines(tasks []ExecutionDraftLine) []ExecutionDraftGroup {
	type groupKey struct {
		buyer, supplier                 int64
		currency, date, terms, contract string
	}
	var groups []ExecutionDraftGroup
	groupIndices := map[groupKey][]int{}
	for _, task := range tasks {
		key := groupKey{task.BuyerID, task.SupplierID, task.Currency, task.Date, task.Terms, task.ContractNo}
		index := -1
		for _, candidate := range groupIndices[key] {
			if !slices.ContainsFunc(groups[candidate].Lines, func(line ExecutionDraftLine) bool { return line.RequirementID == task.RequirementID }) {
				index = candidate
				break
			}
		}
		if index == -1 {
			index = len(groups)
			groupIndices[key] = append(groupIndices[key], index)
			groups = append(groups, ExecutionDraftGroup{BuyerID: task.BuyerID, SupplierID: task.SupplierID,
				BuyerName: task.BuyerName, SupplierName: task.SupplierName, Currency: task.Currency, Date: task.Date,
				Terms: task.Terms, ContractNo: task.ContractNo})
		}
		groups[index].Lines = append(groups[index].Lines, task)
	}
	return groups
}

func (s *Service) createExecutionDraftGroup(ctx context.Context, tx pgx.Tx, tenant int64, op Operator, group ExecutionDraftGroup) (int64, error) {
	lines := make([]OrderLine, 0, len(group.Lines))
	for _, task := range group.Lines {
		lines = append(lines, OrderLine{RequirementID: task.RequirementID, Qty: task.Quantity, UnitPrice: task.Price, ExecutionQuoteID: task.QuoteID})
	}
	prepared, err := s.prepareOrder(ctx, CreateOrderInput{
		SupplierID: group.SupplierID, Currency: group.Currency, ExpectedDate: group.Date,
		FulfillmentMode: "DIRECT_SHIP", DeliveryLocationType: "CUSTOM", DeliveryAddress: "按外销合同约定",
		PaymentTerms: &group.Terms, Lines: lines,
	}, true)
	if err != nil {
		return 0, err
	}
	prepared.fromExecutionAssignment = true
	head, err := s.createPreparedOrder(ctx, tx, tenant, prepared, Operator{ID: group.BuyerID, Name: group.BuyerName})
	if err != nil {
		return 0, err
	}
	for _, task := range group.Lines {
		if _, err := tx.Exec(ctx, `UPDATE purchase_execution_assignments SET po_id=$3,changed_by=$4,updated_at=now(),
			negotiated_price=$5::numeric,expected_date=$6::text::date,payment_terms=$7,remark=$8 WHERE tenant_id=$1 AND id=$2`,
			tenant, task.AssignmentID, head.ID, op.ID, task.Price, task.Date, task.Terms, task.Remark); err != nil {
			return 0, err
		}
		if len(task.FileIDs) == 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO purchase_order_draft_files(tenant_id,po_id,file_name,content_type,size_bytes,file_data,uploaded_by_id,uploaded_by_name)
			SELECT tenant_id,$3,file_name,content_type,octet_length(file_data),file_data,uploaded_by_id,uploaded_by_name
			FROM purchase_execution_assignment_files WHERE tenant_id=$1 AND assignment_id=$2 AND id=ANY($4::bigint[])`,
			tenant, task.AssignmentID, head.ID, task.FileIDs); err != nil {
			return 0, err
		}
	}
	return head.ID, nil
}
