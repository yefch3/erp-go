package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/shopspring/decimal"
)

type ExecutionAssignment struct {
	ID, RequirementID, QuoteID, BuyerID, OrderID, AssignedBy, ChangedBy     int64
	BuyerName, Quantity, Instructions, SupplierName, Currency, InitialPrice string
	NegotiatedPrice, ExpectedDate, PaymentTerms, Remark, OrderStatus        string
	ProductName, UomCode, ContractNo, BatchKey, Specification               string
	Files                                                                   []ExecutionAssignmentFile
}

type ExecutionAssignmentFile struct {
	ID                int64
	Name, ContentType string
}

type ExecutionAssignmentInput struct {
	ID, RequirementID, QuoteID, BuyerID, FileID                int64
	Action, Quantity, Instructions, Price, Date, Terms, Remark string
	FileName, ContentType                                      string
	FileData                                                   []byte
	FileIDs                                                    []int64
	BatchKey, DraftPreviewToken                                string
	DraftLines                                                 []ExecutionDraftInput
}

type ExecutionAssignmentResult struct {
	Assignments           []ExecutionAssignment
	ID, OrderID           int64
	FileName, ContentType string
	FileData              []byte
	DraftGroups           []ExecutionDraftGroup
	DraftPreviewToken     string
}

func (s *Service) CanSelectFactoryQuote(ctx context.Context, op Operator) (bool, error) {
	if s.permissions == nil {
		return false, nil
	}
	return s.permissions.Allowed(ctx, op.ID, "procurement:execution-quote:select")
}

func (s *Service) CanManageExecutionQuotes(ctx context.Context, op Operator) (bool, error) {
	if s.permissions == nil {
		return false, nil
	}
	return s.permissions.Allowed(ctx, op.ID, "procurement:sourcing:approve")
}

func (s *Service) AuthorizeExecutionQuote(ctx context.Context, tenantID, id int64, op Operator) error {
	manager, err := s.canManageAllFactoryQuotes(ctx, op)
	if err != nil || manager {
		return err
	}
	return s.authorizeOwnExecutionQuote(ctx, tenantID, id, op)
}

func (s *Service) AuthorizeExecutionQuoteSelection(ctx context.Context, tenantID, id int64, op Operator) error {
	allowed, err := s.CanSelectFactoryQuote(ctx, op)
	if err != nil {
		return err
	}
	if !allowed {
		return apierr.Permission("ASSIGNMENT_MANAGER_REQUIRED", "需要「选定工厂报价」权限")
	}
	all, err := s.CanViewAllFactoryQuotes(ctx, op)
	if err != nil || all {
		return err
	}
	return s.authorizeOwnExecutionQuote(ctx, tenantID, id, op)
}

func (s *Service) authorizeOwnExecutionQuote(ctx context.Context, tenantID, id int64, op Operator) error {
	var owner int64
	err := s.pool.QueryRow(ctx, `SELECT created_by_id FROM purchase_execution_supplier_quotes WHERE tenant_id=$1 AND id=$2`, tenantID, id).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner != op.ID) {
		return apierr.NotFound("EXECUTION_QUOTE_NOT_FOUND", "实单报价不存在")
	}
	return err
}

func (s *Service) ManageExecutionAssignment(ctx context.Context, tenant int64, op Operator, in ExecutionAssignmentInput) (ExecutionAssignmentResult, error) {
	if in.Action == "delete_batch" {
		return ExecutionAssignmentResult{}, s.deleteExecutionRFQBatch(ctx, tenant, op, in.BatchKey)
	}
	if in.RequirementID > 0 && in.Action != "inbox" {
		if err := s.AuthorizeRequirement(ctx, tenant, in.RequirementID, op); err != nil {
			return ExecutionAssignmentResult{}, err
		}
	}
	selecting := in.Action == "assign" || in.Action == "cancel"
	var selector, manager, allQuotes bool
	var err error
	if selecting || in.Action == "list" {
		selector, err = s.CanSelectFactoryQuote(ctx, op)
		if err != nil {
			return ExecutionAssignmentResult{}, err
		}
	}
	if selecting && !selector {
		return ExecutionAssignmentResult{}, apierr.Permission("ASSIGNMENT_MANAGER_REQUIRED", "需要「选定工厂报价」权限")
	}
	if !selecting && in.Action != "inbox" {
		manager, err = s.CanManageExecutionQuotes(ctx, op)
		if err != nil {
			return ExecutionAssignmentResult{}, err
		}
	}
	if manager || selector {
		allQuotes, err = s.CanViewAllFactoryQuotes(ctx, op)
		if err != nil {
			return ExecutionAssignmentResult{}, err
		}
	}
	canActForOthers := allQuotes && (manager || selector)
	switch in.Action {
	case "list", "inbox":
		if in.Action == "list" && in.RequirementID <= 0 {
			return ExecutionAssignmentResult{}, apierr.Invalid("ASSIGNMENT_REQUIREMENT_REQUIRED", "请选择采购需求")
		}
		rows, err := s.listExecutionAssignments(ctx, tenant, op.ID, in.RequirementID, canActForOthers && in.Action == "list")
		if selector && !manager {
			for i := range rows {
				if rows[i].BuyerID != op.ID {
					rows[i].NegotiatedPrice, rows[i].ExpectedDate, rows[i].PaymentTerms, rows[i].Remark = "", "", "", ""
					rows[i].Files = nil
				}
			}
		}
		return ExecutionAssignmentResult{Assignments: rows}, err
	case "assign", "cancel", "negotiate", "upload", "download", "draft", "preview_drafts", "draft_batch":
	default:
		return ExecutionAssignmentResult{}, apierr.Invalid("ASSIGNMENT_ACTION_INVALID", "无效的议价操作")
	}
	if !selecting && in.Action != "download" && !manager && s.permissions != nil {
		ok, err := s.permissions.Allowed(ctx, op.ID, "procurement:order:write")
		if err != nil {
			return ExecutionAssignmentResult{}, err
		}
		if !ok {
			return ExecutionAssignmentResult{}, apierr.Permission("ASSIGNMENT_WRITE_REQUIRED", "没有处理议价任务的权限")
		}
	}
	if in.Action == "preview_drafts" || in.Action == "draft_batch" {
		return s.manageExecutionDraftBatch(ctx, tenant, op, in, canActForOthers)
	}
	if in.RequirementID <= 0 {
		return ExecutionAssignmentResult{}, apierr.Invalid("ASSIGNMENT_REQUIREMENT_REQUIRED", "请选择采购需求")
	}
	var result ExecutionAssignmentResult
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if in.Action == "assign" || in.Action == "draft" {
			var eligible bool
			if err := tx.QueryRow(ctx, `SELECT `+executionRFQStateSQL+`
				FROM purchase_requirements r WHERE r.tenant_id=$1 AND r.id=$2 FOR UPDATE`, tenant, in.RequirementID).Scan(&eligible); err != nil {
				return err
			}
			if in.Action == "assign" && !eligible {
				return apierr.Conflict("ASSIGNMENT_REQUIREMENT_CLOSED", "该需求已停止实单询价")
			}
		}
		if in.Action == "assign" {
			id, err := s.assignExecutionQuote(ctx, tx, tenant, op, in, canActForOthers)
			result.ID = id
			return err
		}
		var task ExecutionAssignment
		var supplierID int64
		err := tx.QueryRow(ctx, `SELECT a.buyer_id,a.buyer_name,a.quote_id,a.quantity::text,coalesce(a.po_id,0),q.supplier_id,q.currency,
		    coalesce(a.negotiated_price::text,''),coalesce(a.expected_date::text,''),a.payment_terms,a.remark
		    FROM purchase_execution_assignments a JOIN purchase_execution_supplier_quotes q ON q.id=a.quote_id AND q.tenant_id=a.tenant_id
		    WHERE a.tenant_id=$1 AND a.requirement_id=$2 AND a.id=$3 AND NOT a.cancelled AND ($4 OR a.buyer_id=$5) FOR UPDATE OF a`,
			tenant, in.RequirementID, in.ID, canActForOthers, op.ID).Scan(&task.BuyerID, &task.BuyerName, &task.QuoteID, &task.Quantity, &task.OrderID, &supplierID,
			&task.Currency, &task.NegotiatedPrice, &task.ExpectedDate, &task.PaymentTerms, &task.Remark)
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("ASSIGNMENT_NOT_FOUND", "议价任务不存在")
		}
		if err != nil {
			return err
		}
		if in.Action == "download" {
			err = tx.QueryRow(ctx, `SELECT file_name,content_type,file_data FROM purchase_execution_assignment_files WHERE tenant_id=$1 AND assignment_id=$2 AND id=$3`,
				tenant, in.ID, in.FileID).Scan(&result.FileName, &result.ContentType, &result.FileData)
			if errors.Is(err, pgx.ErrNoRows) {
				return apierr.NotFound("ASSIGNMENT_FILE_NOT_FOUND", "资料不存在")
			}
			return err
		}
		if task.OrderID > 0 {
			if in.Action == "draft" {
				result.OrderID = task.OrderID
				return nil
			}
			return apierr.Conflict("ASSIGNMENT_DRAFT_EXISTS", "已生成采购草稿，请打开原草稿处理")
		}
		switch in.Action {
		case "cancel":
			_, err = tx.Exec(ctx, `UPDATE purchase_execution_assignments SET cancelled=true,changed_by=$3,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, in.ID, op.ID)
			return err
		case "negotiate":
			price, err := executionQuantity(in.Price, "请填写有效的议价单价")
			if err != nil {
				return err
			}
			if _, err := time.Parse("2006-01-02", in.Date); err != nil {
				return apierr.Invalid("ASSIGNMENT_DATE_INVALID", "请填写有效的交货日期")
			}
			if strings.TrimSpace(in.Terms) == "" {
				return apierr.Invalid("ASSIGNMENT_TERMS_REQUIRED", "请填写付款条件")
			}
			_, err = tx.Exec(ctx, `UPDATE purchase_execution_assignments SET negotiated_price=$3::numeric,expected_date=$4::text::date,payment_terms=$5,remark=$6,changed_by=$7,updated_at=now() WHERE tenant_id=$1 AND id=$2`,
				tenant, in.ID, price.String(), in.Date, strings.TrimSpace(in.Terms), strings.TrimSpace(in.Remark), op.ID)
			return err
		case "upload":
			if strings.TrimSpace(in.FileName) == "" || len(in.FileData) == 0 || len(in.FileData) > maxOrderDraftFile {
				return apierr.Invalid("ASSIGNMENT_FILE_INVALID", "请选择有效文件，单个文件不超过 10 MB")
			}
			contentType := in.ContentType
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			return tx.QueryRow(ctx, `INSERT INTO purchase_execution_assignment_files (tenant_id,assignment_id,file_name,content_type,file_data,uploaded_by_id,uploaded_by_name)
				VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`, tenant, in.ID, strings.TrimSpace(in.FileName), contentType, in.FileData, op.ID, op.Name).Scan(&result.ID)
		case "draft":
			id, err := s.createAssignmentDraft(ctx, tx, tenant, op, in, task, supplierID)
			result.OrderID = id
			return err
		}
		return nil
	})
	if err == nil && in.Action != "download" {
		s.nudge(ctx, tenant)
	}
	return result, err
}

func executionQuantity(value, message string) (decimal.Decimal, error) {
	n, err := decimal.NewFromString(strings.TrimSpace(value))
	if err != nil || !n.IsPositive() || !n.Equal(n.Truncate(6)) || n.GreaterThanOrEqual(decimal.New(1, 14)) {
		return decimal.Zero, apierr.Invalid("ASSIGNMENT_AMOUNT_INVALID", message)
	}
	return n, nil
}

func (s *Service) assignExecutionQuote(ctx context.Context, tx pgx.Tx, tenant int64, op Operator, in ExecutionAssignmentInput, allQuotes bool) (int64, error) {
	if in.QuoteID <= 0 {
		return 0, apierr.Invalid("ASSIGNMENT_INPUT_REQUIRED", "请选择工厂报价")
	}
	if s.people == nil {
		return 0, apierr.Internal("ASSIGNMENT_PEOPLE_UNAVAILABLE", "员工服务未配置")
	}
	var buyerID int64
	err := tx.QueryRow(ctx, `SELECT created_by_id FROM purchase_execution_supplier_quotes WHERE tenant_id=$1 AND requirement_id=$2 AND id=$3`, tenant, in.RequirementID, in.QuoteID).Scan(&buyerID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !allQuotes && buyerID != op.ID) {
		return 0, apierr.NotFound("EXECUTION_QUOTE_NOT_FOUND", "实单报价不存在")
	}
	if err != nil {
		return 0, err
	}
	if buyerID <= 0 {
		return 0, apierr.Invalid("ASSIGNMENT_BUYER_INVALID", "这条历史报价缺少录入人，请选用有负责采购员的报价")
	}
	person, err := s.people.Employee(ctx, buyerID)
	if err != nil {
		return 0, err
	}
	allowed, err := s.permissions.Allowed(ctx, buyerID, "procurement:order:write")
	if err != nil {
		return 0, err
	}
	if person.ID != buyerID || !allowed {
		return 0, apierr.Invalid("ASSIGNMENT_BUYER_INVALID", "该报价的录入人没有采购制单权限")
	}
	qty, err := unreservedExecutionQuantity(ctx, tx, tenant, in.RequirementID, 0)
	if err != nil {
		return 0, err
	}
	if !qty.IsPositive() {
		return 0, apierr.Conflict("ASSIGNMENT_EXCEEDS_REQUIREMENT", "该规格已分配或已采购，请刷新后查看")
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO purchase_execution_assignments(tenant_id,requirement_id,quote_id,buyer_id,buyer_name,quantity,instructions,assigned_by,changed_by)
		VALUES($1,$2,$3,$4,$5,$6::numeric,$7,$8,$8) RETURNING id`, tenant, in.RequirementID, in.QuoteID, buyerID, person.Name, qty.String(), strings.TrimSpace(in.Instructions), op.ID).Scan(&id)
	return id, err
}

func unreservedExecutionQuantity(ctx context.Context, tx pgx.Tx, tenantID, requirementID, excludeOrderID int64) (decimal.Decimal, error) {
	var remains string
	err := tx.QueryRow(ctx, `SELECT (contract_requirement_open($1,$2) -
		coalesce((SELECT sum(quantity) FROM purchase_execution_assignments WHERE tenant_id=$1 AND requirement_id=$2 AND NOT cancelled AND po_id IS NULL),0) -
		coalesce((SELECT sum(i.qty) FROM purchase_order_items i JOIN purchase_orders o ON o.tenant_id=i.tenant_id AND o.id=i.po_id
			WHERE i.tenant_id=$1 AND i.requirement_id=$2 AND o.id<>$3 AND o.status IN ('DRAFT','REJECTED','PENDING_APPROVAL')),0))::text`,
		tenantID, requirementID, excludeOrderID).Scan(&remains)
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromString(remains)
}

func (s *Service) createAssignmentDraft(ctx context.Context, tx pgx.Tx, tenant int64, op Operator, in ExecutionAssignmentInput, task ExecutionAssignment, supplierID int64) (int64, error) {
	if task.NegotiatedPrice == "" {
		return 0, apierr.Invalid("ASSIGNMENT_NEGOTIATION_REQUIRED", "先保存议价结果再生成草稿")
	}
	if err := checkExecutionDraftQuantity(ctx, tx, tenant, in.RequirementID, decimal.RequireFromString(task.Quantity)); err != nil {
		return 0, err
	}
	var selected int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM purchase_execution_assignment_files WHERE tenant_id=$1 AND assignment_id=$2 AND id=ANY($3::bigint[])`, tenant, in.ID, in.FileIDs).Scan(&selected)
	if err != nil {
		return 0, err
	}
	if selected != len(in.FileIDs) {
		return 0, apierr.Invalid("ASSIGNMENT_FILES_INVALID", "所选资料不属于此议价任务")
	}
	prepared, err := s.prepareOrder(ctx, CreateOrderInput{SupplierID: supplierID, Currency: task.Currency, ExpectedDate: task.ExpectedDate,
		FulfillmentMode: "DIRECT_SHIP", DeliveryLocationType: "CUSTOM", DeliveryAddress: "按外销合同约定",
		Lines: []OrderLine{{RequirementID: in.RequirementID, Qty: task.Quantity, UnitPrice: task.NegotiatedPrice, ExecutionQuoteID: task.QuoteID}}}, true)
	if err != nil {
		return 0, err
	}
	prepared.fromExecutionAssignment = true
	head, err := s.createPreparedOrder(ctx, tx, tenant, prepared, Operator{ID: task.BuyerID, Name: task.BuyerName})
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE purchase_orders SET payment_terms=$3 WHERE tenant_id=$1 AND id=$2`, tenant, head.ID, task.PaymentTerms); err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `UPDATE purchase_execution_assignments SET po_id=$3,changed_by=$4,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, in.ID, head.ID, op.ID)
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO purchase_order_draft_files(tenant_id,po_id,file_name,content_type,size_bytes,file_data,uploaded_by_id,uploaded_by_name)
		SELECT tenant_id,$3,file_name,content_type,octet_length(file_data),file_data,uploaded_by_id,uploaded_by_name
		FROM purchase_execution_assignment_files WHERE tenant_id=$1 AND assignment_id=$2 AND id=ANY($4::bigint[])`, tenant, in.ID, head.ID, in.FileIDs)
	return head.ID, err
}

// The requirement row is locked by the caller. Reserve the existing drafts
// here before converting the current assignments into new draft lines.
func checkExecutionDraftQuantity(ctx context.Context, tx pgx.Tx, tenant, requirementID int64, qty decimal.Decimal) error {
	var remaining string
	err := tx.QueryRow(ctx, `SELECT (CASE WHEN `+executionRFQStateSQL+` THEN contract_requirement_open(r.tenant_id,r.id) ELSE 0 END -
		coalesce((SELECT sum(i.qty) FROM purchase_order_items i JOIN purchase_orders o ON o.tenant_id=i.tenant_id AND o.id=i.po_id
		WHERE i.tenant_id=r.tenant_id AND i.requirement_id=r.id AND o.status IN ('DRAFT','REJECTED','PENDING_APPROVAL')),0))::text
		FROM purchase_requirements r WHERE r.tenant_id=$1 AND r.id=$2`, tenant, requirementID).Scan(&remaining)
	if err != nil {
		return err
	}
	if qty.GreaterThan(decimal.RequireFromString(remaining)) {
		return apierr.Conflict("ASSIGNMENT_QUANTITY_CHANGED", "剩余待采购量已变化，请领导重新确认选厂任务")
	}
	return nil
}

func (s *Service) OrderFromExecutionNegotiation(ctx context.Context, tenantID, orderID int64) (bool, error) {
	var linked bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM purchase_execution_assignments WHERE tenant_id=$1 AND po_id=$2
	)`, tenantID, orderID).Scan(&linked)
	return linked, err
}

func (s *Service) listExecutionAssignments(ctx context.Context, tenant, buyer, requirement int64, manager bool) ([]ExecutionAssignment, error) {
	visible, err := s.visibleRequirementsTo(ctx, Operator{ID: buyer})
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT a.id,a.requirement_id,a.quote_id,a.buyer_id,a.buyer_name,a.quantity::text,a.instructions,q.supplier_name,q.currency,q.unit_price::text,
		coalesce(a.negotiated_price::text,''),coalesce(a.expected_date::text,''),a.payment_terms,a.remark,coalesce(a.po_id,0),coalesce(o.status,''),
		r.product_name,r.uom_code,r.contract_no,CASE WHEN r.contract_id<>0 THEN 'CONTRACT-ID:' || r.contract_id WHEN btrim(r.contract_no)<>'' THEN 'CONTRACT-NO:' || btrim(r.contract_no)
		WHEN r.quotation_id<>0 THEN 'QUOTATION-ID:' || r.quotation_id WHEN btrim(r.quotation_no)<>'' THEN 'QUOTATION-NO:' || btrim(r.quotation_no) ELSE 'MANUAL:' || r.id END,
		a.assigned_by,a.changed_by,coalesce((SELECT json_agg(json_build_object('ID',f.id,'Name',f.file_name,'ContentType',f.content_type) ORDER BY f.id)
		FROM purchase_execution_assignment_files f WHERE f.tenant_id=a.tenant_id AND f.assignment_id=a.id),'[]'),r.spec
		FROM purchase_execution_assignments a JOIN purchase_requirements r ON r.tenant_id=a.tenant_id AND r.id=a.requirement_id
		JOIN purchase_execution_supplier_quotes q ON q.tenant_id=a.tenant_id AND q.id=a.quote_id
		LEFT JOIN purchase_orders o ON o.tenant_id=a.tenant_id AND o.id=a.po_id
		WHERE a.tenant_id=$1 AND NOT a.cancelled AND ($2 OR a.buyer_id=$3)
		AND (($4::bigint=0 AND (o.status='REJECTED' OR (a.po_id IS NULL AND `+executionRFQOpenSQL+`))) OR a.requirement_id=$4)
		AND ($5 OR r.owner_id=ANY($6::bigint[])) ORDER BY a.id`, tenant, manager, buyer, requirement, visible.All, visible.EmployeeIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ExecutionAssignment
	for rows.Next() {
		var a ExecutionAssignment
		var files []byte
		if err := rows.Scan(&a.ID, &a.RequirementID, &a.QuoteID, &a.BuyerID, &a.BuyerName, &a.Quantity, &a.Instructions, &a.SupplierName, &a.Currency, &a.InitialPrice,
			&a.NegotiatedPrice, &a.ExpectedDate, &a.PaymentTerms, &a.Remark, &a.OrderID, &a.OrderStatus, &a.ProductName, &a.UomCode, &a.ContractNo, &a.BatchKey, &a.AssignedBy, &a.ChangedBy, &files, &a.Specification); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(files, &a.Files); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
