package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

const orderImportLifetime = 30 * time.Minute

type OrderImportRow struct {
	RowNo            int32  `json:"rowNo"`
	Product          string `json:"product"`
	MaterialStandard string `json:"materialStandard"`
	Grade            string `json:"grade"`
	Thickness        string `json:"thickness"`
	Width            string `json:"width"`
	QuantityUnit     string `json:"quantityUnit"`
	Quantity         string `json:"quantity"`
	UnitPrice        string `json:"unitPrice"`
}

type PreviewOrderImportInput struct {
	SourceType         string
	SourceMailID       int64
	SourceAttachmentID int64
	SourceFileName     string
	FileSHA256         string
	Rows               []OrderImportRow
}

type OrderImportCandidate struct {
	RequirementID int64
	ContractNo    string
	ProductName   string
	ProductCode   string
	Spec          string
	UomCode       string
	RequiredQty   string
	OrderedQty    string
	OpenQty       string
	Status        string
}

type OrderImportPreviewRow struct {
	RowNo                  int32
	Product                string
	Quantity               string
	QuantityUnit           string
	UnitPrice              string
	Candidates             []OrderImportCandidate
	Result                 string
	Message                string
	SuggestedRequirementID int64
}

type PreviewOrderImportResult struct {
	ImportToken string
	ExpiresAt   time.Time
	Rows        []OrderImportPreviewRow
}

type ConfirmOrderImportLine struct {
	RowNo         int32
	RequirementID int64
	Qty           string
	UnitPrice     string
}

type ConfirmOrderImportInput struct {
	ImportToken  string
	SupplierID   int64
	Currency     string
	ExpectedDate string
	Remark       string
	Lines        []ConfirmOrderImportLine
}

type ConfirmOrderImportResult struct {
	ID             int64
	PONo           string
	Status         string
	AlreadyCreated bool
}

func newImportToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Service) PreviewOrderImport(ctx context.Context, tenantID int64, in PreviewOrderImportInput, op Operator) (PreviewOrderImportResult, error) {
	if len(in.Rows) == 0 {
		return PreviewOrderImportResult{}, apierr.Invalid("PO_IMPORT_LINES_REQUIRED", "Excel 至少需要一条产品明细")
	}
	if len(in.Rows) > 200 {
		return PreviewOrderImportResult{}, apierr.Invalid("PO_IMPORT_TOO_LARGE", "预览最多处理 200 行")
	}
	if in.SourceType != "MAIL_EXCEL" && in.SourceType != "UPLOAD" {
		return PreviewOrderImportResult{}, apierr.Invalid("PO_IMPORT_SOURCE_INVALID", "采购导入来源无效")
	}

	seenRows := make(map[int32]bool, len(in.Rows))
	rows := make([]OrderImportPreviewRow, 0, len(in.Rows))
	for _, raw := range in.Rows {
		row := OrderImportPreviewRow{
			RowNo: raw.RowNo, Product: strings.TrimSpace(raw.Product),
			Quantity: strings.TrimSpace(raw.Quantity), QuantityUnit: strings.TrimSpace(raw.QuantityUnit),
			UnitPrice: strings.TrimSpace(raw.UnitPrice), Result: "NOT_FOUND",
			Candidates: []OrderImportCandidate{},
		}
		if raw.RowNo <= 0 || seenRows[raw.RowNo] {
			row.Result, row.Message = "DUPLICATED", "Excel 行号重复"
			rows = append(rows, row)
			continue
		}
		seenRows[raw.RowNo] = true
		if row.Product == "" {
			row.Message = "产品不能为空"
			rows = append(rows, row)
			continue
		}
		qty, qtyErr := decimal.NewFromString(row.Quantity)
		if qtyErr != nil || qty.LessThanOrEqual(decimal.Zero) {
			row.Result, row.Message = "QTY_EXCEEDED", "数量必须大于 0"
			rows = append(rows, row)
			continue
		}
		if row.QuantityUnit == "" {
			row.Result, row.Message = "UNIT_MISMATCH", "单位不能为空"
			rows = append(rows, row)
			continue
		}
		price, priceErr := decimal.NewFromString(orZero(row.UnitPrice))
		if priceErr != nil || price.IsNegative() {
			row.Result, row.Message = "PRICE_INVALID", "单价必须为非负数"
			rows = append(rows, row)
			continue
		}

		candidates, err := s.q.ImportRequirementCandidates(ctx, store.ImportRequirementCandidatesParams{
			TenantID: tenantID, Keyword: row.Product,
		})
		if err != nil {
			return PreviewOrderImportResult{}, err
		}
		for _, candidate := range candidates {
			row.Candidates = append(row.Candidates, OrderImportCandidate{
				RequirementID: candidate.ID, ContractNo: candidate.ContractNo,
				ProductName: candidate.ProductName, ProductCode: candidate.ProductCode,
				Spec: candidate.Spec, UomCode: candidate.UomCode,
				RequiredQty: candidate.RequiredQty, OrderedQty: candidate.OrderedQty,
				OpenQty: candidate.OpenQty, Status: candidate.Status,
			})
		}
		switch len(row.Candidates) {
		case 0:
			row.Message = "没有找到待采购需求"
		case 1:
			candidate := row.Candidates[0]
			row.SuggestedRequirementID = candidate.RequirementID
			open, err := decimal.NewFromString(candidate.OpenQty)
			if err != nil {
				return PreviewOrderImportResult{}, err
			}
			switch {
			case !strings.EqualFold(row.QuantityUnit, strings.TrimSpace(candidate.UomCode)):
				row.Result, row.Message = "UNIT_MISMATCH", "Excel 单位与采购需求不一致"
			case qty.GreaterThan(open):
				row.Result, row.Message = "QTY_EXCEEDED", "数量超过采购需求未下单数量"
			default:
				row.Result = "MATCHED"
			}
		default:
			row.Result, row.Message = "MULTIPLE", "找到多个候选，请人工确认"
		}
		rows = append(rows, row)
	}

	// Two uniquely matched rows pointing at the same requirement must be
	// resolved before confirmation; otherwise the second silently changes the
	// meaning of the first.
	byRequirement := make(map[int64][]int)
	for i, row := range rows {
		if row.SuggestedRequirementID != 0 {
			byRequirement[row.SuggestedRequirementID] = append(byRequirement[row.SuggestedRequirementID], i)
		}
	}
	for _, indexes := range byRequirement {
		if len(indexes) < 2 {
			continue
		}
		for _, index := range indexes {
			rows[index].Result, rows[index].Message = "DUPLICATED", "同一采购需求在本次导入中重复出现"
		}
	}

	token, err := newImportToken()
	if err != nil {
		return PreviewOrderImportResult{}, err
	}
	encoded, err := json.Marshal(in.Rows)
	if err != nil {
		return PreviewOrderImportResult{}, err
	}
	expires := time.Now().UTC().Add(orderImportLifetime)
	created, err := s.q.CreatePurchaseOrderImport(ctx, store.CreatePurchaseOrderImportParams{
		TenantID: tenantID, ImportToken: token, SourceType: in.SourceType,
		SourceMailID: in.SourceMailID, SourceAttachmentID: in.SourceAttachmentID,
		SourceFileName: strings.TrimSpace(in.SourceFileName), FileSha256: strings.ToLower(strings.TrimSpace(in.FileSHA256)),
		PreviewRows: encoded, CreatedBy: op.ID,
		ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true},
	})
	if err != nil {
		return PreviewOrderImportResult{}, err
	}
	return PreviewOrderImportResult{ImportToken: created.ImportToken, ExpiresAt: expires, Rows: rows}, nil
}

func (s *Service) ConfirmOrderImport(ctx context.Context, tenantID int64, in ConfirmOrderImportInput, op Operator) (ConfirmOrderImportResult, error) {
	if strings.TrimSpace(in.ImportToken) == "" {
		return ConfirmOrderImportResult{}, apierr.Invalid("PO_IMPORT_TOKEN_REQUIRED", "导入预览凭证不能为空")
	}
	var result ConfirmOrderImportResult
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		session, err := q.LockPurchaseOrderImport(ctx, store.LockPurchaseOrderImportParams{
			TenantID: tenantID, ImportToken: strings.TrimSpace(in.ImportToken),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("PO_IMPORT_NOT_FOUND", "采购导入预览不存在")
		}
		if err != nil {
			return err
		}
		if session.CreatedBy != op.ID {
			return apierr.NotFound("PO_IMPORT_NOT_FOUND", "采购导入预览不存在")
		}
		if session.Status == "CREATED" && session.PurchaseOrderID != 0 {
			order, err := q.PurchaseOrderImportResult(ctx, store.PurchaseOrderImportResultParams{
				TenantID: tenantID, ID: session.PurchaseOrderID,
			})
			if err != nil {
				return err
			}
			result = ConfirmOrderImportResult{ID: order.ID, PONo: order.PoNo, Status: order.Status, AlreadyCreated: true}
			return nil
		}
		if session.Status != "PREVIEWED" || !session.ExpiresAt.Valid || time.Now().After(session.ExpiresAt.Time) {
			return apierr.Invalid("PO_IMPORT_EXPIRED", "导入预览已过期，请重新导入")
		}

		var rawRows []OrderImportRow
		if err := json.Unmarshal(session.PreviewRows, &rawRows); err != nil {
			return err
		}
		if len(in.Lines) != len(rawRows) {
			return apierr.Invalid("PO_IMPORT_REQUIREMENT_REQUIRED", "每一行都必须匹配采购需求")
		}
		rawByNo := make(map[int32]OrderImportRow, len(rawRows))
		for _, row := range rawRows {
			rawByNo[row.RowNo] = row
		}
		seenRows := make(map[int32]bool, len(in.Lines))
		seenRequirements := make(map[int64]bool, len(in.Lines))
		orderLines := make([]OrderLine, 0, len(in.Lines))
		for _, line := range in.Lines {
			raw, ok := rawByNo[line.RowNo]
			if !ok || seenRows[line.RowNo] || line.RequirementID == 0 {
				return apierr.Invalid("PO_IMPORT_REQUIREMENT_REQUIRED", "每一行都必须匹配采购需求")
			}
			seenRows[line.RowNo] = true
			if seenRequirements[line.RequirementID] {
				return apierr.Invalid("PO_IMPORT_DUPLICATED", "同一采购需求在本次导入中重复出现")
			}
			seenRequirements[line.RequirementID] = true

			candidates, err := q.ImportRequirementCandidates(ctx, store.ImportRequirementCandidatesParams{
				TenantID: tenantID, Keyword: strings.TrimSpace(raw.Product),
			})
			if err != nil {
				return err
			}
			candidateFound := false
			for _, candidate := range candidates {
				if candidate.ID == line.RequirementID {
					candidateFound = true
					break
				}
			}
			if !candidateFound {
				return apierr.Invalid("PO_IMPORT_REQUIREMENT_REQUIRED", "选择的采购需求不再可用，请重新预览")
			}
			orderLines = append(orderLines, OrderLine{
				RequirementID: line.RequirementID, Qty: line.Qty,
				UnitPrice: line.UnitPrice, UomCode: raw.QuantityUnit,
			})
		}

		prepared, err := s.prepareOrder(ctx, CreateOrderInput{
			SupplierID: in.SupplierID, Currency: strings.ToUpper(strings.TrimSpace(in.Currency)),
			ExpectedDate: strings.TrimSpace(in.ExpectedDate), Remark: strings.TrimSpace(in.Remark), Lines: orderLines,
		})
		if err != nil {
			return err
		}
		head, err := s.createPreparedOrder(ctx, tx, tenantID, prepared, op)
		if err != nil {
			return err
		}
		id := head.ID
		if err := q.MarkPurchaseOrderImportCreated(ctx, store.MarkPurchaseOrderImportCreatedParams{
			PurchaseOrderID: &id, TenantID: tenantID, ID: session.ID,
		}); err != nil {
			return err
		}
		result = ConfirmOrderImportResult{ID: head.ID, PONo: head.PoNo, Status: head.Status}
		return nil
	})
	if err != nil {
		return ConfirmOrderImportResult{}, err
	}
	s.nudge(ctx, tenantID)
	return result, nil
}
