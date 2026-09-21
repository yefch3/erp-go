package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"

	"github.com/shopspring/decimal"
)

// InquiryAccess is deliberately checked in the service as well as HTTP.
// An absent IAM dependency fails closed, including for direct gRPC callers.
type InquiryAccess interface {
	HasPermission(context.Context, int64, string) (bool, error)
}
type InquiryDocuments interface {
	SetAvailability(context.Context, int64, bool) error
}

type InquiryProduct struct {
	PackageQuantity string            `json:"packageQuantity"`
	ID              string            `json:"id"`
	Product         string            `json:"product"`
	Specification   string            `json:"specification"`
	Quantity        string            `json:"quantity"`
	Unit            string            `json:"unit"`
	Delivery        string            `json:"delivery"`
	Weight          string            `json:"weight"`
	Volume          string            `json:"volume"`
	Packaging       string            `json:"packaging"`
	Remark          string            `json:"remark"`
	CustomFields    map[string]string `json:"customFields"`
}
type InquiryTemplateFieldSnapshot struct {
	FieldKey     string `json:"fieldKey"`
	DisplayName  string `json:"displayName"`
	SortOrder    int32  `json:"sortOrder"`
	IsRequired   bool   `json:"isRequired"`
	DefaultValue string `json:"defaultValue"`
	DataType     string `json:"dataType"`
	IsCustom     bool   `json:"isCustom"`
	IsCore       bool   `json:"isCore"`
}
type InquiryTemplateSnapshot struct {
	ID           string                         `json:"id"`
	TemplateCode string                         `json:"templateCode"`
	Version      int32                          `json:"version"`
	Name         string                         `json:"name"`
	Fields       []InquiryTemplateFieldSnapshot `json:"fields"`
}
type InquiryBody struct {
	Title           string                   `json:"title"`
	CustomerID      string                   `json:"customerId"`
	Customer        string                   `json:"customer"`
	ContactID       string                   `json:"contactId"`
	Contact         string                   `json:"contact"`
	Delivery        string                   `json:"delivery"`
	LoadingPort     string                   `json:"loadingPort"`
	DestinationPort string                   `json:"destinationPort"`
	Incoterm        string                   `json:"incoterm"`
	Remark          string                   `json:"remark"`
	Template        *InquiryTemplateSnapshot `json:"template,omitempty"`
	Products        []InquiryProduct         `json:"products"`
	Attachments     []InquiryAttachment      `json:"attachments"`
}
type InquiryAttachment struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}
type InquiryPrice struct {
	ProductID    string `json:"productId"`
	Price        string `json:"price"`
	FactoryPrice string `json:"factoryPrice"`
	FOBPrice     string `json:"fobPrice"`
	Slitting     string `json:"slitting"`
	Delivery     string `json:"delivery"`
	Remark       string `json:"remark"`
}
type InquiryCharge struct {
	Name           string `json:"name"`
	Amount         string `json:"amount"`
	Currency       string `json:"currency"`
	Unit           string `json:"unit"`
	Quantity       string `json:"quantity"`
	Subtotal       string `json:"subtotal"`
	Remark         string `json:"remark"`
	AllocationType string `json:"allocationType"`
	ProductID      string `json:"productId"`
	USDSubtotal    string `json:"usdSubtotal"`
}
type InquiryFreightRate struct {
	ProductID string `json:"productId"`
	Price     string `json:"price"`
	Currency  string `json:"currency"`
	Unit      string `json:"unit"`
	Remark    string `json:"remark"`
	USDPrice  string `json:"usdPrice"`
}
type InquiryQuoteBody struct {
	Company         string               `json:"company"`
	Currency        string               `json:"currency"`
	QuoteCategory   string               `json:"quoteCategory"`
	Delivery        string               `json:"delivery"`
	ValidUntil      string               `json:"validUntil"`
	PaymentTerms    string               `json:"paymentTerms"`
	Incoterm        string               `json:"incoterm"`
	Remark          string               `json:"remark"`
	Prices          []InquiryPrice       `json:"prices"`
	Carrier         string               `json:"carrier"`
	Route           string               `json:"route"`
	Vessel          string               `json:"vessel"`
	Voyage          string               `json:"voyage"`
	Departure       string               `json:"departure"`
	Arrival         string               `json:"arrival"`
	TransitDays     string               `json:"transitDays"`
	LoadingPort     string               `json:"loadingPort"`
	DestinationPort string               `json:"destinationPort"`
	CargoIDs        []string             `json:"cargoIds"`
	FreightRates    []InquiryFreightRate `json:"freightRates"`
	Charges         []InquiryCharge      `json:"charges"`
	ExchangeRates   map[string]string    `json:"exchangeRates"`
	Totals          map[string]string    `json:"totals"`
	TotalUSD        string               `json:"totalUsd"`
	OtherChargesUSD string               `json:"otherChargesTotalUsd"`
	Attachments     []InquiryAttachment  `json:"attachments"`
}
type InquiryQuoteView struct {
	Historical  bool             `json:"historical"`
	ID          string           `json:"id"`
	Kind        string           `json:"kind"`
	Version     int64            `json:"version"`
	Body        InquiryQuoteBody `json:"body"`
	AuthorID    string           `json:"authorId"`
	Author      string           `json:"author"`
	SubmittedAt string           `json:"submittedAt"`
	UpdatedBy   string           `json:"updatedBy"`
	UpdatedAt   string           `json:"updatedAt"`
	CanEdit     bool             `json:"canEdit"`
}
type InquiryView struct {
	ID               string             `json:"id"`
	Number           string             `json:"number"`
	DisplayInquiryNo string             `json:"displayInquiryNo"`
	OwnerID          string             `json:"ownerId"`
	Owner            string             `json:"owner"`
	State            string             `json:"state"`
	Revision         int64              `json:"revision"`
	SubmittedAt      string             `json:"submittedAt"`
	Body             InquiryBody        `json:"body"`
	Quotes           []InquiryQuoteView `json:"quotes"`
	ProcurementCount int                `json:"procurementCount"`
	LogisticsCount   int                `json:"logisticsCount"`
	CanEdit          bool               `json:"canEdit"`
	CanDelete        bool               `json:"canDelete"`
	ReadOnlyReason   string             `json:"readOnlyReason,omitempty"`
	CanWithdraw      bool               `json:"canWithdraw"`
	WithdrawReason   string             `json:"withdrawReason,omitempty"`
	SourceMailID     string             `json:"sourceMailId"`
	Legacy           bool               `json:"legacy"`
}
type InquiryCommand struct {
	FileData     []byte           `json:"fileData"`
	FileName     string           `json:"fileName"`
	FileKey      string           `json:"fileKey"`
	Action       string           `json:"action"`
	View         string           `json:"view"`
	ID           string           `json:"id"`
	Revision     int64            `json:"revision"`
	QuoteID      string           `json:"quoteId"`
	QuoteVersion int64            `json:"quoteVersion"`
	Submit       bool             `json:"submit"`
	Body         InquiryBody      `json:"body"`
	Quote        InquiryQuoteBody `json:"quote"`
	Keyword      string           `json:"keyword"`
	State        string           `json:"state"`
	Page         int              `json:"page"`
	Size         int              `json:"size"`
}
type InquiryResult struct {
	FileData   []byte             `json:"fileData,omitempty"`
	FileName   string             `json:"fileName,omitempty"`
	Attachment *InquiryAttachment `json:"attachment,omitempty"`
	URL        string             `json:"url,omitempty"`
	Items      []InquiryView      `json:"items"`
	Total      int                `json:"total"`
	Item       *InquiryView       `json:"item,omitempty"`
}

func inquiryID(v string) int64 { n, _ := strconv.ParseInt(v, 10, 64); return n }
func inquiryLookupID(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "-") {
		positive := strings.TrimPrefix(v, "-")
		if inquiryID(positive) > 0 {
			return positive
		}
	}
	return v
}
func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
func inquiryPermission(view string, write bool) string {
	suffix := "read"
	if write {
		suffix = "write"
	}
	switch view {
	case "SALES", "QUOTATIONS":
		return "sales:inquiry:" + suffix
	case "PROCUREMENT":
		return "procurement:sourcing:" + suffix
	case "LOGISTICS":
		return "shipping:sourcing:" + suffix
	}
	return ""
}
func (s *Service) inquiryAllowed(ctx context.Context, op Operator, view string, write bool) error {
	access, ok := s.scopes.(InquiryAccess)
	code := inquiryPermission(view, write)
	if !ok || code == "" || op.ID <= 0 {
		return apierr.Permission("INQUIRY_PERMISSION", "没有此业务权限")
	}
	allowed, err := access.HasPermission(ctx, op.ID, code)
	if err != nil {
		return err
	}
	if !allowed {
		return apierr.Permission("INQUIRY_PERMISSION", "没有此业务权限")
	}
	return nil
}

func (s *Service) inquiryDeleteAllowed(ctx context.Context, op Operator) error {
	access, ok := s.scopes.(InquiryAccess)
	if !ok || op.ID <= 0 {
		return apierr.Permission("INQUIRY_DELETE_PERMISSION", "没有删除询盘权限")
	}
	allowed, err := access.HasPermission(ctx, op.ID, "sales:inquiry:delete")
	if err != nil {
		return err
	}
	if !allowed {
		return apierr.Permission("INQUIRY_DELETE_PERMISSION", "没有删除询盘权限")
	}
	return nil
}
func inquiryState(status, handoff string) string {
	if handoff == "SALES_WITHDRAWN" {
		return "WITHDRAWN"
	}
	if status == "INTAKE_PENDING" {
		return "UNSUBMITTED"
	}
	return "INQUIRING"
}

func inquiryTemplateSnapshot(view InquiryTemplateView) *InquiryTemplateSnapshot {
	fields := make([]InquiryTemplateFieldSnapshot, 0, len(view.Fields))
	for _, field := range view.Fields {
		fields = append(fields, InquiryTemplateFieldSnapshot{
			FieldKey: field.FieldKey, DisplayName: field.DisplayName, SortOrder: field.SortOrder,
			IsRequired: field.IsRequired, DefaultValue: field.DefaultValue, DataType: field.DataType,
			IsCustom: field.IsCustom, IsCore: IsCoreInquiryField(field.FieldKey),
		})
	}
	return &InquiryTemplateSnapshot{ID: strconv.FormatInt(view.Template.ID, 10), TemplateCode: view.Template.TemplateCode, Version: view.Template.Version, Name: view.Template.Name, Fields: fields}
}

func inquiryProductValue(product InquiryProduct, key string) string {
	switch key {
	case "product":
		return product.Product
	case "quantity":
		return product.Quantity
	case "quantity_unit":
		return product.Unit
	case "delivery":
		return product.Delivery
	case "packaging":
		return product.Packaging
	case "remarks":
		return product.Remark
	case "specification":
		return product.Specification
	case "weight":
		return product.Weight
	case "volume":
		return product.Volume
	case "package_quantity":
		return product.PackageQuantity
	default:
		return product.CustomFields[key]
	}
}

func setInquiryProductValue(product *InquiryProduct, key, value string) {
	switch key {
	case "product":
		product.Product = value
	case "quantity":
		product.Quantity = value
	case "quantity_unit":
		product.Unit = value
	case "delivery":
		product.Delivery = value
	case "packaging":
		product.Packaging = value
	case "remarks":
		product.Remark = value
	case "specification":
		product.Specification = value
	case "weight":
		product.Weight = value
	case "volume":
		product.Volume = value
	case "package_quantity":
		product.PackageQuantity = value
	default:
		if product.CustomFields == nil {
			product.CustomFields = map[string]string{}
		}
		product.CustomFields[key] = value
	}
}

func applyInquiryTemplateDefaults(body *InquiryBody) {
	if body.Template == nil {
		return
	}
	for i := range body.Products {
		if body.Products[i].CustomFields == nil {
			body.Products[i].CustomFields = map[string]string{}
		}
		for _, field := range body.Template.Fields {
			if strings.TrimSpace(inquiryProductValue(body.Products[i], field.FieldKey)) == "" && field.DefaultValue != "" {
				setInquiryProductValue(&body.Products[i], field.FieldKey, field.DefaultValue)
			}
		}
		specification := compactStrings([]string{
			body.Products[i].CustomFields["material_standard"], body.Products[i].CustomFields["grade"],
			body.Products[i].CustomFields["thickness"], body.Products[i].CustomFields["width"],
			body.Products[i].CustomFields["length_or_form"], body.Products[i].CustomFields["surface_requirement"],
		})
		if len(specification) > 0 {
			body.Products[i].Specification = strings.Join(specification, " · ")
		}
	}
}

func (s *Service) prepareInquiryTemplate(ctx context.Context, tenant int64, body *InquiryBody) error {
	var view InquiryTemplateView
	var err error
	if body.Template == nil || inquiryID(body.Template.ID) <= 0 {
		view, err = s.GetDefaultInquiryTemplate(ctx, tenant)
	} else {
		view, err = s.GetInquiryTemplate(ctx, tenant, inquiryID(body.Template.ID))
		if err == nil && ((body.Template.TemplateCode != "" && body.Template.TemplateCode != view.Template.TemplateCode) || (body.Template.Version > 0 && body.Template.Version != view.Template.Version)) {
			return apierr.Conflict("INQUIRY_TEMPLATE_CHANGED", "询盘模板版本不一致，请重新选择模板")
		}
	}
	if err != nil {
		return err
	}
	body.Template = inquiryTemplateSnapshot(view)
	applyInquiryTemplateDefaults(body)
	return nil
}

func (s *Service) InquiryWorkspace(ctx context.Context, tenant int64, op Operator, in InquiryCommand) (InquiryResult, error) {
	in.View = strings.ToUpper(in.View)
	// The retired inquiry page used negative route IDs to distinguish its
	// synthetic detail links from newer records. Those links are still present
	// in bookmarks and browser history. Normalize them only for read operations;
	// every write continues to require the canonical positive ID returned by the
	// current workspace.
	if in.Action == "get" || in.Action == "download" || in.Action == "readFile" {
		in.ID = inquiryLookupID(in.ID)
	}
	if in.View == "AUTO" && (in.Action == "download" || in.Action == "readFile") {
		for _, v := range []string{"SALES", "PROCUREMENT", "LOGISTICS"} {
			if s.inquiryAllowed(ctx, op, v, false) == nil {
				if _, err := s.readInquiry(ctx, tenant, inquiryID(in.ID), op, v); err == nil {
					in.View = v
					break
				}
			}
		}
	}
	if (in.ID != "" && inquiryID(in.ID) <= 0) || (in.QuoteID != "" && inquiryID(in.QuoteID) <= 0) {
		return InquiryResult{}, apierr.Invalid("INQUIRY_ID", "编号无效")
	}
	write := in.Action != "list" && in.Action != "get" && in.Action != "download" && in.Action != "readFile" && in.Action != "delete"
	if err := s.inquiryAllowed(ctx, op, in.View, write); err != nil {
		return InquiryResult{}, err
	}
	switch in.Action {
	case "upload":
		return s.inquiryUpload(ctx, tenant, op, in)
	case "download", "readFile":
		return s.inquiryDownload(ctx, tenant, op, in)
	case "list":
		return s.listInquiryWorkspace(ctx, tenant, op, in)
	case "get":
		v, err := s.readInquiry(ctx, tenant, inquiryID(in.ID), op, in.View)
		return InquiryResult{Item: v}, err
	case "delete":
		if in.View != "SALES" {
			return InquiryResult{}, apierr.Permission("INQUIRY_DELETE_PERMISSION", "仅可从客户询盘删除")
		}
		return s.deleteInquiry(ctx, tenant, op, in)
	case "save":
		if in.View != "SALES" {
			return InquiryResult{}, apierr.Permission("INQUIRY_OWNER_ONLY", "仅负责销售可以编辑询盘")
		}
		return s.saveInquiry(ctx, tenant, op, in)
	case "updateBasic":
		if in.View != "SALES" {
			return InquiryResult{}, apierr.Permission("INQUIRY_OWNER_ONLY", "仅负责销售可以编辑询盘")
		}
		return s.updateInquiryBasic(ctx, tenant, op, in)
	case "submit", "withdraw":
		if in.View != "SALES" {
			return InquiryResult{}, apierr.Permission("INQUIRY_OWNER_ONLY", "仅负责销售可以处理询盘")
		}
		return s.changeInquiry(ctx, tenant, op, in)
	case "quote":
		if in.View != "PROCUREMENT" && in.View != "LOGISTICS" {
			return InquiryResult{}, apierr.Permission("INQUIRY_QUOTE_READ_ONLY", "销售只能查看报价")
		}
		return s.saveInquiryQuote(ctx, tenant, op, in)
	default:
		return InquiryResult{}, apierr.Invalid("INQUIRY_COMMAND", "无效操作")
	}
}

func (s *Service) readInquiry(ctx context.Context, tenant, id int64, op Operator, view string) (*InquiryView, error) {
	var v InquiryView
	var raw []byte
	var status, handoff string
	var owner int64
	var source, templateID int64
	err := s.pool.QueryRow(ctx, `SELECT id::text,case_no,display_inquiry_no,owner_id,owner_name,status,handoff_status,inquiry_revision,coalesce(inquiry_submitted_at::text,''),inquiry_body,source_mail_id,inquiry_template_id FROM sourcing_cases WHERE tenant_id=$1 AND id=$2 AND status<>'CANCELLED' AND deleted_at IS NULL`, tenant, id).Scan(&v.ID, &v.Number, &v.DisplayInquiryNo, &owner, &v.Owner, &status, &handoff, &v.Revision, &v.SubmittedAt, &raw, &source, &templateID)
	if err == pgx.ErrNoRows {
		return nil, apierr.NotFound("INQUIRY_NOT_FOUND", "询盘不存在")
	}
	if err != nil {
		return nil, err
	}
	v.OwnerID = strconv.FormatInt(owner, 10)
	v.SourceMailID = strconv.FormatInt(source, 10)
	v.State = inquiryState(status, handoff)
	if view == "SALES" || view == "QUOTATIONS" {
		if s.customers != nil {
			var customerID int64
			if err := s.pool.QueryRow(ctx, "SELECT customer_id FROM sourcing_cases WHERE tenant_id=$1 AND id=$2", tenant, id).Scan(&customerID); err != nil {
				return nil, err
			}
			if err := s.checkInquiryCustomer(ctx, customerID); err != nil {
				return nil, err
			}
		}
		if err = s.AuthorizeSourcingCase(ctx, tenant, id, op); err != nil {
			return nil, err
		}
	} else if v.State != "INQUIRING" {
		return nil, apierr.NotFound("INQUIRY_NOT_FOUND", "询盘不存在")
	}
	// SALES visibility was checked above. Managers use the same write permission
	// as salespeople, within the employee range supplied by IAM.
	v.CanEdit = view == "SALES" && s.inquiryAllowed(ctx, op, view, true) == nil
	v.CanDelete = view == "SALES" && s.inquiryDeleteAllowed(ctx, op) == nil
	if view == "SALES" && !v.CanEdit {
		v.ReadOnlyReason = "当前账号没有询盘编辑权限"
	}
	if view == "SALES" && v.CanEdit && v.State == "INQUIRING" {
		var confirmed bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sourcing_customer_selections WHERE tenant_id=$1 AND case_id=$2 AND status='CUSTOMER_CONFIRMED')`, tenant, id).Scan(&confirmed); err != nil {
			return nil, err
		}
		v.CanWithdraw = !confirmed
		if confirmed {
			v.WithdrawReason = "客户已确认，不能直接撤回，请走后续业务变更流程"
		}
	}
	v.Legacy = len(raw) == 0
	if len(raw) > 0 {
		if err = json.Unmarshal(raw, &v.Body); err != nil {
			return nil, err
		}
	} else {
		old, e := s.GetSourcingCase(ctx, tenant, id)
		if e != nil {
			return nil, e
		}
		if old.Head.SourceFileKey != "" {
			v.Body.Attachments = []InquiryAttachment{{Key: old.Head.SourceFileKey, Name: old.Head.SourceFileName}}
		}
		v.Body.Customer = old.Head.CustomerName
		v.Body.CustomerID = strconv.FormatInt(old.Head.CustomerID, 10)
		v.Body.Contact = old.Head.ContactName
		v.Body.ContactID = strconv.FormatInt(old.Head.ContactID, 10)
		if len(old.Lines) > 0 {
			v.Body.DestinationPort = old.Lines[0].Port
			v.Body.Delivery = old.Lines[0].Delivery
			v.Body.Incoterm = old.Lines[0].Incoterm
		}
		for _, l := range old.Lines {
			values := map[string]string{}
			_ = json.Unmarshal(l.CustomFields, &values)
			for key, value := range map[string]string{"material_standard": l.MaterialStandard, "grade": l.Grade, "thickness": l.Thickness, "width": l.Width, "length_or_form": l.LengthOrForm, "surface_requirement": l.SurfaceRequirement, "coating": l.Coating, "tolerance": l.Tolerance, "coil_weight": l.CoilWeight, "coil_id": l.CoilID, "payment_terms": l.PaymentTerms, "incoterm": l.Incoterm, "port": l.Port} {
				if value != "" {
					values[key] = value
				}
			}
			v.Body.Products = append(v.Body.Products, InquiryProduct{ID: strconv.FormatInt(l.ID, 10), Product: l.Product, Specification: strings.Join(compactStrings([]string{l.MaterialStandard, l.Grade, l.Thickness, l.Width, l.LengthOrForm, l.SurfaceRequirement}), " · "), Quantity: l.Quantity, Unit: l.QuantityUnit, Delivery: l.Delivery, Packaging: l.Packaging, Remark: l.Remarks, CustomFields: values})
		}
	}
	if v.Body.Template == nil && templateID > 0 {
		v.Body.Template = &InquiryTemplateSnapshot{ID: strconv.FormatInt(templateID, 10)}
	}
	if err = s.prepareInquiryTemplate(ctx, tenant, &v.Body); err != nil {
		return nil, err
	}
	if v.Body.Products == nil {
		v.Body.Products = []InquiryProduct{}
	}
	if v.Body.Attachments == nil {
		v.Body.Attachments = []InquiryAttachment{}
	}
	for i := range v.Body.Products {
		if v.Body.Products[i].CustomFields == nil {
			v.Body.Products[i].CustomFields = map[string]string{}
		}
	}
	v.Quotes = []InquiryQuoteView{}
	rows, err := s.pool.Query(ctx, `SELECT id::text,kind,version,body,created_by::text,created_by_name,coalesce(submitted_at::text,''),updated_by_name,updated_at::text FROM inquiry_quotes WHERE tenant_id=$1 AND case_id=$2 AND inquiry_revision=$3 AND (submitted_at IS NOT NULL OR (kind=$5 AND $4>0)) ORDER BY id`, tenant, id, v.Revision, op.ID, view)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var q InquiryQuoteView
		var data []byte
		if err = rows.Scan(&q.ID, &q.Kind, &q.Version, &data, &q.AuthorID, &q.Author, &q.SubmittedAt, &q.UpdatedBy, &q.UpdatedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &q.Body); err != nil {
			return nil, err
		}
		if q.SubmittedAt != "" {
			if q.Kind == "PROCUREMENT" {
				v.ProcurementCount++
			} else {
				v.LogisticsCount++
			}
		}
		// A department sees its own quotes; sales sees submitted quotes from both.
		if view == "PROCUREMENT" || view == "LOGISTICS" {
			if q.Kind != view {
				continue
			}
		}
		q.CanEdit = q.Kind == view && (q.SubmittedAt != "" || q.AuthorID == strconv.FormatInt(op.ID, 10)) && s.inquiryAllowed(ctx, op, view, true) == nil
		v.Quotes = append(v.Quotes, q)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if v.State == "INQUIRING" {
		history, e := s.inquiryHistory(ctx, tenant, id, view)
		if e != nil {
			return nil, e
		}
		v.Quotes = append(v.Quotes, history...)
	}
	return &v, nil
}

func (s *Service) listInquiryWorkspace(ctx context.Context, tenant int64, op Operator, in InquiryCommand) (InquiryResult, error) {
	visible := Visibility{All: true}
	var err error
	if in.View == "SALES" || in.View == "QUOTATIONS" {
		visible, err = s.visibleSourcingTo(ctx, op)
		if err != nil {
			return InquiryResult{}, err
		}
	}
	customerAll := true
	var customerIDs []int64
	if (in.View == "SALES" || in.View == "QUOTATIONS") && s.customers != nil {
		customerIDs, customerAll, err = s.customers.VisibleIDs(ctx, op.ID)
		if err != nil {
			return InquiryResult{}, err
		}
	}
	rows, err := s.pool.Query(ctx, `SELECT id FROM sourcing_cases WHERE tenant_id=$1 AND ($6 OR customer_id=0 OR customer_id=ANY($7::bigint[])) AND status<>'CANCELLED' AND deleted_at IS NULL AND ($5 OR (status<>'INTAKE_PENDING' AND handoff_status<>'SALES_WITHDRAWN')) AND ($2 OR owner_id=ANY($3::bigint[])) AND ($4='' OR case_no ILIKE '%'||$4||'%' OR display_inquiry_no ILIKE '%'||$4||'%' OR customer_name ILIKE '%'||$4||'%' OR inquiry_body::text ILIKE '%'||$4||'%') ORDER BY updated_at DESC,id DESC`, tenant, visible.All, visible.EmployeeIDs, strings.TrimSpace(in.Keyword), in.View == "SALES", customerAll, customerIDs)
	if err != nil {
		return InquiryResult{}, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return InquiryResult{}, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return InquiryResult{}, err
	}
	out := InquiryResult{Items: []InquiryView{}}
	if in.Page < 1 {
		in.Page = 1
	}
	if in.Size != 20 && in.Size != 50 && in.Size != 100 {
		in.Size = 20
	}
	for _, id := range ids {
		v, e := s.readInquiry(ctx, tenant, id, op, in.View)
		if e != nil {
			if in.View == "PROCUREMENT" || in.View == "LOGISTICS" {
				var state string
				_ = s.pool.QueryRow(ctx, `SELECT status FROM sourcing_cases WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&state)
				if state == "INTAKE_PENDING" {
					continue
				}
			}
			return InquiryResult{}, e
		}
		if in.View == "QUOTATIONS" && v.State != "INQUIRING" {
			continue
		}
		state := v.State
		if in.View == "PROCUREMENT" {
			state = "WAITING"
			if v.ProcurementCount > 0 {
				state = "QUOTED"
			}
		}
		if in.View == "LOGISTICS" {
			state = "WAITING"
			if v.LogisticsCount > 0 {
				state = "QUOTED"
			}
		}
		if in.State != "" && in.State != state {
			continue
		}
		out.Total++
		if out.Total > (in.Page-1)*in.Size && len(out.Items) < in.Size {
			v.Quotes = nil
			out.Items = append(out.Items, *v)
		}
	}
	return out, nil
}

func validateInquiryBody(b InquiryBody, submit bool) error {
	if len(b.Products) == 0 || len(b.Products) > 10000 {
		return apierr.Invalid("INQUIRY_PRODUCTS", "请填写产品明细")
	}
	if submit && strings.TrimSpace(b.Customer) == "" {
		return apierr.Invalid("INQUIRY_CUSTOMER", "请填写客户")
	}
	for index, p := range b.Products {
		if !submit && p.Quantity == "" {
			continue
		}
		row := fmt.Sprintf("第%d行", index+1)
		if strings.TrimSpace(p.Product) == "" {
			return apierr.Invalid("INQUIRY_PRODUCT", row+"未填写产品")
		}
		qty, e := decimal.NewFromString(p.Quantity)
		if e != nil || !qty.IsPositive() {
			return apierr.Invalid("INQUIRY_PRODUCT", row+"数量必须是大于 0 的数字")
		}
		if strings.TrimSpace(p.Unit) == "" {
			return apierr.Invalid("INQUIRY_PRODUCT", row+"未填写单位")
		}
		if submit && b.Template != nil {
			for _, field := range b.Template.Fields {
				value := strings.TrimSpace(inquiryProductValue(p, field.FieldKey))
				if field.IsRequired && value == "" {
					return apierr.Invalid("INQUIRY_TEMPLATE_REQUIRED", row+"未填写"+field.DisplayName)
				}
				if value == "" || field.FieldKey == "unit_price" || field.FieldKey == "total_price" {
					continue
				}
				switch field.DataType {
				case "NUMBER":
					if _, err := decimal.NewFromString(value); err != nil {
						return apierr.Invalid("INQUIRY_TEMPLATE_NUMBER", row+field.DisplayName+"必须是数字")
					}
				case "DATE":
					if _, err := time.Parse("2006-01-02", value); err != nil {
						return apierr.Invalid("INQUIRY_TEMPLATE_DATE", row+field.DisplayName+"必须是有效日期")
					}
				}
			}
		}
	}
	return nil
}

func (s *Service) saveInquiry(ctx context.Context, tenant int64, op Operator, in InquiryCommand) (InquiryResult, error) {
	if err := s.validateInquiryCustomer(ctx, &in.Body); err != nil {
		return InquiryResult{}, err
	}
	if inquiryID(in.ID) > 0 && s.customers != nil {
		var customerID int64
		if err := s.pool.QueryRow(ctx, "SELECT customer_id FROM sourcing_cases WHERE tenant_id=$1 AND id=$2", tenant, inquiryID(in.ID)).Scan(&customerID); err != nil {
			return InquiryResult{}, err
		}
		if err := s.checkInquiryCustomer(ctx, customerID); err != nil {
			return InquiryResult{}, err
		}
	}
	if err := s.prepareInquiryTemplate(ctx, tenant, &in.Body); err != nil {
		return InquiryResult{}, err
	}
	if err := validateInquiryBody(in.Body, false); err != nil {
		return InquiryResult{}, err
	}
	id := inquiryID(in.ID)
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if id == 0 {
			if err := tx.QueryRow(ctx, `INSERT INTO sourcing_cases(tenant_id,case_no,owner_id,owner_name,status,handoff_status,customer_id,customer_name,contact_id,contact_name,inquiry_template_id,inquiry_template_code,inquiry_template_version) VALUES($1,'SC-'||to_char(current_date,'YYYYMMDD')||'-'||lpad(nextval('sourcing_case_no_seq')::text,6,'0'),$2,$3,'INTAKE_PENDING','DRAFT',$4,$5,$6,$7,$8,$9,$10) RETURNING id`, tenant, op.ID, op.Name, inquiryID(in.Body.CustomerID), in.Body.Customer, inquiryID(in.Body.ContactID), in.Body.Contact, inquiryID(in.Body.Template.ID), in.Body.Template.TemplateCode, in.Body.Template.Version).Scan(&id); err != nil {
				return err
			}
		} else {
			var owner, revision int64
			var state, handoff string
			if err := tx.QueryRow(ctx, `SELECT owner_id,inquiry_revision,status,handoff_status FROM sourcing_cases WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id).Scan(&owner, &revision, &state, &handoff); err != nil {
				return err
			}
			if err := s.authorizeInquiryOwner(ctx, op, owner); err != nil {
				return err
			}
			if revision != in.Revision || state != "INTAKE_PENDING" {
				return apierr.Conflict("INQUIRY_CHANGED", "询盘已变化，请刷新后重试")
			}
		}
		if e := validateInquiryAttachments(ctx, tx, tenant, id, "SALES", in.Body.Attachments); e != nil {
			return e
		}
		// Preserve line identities across edits, including references from mail.
		if _, err := tx.Exec(ctx, `UPDATE sourcing_lines SET line_no=-line_no WHERE tenant_id=$1 AND case_id=$2`, tenant, id); err != nil {
			return err
		}
		keep := []int64{}
		seen := map[int64]bool{}
		for i, p := range in.Body.Products {
			customFields := map[string]string{}
			for key, value := range p.CustomFields {
				if strings.HasPrefix(key, "custom.") && strings.TrimSpace(value) != "" {
					customFields[key] = value
				}
			}
			customJSON, _ := json.Marshal(customFields)
			lineID := inquiryID(p.ID)
			if lineID > 0 {
				if seen[lineID] {
					return apierr.Invalid("INQUIRY_LINE", "产品行重复")
				}
				seen[lineID] = true
				tag, e := tx.Exec(ctx, `UPDATE sourcing_lines SET line_no=$4,product=$5,material_standard=$6,grade=$7,thickness=$8,width=$9,length_or_form=$10,surface_requirement=$11,coating=$12,tolerance=$13,coil_weight=$14,coil_id=$15,packaging=$16,delivery=$17,payment_terms=$18,incoterm=$19,port=$20,quantity_unit=$21,remarks=$22,quantity=NULLIF($23,'')::numeric,custom_fields=$24::jsonb,updated_at=now() WHERE tenant_id=$1 AND case_id=$2 AND id=$3`, tenant, id, lineID, i+1, p.Product, p.CustomFields["material_standard"], p.CustomFields["grade"], p.CustomFields["thickness"], p.CustomFields["width"], p.CustomFields["length_or_form"], p.CustomFields["surface_requirement"], p.CustomFields["coating"], p.CustomFields["tolerance"], p.CustomFields["coil_weight"], p.CustomFields["coil_id"], p.Packaging, p.Delivery, p.CustomFields["payment_terms"], valueOr(p.CustomFields["incoterm"], in.Body.Incoterm), valueOr(p.CustomFields["port"], in.Body.DestinationPort), p.Unit, p.Remark, p.Quantity, customJSON)
				if e != nil {
					return e
				}
				if tag.RowsAffected() != 1 {
					return apierr.Invalid("INQUIRY_LINE", "产品不属于询盘")
				}
			} else {
				e := tx.QueryRow(ctx, `INSERT INTO sourcing_lines(tenant_id,case_id,line_no,product,material_standard,grade,thickness,width,length_or_form,surface_requirement,coating,tolerance,coil_weight,coil_id,packaging,delivery,payment_terms,incoterm,port,quantity_unit,remarks,quantity,custom_fields) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,NULLIF($22,'')::numeric,$23::jsonb) RETURNING id`, tenant, id, i+1, p.Product, p.CustomFields["material_standard"], p.CustomFields["grade"], p.CustomFields["thickness"], p.CustomFields["width"], p.CustomFields["length_or_form"], p.CustomFields["surface_requirement"], p.CustomFields["coating"], p.CustomFields["tolerance"], p.CustomFields["coil_weight"], p.CustomFields["coil_id"], p.Packaging, p.Delivery, p.CustomFields["payment_terms"], valueOr(p.CustomFields["incoterm"], in.Body.Incoterm), valueOr(p.CustomFields["port"], in.Body.DestinationPort), p.Unit, p.Remark, p.Quantity, customJSON).Scan(&lineID)
				if e != nil {
					return e
				}
			}
			keep = append(keep, lineID)
			in.Body.Products[i].ID = strconv.FormatInt(lineID, 10)
		}
		if _, e := tx.Exec(ctx, `DELETE FROM sourcing_lines WHERE tenant_id=$1 AND case_id=$2 AND NOT(id=ANY($3::bigint[]))`, tenant, id, keep); e != nil {
			return e
		}
		raw, err := json.Marshal(in.Body)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE sourcing_cases SET inquiry_body=$3,customer_id=$4,customer_name=$5,contact_id=$6,contact_name=$7,title=$8,inquiry_template_id=$9,inquiry_template_code=$10,inquiry_template_version=$11,inquiry_revision=inquiry_revision+1,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, id, raw, inquiryID(in.Body.CustomerID), in.Body.Customer, inquiryID(in.Body.ContactID), in.Body.Contact, valueOr(strings.TrimSpace(in.Body.Title), "客户询盘"), inquiryID(in.Body.Template.ID), in.Body.Template.TemplateCode, in.Body.Template.Version)
		if err != nil {
			return err
		}
		return s.recordInquiryAction(ctx, tx, tenant, id, op, "save")
	})
	if err != nil {
		return InquiryResult{}, err
	}
	s.nudge(ctx, tenant)
	v, err := s.readInquiry(ctx, tenant, id, op, "SALES")
	return InquiryResult{Item: v}, err
}

func (s *Service) updateInquiryBasic(ctx context.Context, tenant int64, op Operator, in InquiryCommand) (InquiryResult, error) {
	if err := s.validateInquiryCustomer(ctx, &in.Body); err != nil {
		return InquiryResult{}, err
	}
	if inquiryID(in.ID) > 0 && s.customers != nil {
		var customerID int64
		if err := s.pool.QueryRow(ctx, "SELECT customer_id FROM sourcing_cases WHERE tenant_id=$1 AND id=$2", tenant, inquiryID(in.ID)).Scan(&customerID); err != nil {
			return InquiryResult{}, err
		}
		if err := s.checkInquiryCustomer(ctx, customerID); err != nil {
			return InquiryResult{}, err
		}
	}
	id := inquiryID(in.ID)
	if id == 0 {
		return InquiryResult{}, apierr.Invalid("INQUIRY_ID", "询盘不存在")
	}
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var owner, revision int64
		var state string
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT owner_id,inquiry_revision,status,inquiry_body FROM sourcing_cases WHERE tenant_id=$1 AND id=$2 AND status<>'CANCELLED' FOR UPDATE`, tenant, id).Scan(&owner, &revision, &state, &raw); err != nil {
			if err == pgx.ErrNoRows {
				return apierr.NotFound("INQUIRY_NOT_FOUND", "询盘不存在")
			}
			return err
		}
		if err := s.authorizeInquiryOwner(ctx, op, owner); err != nil {
			return err
		}
		if revision != in.Revision || state == "INTAKE_PENDING" {
			return apierr.Conflict("INQUIRY_CHANGED", "询盘已变化，请刷新后重试")
		}
		var body InquiryBody
		if err := json.Unmarshal(raw, &body); err != nil {
			return err
		}
		body.CustomerID = in.Body.CustomerID
		body.Customer = strings.TrimSpace(in.Body.Customer)
		body.ContactID = in.Body.ContactID
		body.Contact = strings.TrimSpace(in.Body.Contact)
		body.Delivery = strings.TrimSpace(in.Body.Delivery)
		body.LoadingPort = strings.TrimSpace(in.Body.LoadingPort)
		body.DestinationPort = strings.TrimSpace(in.Body.DestinationPort)
		body.Remark = strings.TrimSpace(in.Body.Remark)
		updated, err := json.Marshal(body)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE sourcing_cases SET inquiry_body=$3,customer_id=$4,customer_name=$5,contact_id=$6,contact_name=$7,inquiry_revision=inquiry_revision+1,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, id, updated, inquiryID(body.CustomerID), body.Customer, inquiryID(body.ContactID), body.Contact); err != nil {
			return err
		}
		return s.recordInquiryAction(ctx, tx, tenant, id, op, "updateBasic")
	})
	if err != nil {
		return InquiryResult{}, err
	}
	s.nudge(ctx, tenant)
	v, err := s.readInquiry(ctx, tenant, id, op, "SALES")
	return InquiryResult{Item: v}, err
}
func (s *Service) changeInquiry(ctx context.Context, tenant int64, op Operator, in InquiryCommand) (InquiryResult, error) {
	id := inquiryID(in.ID)
	view, err := s.readInquiry(ctx, tenant, id, op, "SALES")
	if err != nil {
		return InquiryResult{}, err
	}
	if in.Action == "submit" {
		if err = validateInquiryBody(view.Body, true); err != nil {
			return InquiryResult{}, err
		}
		access := s.scopes.(InquiryAccess)
		ok, e := access.HasPermission(ctx, op.ID, "sales:inquiry:submit")
		if e != nil {
			return InquiryResult{}, e
		}
		if !ok {
			return InquiryResult{}, apierr.Permission("INQUIRY_SUBMIT_PERMISSION", "没有提交询盘权限")
		}
	}
	remoteAvailabilityChanged := false
	remoteAvailability := false
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var owner, revision int64
		var state, handoff string
		if e := tx.QueryRow(ctx, `SELECT owner_id,inquiry_revision,status,handoff_status FROM sourcing_cases WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id).Scan(&owner, &revision, &state, &handoff); e != nil {
			return e
		}
		if err := s.authorizeInquiryOwner(ctx, op, owner); err != nil {
			return err
		}
		if revision != in.Revision {
			return apierr.Conflict("INQUIRY_CHANGED", "询盘已变化，请刷新后重试")
		}
		if s.inquiryDocuments == nil {
			return apierr.Conflict("INQUIRY_EXPORT_UNAVAILABLE", "报价合同服务未连接，未执行此操作")
		}
		if in.Action == "submit" {
			if state != "INTAKE_PENDING" {
				return apierr.Conflict("INQUIRY_CHANGED", "当前询盘不能提交")
			}
			if e := s.inquiryDocuments.SetAvailability(ctx, id, true); e != nil {
				return e
			}
			remoteAvailabilityChanged, remoteAvailability = true, true
			raw, _ := json.Marshal(view.Body)
			if _, e := tx.Exec(ctx, `UPDATE sourcing_cases SET inquiry_body=$3,status='REVIEWING',handoff_status='WAITING_ACCEPTANCE',inquiry_submitted_at=now(),inquiry_revision=inquiry_revision+1,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, id, raw); e != nil {
				return e
			}
			_, e := tx.Exec(ctx, `UPDATE sourcing_lines SET decision='CONFIRMED' WHERE tenant_id=$1 AND case_id=$2`, tenant, id)
			if e != nil {
				return e
			}
			return s.recordInquiryAction(ctx, tx, tenant, id, op, "submit")
		}
		if state == "INTAKE_PENDING" || state == "CANCELLED" {
			return apierr.Conflict("INQUIRY_CHANGED", "当前询盘不能撤回")
		}
		var confirmed bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sourcing_customer_selections WHERE tenant_id=$1 AND case_id=$2 AND status='CUSTOMER_CONFIRMED')`, tenant, id).Scan(&confirmed); e != nil {
			return e
		}
		if confirmed {
			return apierr.Conflict("INQUIRY_CONFIRMED", "客户已确认，不能撤回")
		}
		if e := s.inquiryDocuments.SetAvailability(ctx, id, false); e != nil {
			return e
		}
		remoteAvailabilityChanged, remoteAvailability = true, false
		for _, statement := range []string{
			`DELETE FROM inquiry_quotes WHERE tenant_id=$1 AND case_id=$2`,
			`DELETE FROM sourcing_customer_selections WHERE tenant_id=$1 AND case_id=$2`,
			`DELETE FROM sourcing_customer_feedback WHERE tenant_id=$1 AND case_id=$2`,
			`DELETE FROM sourcing_shipping_rework_requests WHERE tenant_id=$1 AND case_id=$2`,
			`DELETE FROM sourcing_sales_plans WHERE tenant_id=$1 AND case_id=$2`,
			`DELETE FROM procurement_rework_requests WHERE tenant_id=$1 AND case_id=$2`,
			`DELETE FROM cost_scenarios WHERE tenant_id=$1 AND case_id=$2`,
			`DELETE FROM sourcing_shipping_plans WHERE tenant_id=$1 AND request_id IN(SELECT id FROM sourcing_shipping_requests WHERE tenant_id=$1 AND case_id=$2)`,
			`DELETE FROM sourcing_shipping_options WHERE tenant_id=$1 AND request_id IN(SELECT id FROM sourcing_shipping_requests WHERE tenant_id=$1 AND case_id=$2)`,
			`DELETE FROM sourcing_shipping_requests WHERE tenant_id=$1 AND case_id=$2`,
			`DELETE FROM procurement_plans WHERE tenant_id=$1 AND case_id=$2`,
			`DELETE FROM factory_rfqs WHERE tenant_id=$1 AND case_id=$2`,
			`DELETE FROM sourcing_procurement_participants WHERE tenant_id=$1 AND case_id=$2`,
		} {
			if _, e := tx.Exec(ctx, statement, tenant, id); e != nil {
				return e
			}
		}
		_, e := tx.Exec(ctx, `UPDATE sourcing_cases SET status='INTAKE_PENDING',handoff_status='SALES_WITHDRAWN',inquiry_revision=inquiry_revision+1,inquiry_submitted_at=NULL,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, id)
		if e != nil {
			return e
		}
		return s.recordInquiryAction(ctx, tx, tenant, id, op, "withdraw")
	})
	if err != nil {
		if remoteAvailabilityChanged {
			s.compensateInquiryAvailability(ctx, id, !remoteAvailability)
		}
		return InquiryResult{}, err
	}
	s.nudge(ctx, tenant)
	v, err := s.readInquiry(ctx, tenant, id, op, "SALES")
	return InquiryResult{Item: v}, err
}

// deleteInquiry performs a soft delete.  CANCELLED keeps older readers and
// downstream work queues from treating the inquiry as active, while the
// deletion columns and sourcing change record preserve who deleted it and
// when.  Related lines, quotes and documents are intentionally retained.
func (s *Service) deleteInquiry(ctx context.Context, tenant int64, op Operator, in InquiryCommand) (InquiryResult, error) {
	if err := s.inquiryDeleteAllowed(ctx, op); err != nil {
		return InquiryResult{}, err
	}
	id := inquiryID(in.ID)
	if id <= 0 {
		return InquiryResult{}, apierr.Invalid("INQUIRY_ID", "询盘不存在")
	}
	if err := s.AuthorizeSourcingCase(ctx, tenant, id, op); err != nil {
		return InquiryResult{}, err
	}
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var lockedID int64
		if err := tx.QueryRow(ctx, `SELECT id FROM sourcing_cases WHERE tenant_id=$1 AND id=$2 AND status<>'CANCELLED' AND deleted_at IS NULL FOR UPDATE`, tenant, id).Scan(&lockedID); err != nil {
			if err == pgx.ErrNoRows {
				return apierr.NotFound("INQUIRY_NOT_FOUND", "询盘不存在")
			}
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE sourcing_cases SET status='CANCELLED',handoff_status='CANCELLED',deleted_at=now(),deleted_by_id=$3,deleted_by_name=$4,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, id, op.ID, op.Name); err != nil {
			return err
		}
		// Closing the parent alone hides it from the shared inquiry workspace,
		// but procurement and shipping also have independent task tables used by
		// dashboards and older detail endpoints.  Cancel every still-active task
		// in the same transaction so a deleted inquiry cannot remain actionable
		// anywhere else.  Quotes and plans stay in place as audit history.
		for _, statement := range []struct {
			query string
			args  []any
		}{
			{`UPDATE factory_rfqs SET status='CANCELLED',updated_at=now() WHERE tenant_id=$1 AND case_id=$2 AND status NOT IN ('CANCELLED','CLOSED')`, []any{tenant, id}},
			{`UPDATE procurement_rework_requests SET status='CANCELLED',resolved_by=$3,resolved_by_name=$4,resolved_at=now(),resolution_note=CASE WHEN resolution_note='' THEN '来源询盘已删除' ELSE resolution_note END WHERE tenant_id=$1 AND case_id=$2 AND status='OPEN'`, []any{tenant, id, op.ID, op.Name}},
			{`UPDATE sourcing_shipping_requests SET status='CANCELLED',updated_at=now() WHERE tenant_id=$1 AND case_id=$2 AND status<>'CANCELLED'`, []any{tenant, id}},
			{`UPDATE sourcing_shipping_options SET status='CANCELLED' WHERE tenant_id=$1 AND request_id IN (SELECT id FROM sourcing_shipping_requests WHERE tenant_id=$1 AND case_id=$2) AND status<>'CANCELLED'`, []any{tenant, id}},
			{`UPDATE sourcing_shipping_rework_requests SET status='CANCELLED',resolved_by=$3,resolved_by_name=$4,resolved_at=now(),resolution_note=CASE WHEN resolution_note='' THEN '来源询盘已删除' ELSE resolution_note END WHERE tenant_id=$1 AND case_id=$2 AND status='OPEN'`, []any{tenant, id, op.ID, op.Name}},
		} {
			if _, err := tx.Exec(ctx, statement.query, statement.args...); err != nil {
				return err
			}
		}
		return s.recordInquiryAction(ctx, tx, tenant, id, op, "delete")
	})
	if err != nil {
		return InquiryResult{}, err
	}
	s.nudge(ctx, tenant)
	return InquiryResult{}, nil
}

// Called with the case row locked so authorization uses its current owner.
func (s *Service) authorizeInquiryOwner(ctx context.Context, op Operator, owner int64) error {
	if err := s.inquiryAllowed(ctx, op, "SALES", true); err != nil {
		return err
	}
	visible, err := s.visibleSourcingTo(ctx, op)
	if err != nil {
		return err
	}
	if !allowedSourcingOwner(visible, owner) {
		return apierr.NotFound("SC_CASE_NOT_FOUND", "询盘不存在")
	}
	return nil
}

func (s *Service) recordInquiryAction(ctx context.Context, tx pgx.Tx, tenant, id int64, op Operator, action string) error {
	summary := map[string]string{"save": "保存询盘", "updateBasic": "更新询盘基本信息", "submit": "提交询价", "withdraw": "撤回询价", "delete": "删除询盘"}[action]
	return s.q.WithTx(tx).CreateSourcingChange(ctx, store.CreateSourcingChangeParams{
		TenantID: tenant, CaseID: id, Section: "INQUIRY", Action: strings.ToUpper(action),
		Summary: summary, BeforeJson: []byte(`{}`), AfterJson: []byte(`{}`),
		OperatorID: op.ID, OperatorName: op.Name,
	})
}

// A document-service fence is changed before the local transaction so a
// concurrently created customer quotation can never slip through a withdrawal.
// If the local commit then fails, restore the former document state without
// relying on the salesperson to retry the original business action.
func (s *Service) compensateInquiryAvailability(ctx context.Context, id int64, available bool) {
	retryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(attempt*250) * time.Millisecond)
			select {
			case <-retryCtx.Done():
				timer.Stop()
				err = retryCtx.Err()
				attempt = 3
				continue
			case <-timer.C:
			}
		}
		err = s.inquiryDocuments.SetAvailability(retryCtx, id, available)
		if err == nil {
			s.log.Info("询盘跨服务状态已自动恢复", "case_id", id, "available", available, "attempt", attempt+1)
			return
		}
	}
	s.log.Error("询盘跨服务状态自动恢复失败", "case_id", id, "available", available, "error", err)
}

func validateInquiryQuote(b *InquiryQuoteBody, products []InquiryProduct, kind string, submit bool) error {
	if strings.TrimSpace(b.Company) == "" {
		if kind == "LOGISTICS" {
			return apierr.Invalid("INQUIRY_COMPANY", "请填写船运公司名称")
		}
		return apierr.Invalid("INQUIRY_COMPANY", "请选择或输入工厂/供应商")
	}
	b.Incoterm = strings.ToUpper(strings.TrimSpace(b.Incoterm))
	allowed := map[string]bool{}
	productByID := map[string]InquiryProduct{}
	for _, p := range products {
		allowed[p.ID] = true
		productByID[p.ID] = p
	}
	for _, d := range []string{b.Delivery, b.ValidUntil, b.Departure, b.Arrival} {
		if d != "" {
			if _, e := time.Parse("2006-01-02", d); e != nil {
				return apierr.Invalid("INQUIRY_DATE", "日期格式无效")
			}
		}
	}
	if b.Departure != "" && b.Arrival != "" && b.Arrival < b.Departure {
		return apierr.Invalid("INQUIRY_DATES", "预计到港不能早于开船")
	}
	if kind == "PROCUREMENT" {
		b.QuoteCategory = strings.ToUpper(strings.TrimSpace(b.QuoteCategory))
		categoryCurrencies := map[string]string{
			"FOB_USD":          "USD",
			"FOB_CNY":          "CNY",
			"ALL_IN_PORT_CNY":  "CNY",
			"EX_FACTORY_CNY":   "CNY",
			"REPROCESSING_CNY": "CNY",
			"DIRECT_CFR_USD":   "USD",
		}
		expectedCurrency, categoryValid := categoryCurrencies[b.QuoteCategory]
		if b.QuoteCategory != "" && !categoryValid {
			return apierr.Invalid("INQUIRY_QUOTE_CATEGORY", "请选择有效的报价分类")
		}
		if submit && !categoryValid {
			return apierr.Invalid("INQUIRY_QUOTE_CATEGORY", "请选择报价分类")
		}
		if categoryValid {
			b.Currency = expectedCurrency
		}
		if submit && (len(b.Prices) == 0 || len(b.Currency) != 3) {
			return apierr.Invalid("INQUIRY_PRICES", "请填写币种并选择至少一个产品报价")
		}
		seen := map[string]bool{}
		for _, l := range b.Prices {
			if !allowed[l.ProductID] || seen[l.ProductID] {
				return apierr.Invalid("INQUIRY_LINE", "产品不属于询盘或重复")
			}
			seen[l.ProductID] = true
			if l.Price == "" && l.FactoryPrice == "" && l.FOBPrice == "" && !submit {
				continue
			}
			for name, value := range map[string]string{"price": l.Price, "factoryPrice": l.FactoryPrice, "fobPrice": l.FOBPrice, "slitting": l.Slitting} {
				if value == "" {
					continue
				}
				p, e := decimal.NewFromString(value)
				if e != nil || p.IsNegative() {
					return apierr.Invalid("INQUIRY_PRICE", "请填写有效的工厂报价").WithMeta("field", name)
				}
			}
			if submit && b.QuoteCategory == "REPROCESSING_CNY" && (strings.TrimSpace(l.Price) == "" || strings.TrimSpace(l.FactoryPrice) == "") {
				return apierr.Invalid("INQUIRY_PRICE", "再加工报价必须填写加工费和出厂单价")
			}
			if submit && l.Price == "" && l.FactoryPrice == "" && l.FOBPrice == "" {
				return apierr.Invalid("INQUIRY_PRICE", "请填写工厂报价")
			}
			if l.Delivery != "" && !validOptionalDate(l.Delivery) {
				return apierr.Invalid("INQUIRY_DATE", "交货日期无效")
			}
		}
	} else {
		b.Currency = "USD"
		normalizedRates := map[string]string{}
		parsedRates := map[string]decimal.Decimal{}
		for currency, raw := range b.ExchangeRates {
			currency = strings.ToUpper(strings.TrimSpace(currency))
			raw = strings.TrimSpace(raw)
			if currency == "USD" || raw == "" {
				continue
			}
			rate, e := decimal.NewFromString(raw)
			if len(currency) != 3 || e != nil || !rate.IsPositive() {
				return apierr.Invalid("INQUIRY_EXCHANGE_RATE", "请填写有效的汇率（1 USD 可兑换多少原币）").WithMeta("currency", currency)
			}
			normalizedRates[currency] = rate.String()
			parsedRates[currency] = rate
		}
		b.ExchangeRates = normalizedRates
		toUSD := func(amount decimal.Decimal, currency string, scale int32) (decimal.Decimal, bool) {
			if currency == "USD" {
				return amount.Round(scale), true
			}
			rate, ok := parsedRates[currency]
			if !ok {
				return decimal.Zero, false
			}
			return amount.Div(rate).Round(scale), true
		}
		for _, id := range b.CargoIDs {
			if !allowed[id] {
				return apierr.Invalid("INQUIRY_CARGO", "产品不属于询盘")
			}
		}
		seenRates := map[string]bool{}
		for i := range b.FreightRates {
			rate := &b.FreightRates[i]
			rate.Currency = strings.ToUpper(strings.TrimSpace(rate.Currency))
			rate.Unit = strings.TrimSpace(rate.Unit)
			price, e := decimal.NewFromString(strings.TrimSpace(rate.Price))
			if !allowed[rate.ProductID] || seenRates[rate.ProductID] {
				return apierr.Invalid("INQUIRY_FREIGHT_PRODUCT", "产品海运价格包含无效或重复产品")
			}
			seenRates[rate.ProductID] = true
			if len(rate.Currency) != 3 || rate.Unit == "" || e != nil || price.IsNegative() {
				return apierr.Invalid("INQUIRY_FREIGHT_RATE", "请填写有效的产品海运单价、币种和计价单位")
			}
			rate.Price = price.String()
			if converted, ok := toUSD(price, rate.Currency, 4); ok {
				rate.USDPrice = converted.StringFixed(4)
			} else {
				rate.USDPrice = ""
				if submit {
					return apierr.Invalid("INQUIRY_EXCHANGE_RATE", "非 USD 物流费用必须填写汇率").WithMeta("currency", rate.Currency)
				}
			}
		}
		if submit {
			if len(b.FreightRates) == 0 && len(b.Charges) == 0 {
				return apierr.Invalid("INQUIRY_CHARGE", "请填写物流报价")
			}
		}
		totals := map[string]decimal.Decimal{}
		totalUSD := decimal.Zero
		usdComplete := true
		for i := range b.Charges {
			c := &b.Charges[i]
			c.Currency = strings.ToUpper(strings.TrimSpace(c.Currency))
			if !submit && (c.Amount == "" || c.Quantity == "") {
				continue
			}
			a, e := decimal.NewFromString(c.Amount)
			q, qe := decimal.NewFromString(c.Quantity)
			if c.Name == "" || len(c.Currency) != 3 || e != nil || qe != nil || a.IsNegative() || !q.IsPositive() {
				return apierr.Invalid("INQUIRY_CHARGE", "请填写费用名称、币种、金额和数量")
			}
			c.AllocationType = strings.ToUpper(strings.TrimSpace(c.AllocationType))
			if c.AllocationType == "" {
				c.AllocationType = "FIXED"
			}
			if c.AllocationType != "DIRECT" && c.AllocationType != "PER_TON" && c.AllocationType != "FIXED" {
				return apierr.Invalid("INQUIRY_CHARGE_ALLOCATION", "请选择产品专属、按吨或整票固定费用")
			}
			if c.AllocationType == "DIRECT" && !allowed[c.ProductID] {
				return apierr.Invalid("INQUIRY_CHARGE_PRODUCT", "产品专属费用必须选择对应产品")
			}
			sub := a.Mul(q).Round(2)
			c.Subtotal = sub.StringFixed(2)
			totals[c.Currency] = totals[c.Currency].Add(sub)
			if converted, ok := toUSD(sub, c.Currency, 2); ok {
				c.USDSubtotal = converted.StringFixed(2)
				totalUSD = totalUSD.Add(converted)
			} else {
				c.USDSubtotal = ""
				usdComplete = false
				if submit {
					return apierr.Invalid("INQUIRY_EXCHANGE_RATE", "非 USD 物流费用必须填写汇率").WithMeta("currency", c.Currency)
				}
			}
		}
		b.Totals = map[string]string{}
		for k, v := range totals {
			b.Totals[k] = v.StringFixed(2)
		}
		b.TotalUSD = ""
		b.OtherChargesUSD = ""
		if usdComplete && len(b.Charges) > 0 {
			b.TotalUSD = totalUSD.Round(2).StringFixed(2)
			b.OtherChargesUSD = b.TotalUSD
		}
	}
	return nil
}

func (s *Service) saveInquiryQuote(ctx context.Context, tenant int64, op Operator, in InquiryCommand) (InquiryResult, error) {
	id := inquiryID(in.ID)
	quoteID := inquiryID(in.QuoteID)
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var revision int64
		var status, handoff string
		var raw []byte
		if e := tx.QueryRow(ctx, `SELECT inquiry_revision,status,handoff_status,inquiry_body FROM sourcing_cases WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id).Scan(&revision, &status, &handoff, &raw); e != nil {
			return e
		}
		if inquiryState(status, handoff) != "INQUIRING" || status == "CANCELLED" || revision != in.Revision {
			return apierr.Conflict("INQUIRY_CHANGED", "询盘已撤回或变化，请刷新后重试")
		}
		if len(raw) == 0 {
			legacy, e := s.readInquiry(ctx, tenant, id, op, in.View)
			if e != nil {
				return e
			}
			raw, e = json.Marshal(legacy.Body)
			if e != nil {
				return e
			}
		}
		var body InquiryBody
		if e := json.Unmarshal(raw, &body); e != nil {
			return e
		}
		if e := validateInquiryQuote(&in.Quote, body.Products, in.View, in.Submit); e != nil {
			return e
		}
		if e := validateInquiryAttachments(ctx, tx, tenant, id, in.View, in.Quote.Attachments); e != nil {
			return e
		}
		data, e := json.Marshal(in.Quote)
		if e != nil {
			return e
		}
		if quoteID == 0 {
			_, e = tx.Exec(ctx, `INSERT INTO inquiry_quotes(tenant_id,case_id,inquiry_revision,kind,body,created_by,created_by_name,submitted_at,submitted_by,updated_by,updated_by_name) VALUES($1,$2,$3,$4,$5,$6,$7,CASE WHEN $8 THEN now() END,CASE WHEN $8 THEN $6::bigint END,$6,$7)`, tenant, id, revision, in.View, data, op.ID, op.Name, in.Submit)
			return e
		}
		var author, version int64
		var kind string
		var submitted bool
		if e = tx.QueryRow(ctx, `SELECT created_by,version,kind,submitted_at IS NOT NULL FROM inquiry_quotes WHERE tenant_id=$1 AND case_id=$2 AND id=$3 AND inquiry_revision=$4 FOR UPDATE`, tenant, id, quoteID, revision).Scan(&author, &version, &kind, &submitted); e != nil {
			return e
		}
		if kind != in.View || (!submitted && author != op.ID) {
			return apierr.Permission("INQUIRY_QUOTE_AUTHOR", "未提交报价仅填写人可编辑")
		}
		if submitted {
			if err := validateInquiryQuote(&in.Quote, body.Products, in.View, true); err != nil {
				return err
			}
			data, _ = json.Marshal(in.Quote)
		}
		if version != in.QuoteVersion {
			return apierr.Conflict("INQUIRY_QUOTE_CHANGED", "报价已由其他人修改，请刷新")
		}
		_, e = tx.Exec(ctx, `UPDATE inquiry_quotes SET body=$4,updated_by=$5,updated_by_name=$6,updated_at=now(),version=version+1,submitted_at=CASE WHEN $7 AND submitted_at IS NULL THEN now() ELSE submitted_at END,submitted_by=CASE WHEN $7 AND submitted_by IS NULL THEN $5::bigint ELSE submitted_by END WHERE tenant_id=$1 AND case_id=$2 AND id=$3`, tenant, id, quoteID, data, op.ID, op.Name, in.Submit)
		return e
	})
	if err != nil {
		return InquiryResult{}, err
	}
	s.nudge(ctx, tenant)
	v, err := s.readInquiry(ctx, tenant, id, op, in.View)
	return InquiryResult{Item: v}, err
}

// Keep errors attributable without exposing database text at transport edges.

func (s *Service) AuthorizeInquiryCreate(ctx context.Context, op Operator) error {
	return s.inquiryAllowed(ctx, op, "SALES", true)
}
