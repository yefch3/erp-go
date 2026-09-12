package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/shopspring/decimal"
)

var offerCurrency = regexp.MustCompile(`^[A-Z]{3}$`)

func offerID(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }
func initialOffer(source OfferInquiry) OfferBody {
	b := source.Body
	out := OfferBody{CustomerID: b.CustomerID, Customer: b.Customer, ContactID: b.ContactID, Contact: b.Contact, Currency: "USD", Delivery: b.Delivery, LoadingPort: b.LoadingPort, DestinationPort: b.DestinationPort, Incoterm: b.Incoterm, Remark: b.Remark, Lines: []OfferLine{}, Transports: []OfferTransport{}}
	for _, p := range b.Products {
		mt := ""
		if strings.EqualFold(p.Unit, "MT") {
			mt = "1"
		}
		out.Lines = append(out.Lines, OfferLine{OfferProduct: p, Calculation: OfferCalculation{MTPerUnit: mt}})
	}
	return out
}
func (s *Service) CustomerOffer(ctx context.Context, raw string) (string, error) {
	var cmd OfferCommand
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if len(raw) > 8*1024*1024 {
		return "", apierr.Invalid("OFFER_INPUT", "报价内容过大")
	}
	if err := dec.Decode(&cmd); err != nil {
		return "", apierr.Invalid("OFFER_INPUT", "报价输入无效")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return "", apierr.Invalid("OFFER_INPUT", "报价输入无效")
	}
	id := offerID(cmd.CaseID)
	op, ok := grpcx.OperatorFromContext(ctx)
	if !ok || op.TenantID <= 0 || op.EmployeeID <= 0 {
		return "", apierr.Unauthorized("OFFER_ACTOR", "请先登录")
	}
	access, ok := s.scopes.(OfferAccess)
	if !ok || s.offerSource == nil {
		return "", apierr.Permission("OFFER_ACCESS", "报价权限校验不可用")
	}
	write := cmd.Action == "save" || cmd.Action == "calculate" || cmd.Action == "calculate_all" || cmd.Action == "confirm"
	if cmd.Action != "get" && cmd.Action != "pdf" && cmd.Action != "summaries" && !write {
		return "", apierr.Invalid("OFFER_ACTION", "不支持的报价操作")
	}
	permission := "export:quotation:read"
	if write {
		permission = "export:quotation:write"
	}
	allowed, err := access.HasPermission(ctx, op.EmployeeID, permission)
	if err != nil {
		return "", err
	}
	if !allowed {
		return "", apierr.Permission("OFFER_PERMISSION", "没有客户报价权限")
	}
	if cmd.Action == "summaries" {
		return s.offerSummaries(ctx, op)
	}
	if id <= 0 {
		return "", apierr.Invalid("OFFER_CASE", "请选择客户询盘")
	}
	source, err := s.offerSource.ReadInquiry(ctx, id)
	if err != nil {
		return "", err
	}
	if source.State != "INQUIRING" {
		return "", apierr.Conflict("OFFER_WITHDRAWN", "询盘尚未提交或已撤回")
	}
	owner := offerID(source.OwnerID) == op.EmployeeID
	if write && !owner {
		return "", apierr.Permission("OFFER_OWNER", "只有负责销售可以修改或确认客户报价")
	}
	view, err := s.readOffer(ctx, op.TenantID, id, source)
	if err != nil {
		return "", err
	}
	view.CanEdit = owner && view.Status != "CONFIRMED"
	switch cmd.Action {
	case "get":
	case "pdf":
		if view.Revision == 0 {
			return "", apierr.Invalid("OFFER_SAVE_FIRST", "请先保存客户报价")
		}
		data, err := offerPDF(view)
		if err != nil {
			return "", err
		}
		out, _ := json.Marshal(map[string]any{"fileName": source.Number + "-客户报价.pdf", "fileData": data})
		return string(out), nil
	case "confirm":
		if view.Status != "CONFIRMED" {
			if cmd.Revision != view.Revision || view.Revision == 0 {
				return "", apierr.Conflict("OFFER_REVISION", "报价已变化，请刷新后检查再确认")
			}
			if err = s.confirmOffer(ctx, op, id, view); err != nil {
				return "", err
			}
		}
		view, err = s.readOffer(ctx, op.TenantID, id, source)
		if err != nil {
			return "", err
		}
		view.CanEdit = false
	case "save", "calculate", "calculate_all":
		if !view.CanEdit {
			return "", apierr.Conflict("OFFER_CONFIRMED", "客户已确认，不能覆盖成交报价")
		}
		if cmd.Revision != view.Revision {
			return "", apierr.Conflict("OFFER_REVISION", "报价已变化，请刷新后再保存")
		}
		rates, ok := s.rates.(OfferRates)
		if !ok {
			return "", apierr.Internal("OFFER_RATES", "有效汇率服务不可用")
		}
		confirmed, err := rates.EffectiveRates(ctx)
		if err != nil {
			return "", err
		}
		calculate := cmd.Action == "calculate" || cmd.Action == "calculate_all"
		target := cmd.LineID
		if cmd.Action == "calculate_all" {
			target = "*"
		}
		body, err := prepareOffer(cmd.Body, source, confirmed, calculate, target)
		if err != nil {
			return "", err
		}
		if calculate {
			view.Body = body
			break
		}
		data, _ := json.Marshal(body)
		snapshot, _ := json.Marshal(source)
		err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
			if err := lockOfferInquiry(ctx, tx, op.TenantID, id); err != nil {
				return err
			}
			var rows int64
			if view.Revision == 0 {
				tag, e := tx.Exec(ctx, `INSERT INTO customer_offers(tenant_id,case_id,body,source_snapshot,updated_by) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, op.TenantID, id, data, snapshot, op.EmployeeID)
				if e != nil {
					return e
				}
				rows = tag.RowsAffected()
			} else {
				tag, e := tx.Exec(ctx, `UPDATE customer_offers SET body=$3,source_snapshot=$4,updated_by=$5,updated_at=now(),revision=revision+1 WHERE tenant_id=$1 AND case_id=$2 AND revision=$6 AND confirmed_at IS NULL`, op.TenantID, id, data, snapshot, op.EmployeeID, view.Revision)
				if e != nil {
					return e
				}
				rows = tag.RowsAffected()
			}
			if rows != 1 {
				return apierr.Conflict("OFFER_REVISION", "报价已变化，请刷新后再保存")
			}
			return nil
		})
		if err != nil {
			return "", err
		}
		view, err = s.readOffer(ctx, op.TenantID, id, source)
		if err != nil {
			return "", err
		}
		view.CanEdit = true
	}
	out, err := json.Marshal(view)
	return string(out), err
}
func lockOfferInquiry(ctx context.Context, tx pgx.Tx, tenant, id int64) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::bigint::text || ':inquiry:' || $2::bigint::text,0))`, tenant, id); err != nil {
		return err
	}
	var withdrawn bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM inquiry_withdrawal_fences WHERE tenant_id=$1 AND case_id=$2)`, tenant, id).Scan(&withdrawn); err != nil {
		return err
	}
	if withdrawn {
		return apierr.Conflict("OFFER_WITHDRAWN", "询盘已撤回")
	}
	return nil
}
func (s *Service) readOffer(ctx context.Context, tenant, id int64, source OfferInquiry) (OfferView, error) {
	out := OfferView{Source: source, Body: initialOffer(source), Status: "PENDING"}
	var body, snapshot []byte
	var quotation, contract int64
	err := s.pool.QueryRow(ctx, `SELECT revision,body,source_snapshot,COALESCE(quotation_id,0),COALESCE(contract_id,0) FROM customer_offers WHERE tenant_id=$1 AND case_id=$2`, tenant, id).Scan(&out.Revision, &body, &snapshot, &quotation, &contract)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(body, &out.Body); err != nil {
		return out, err
	}
	out.Status = "QUOTED"
	if contract > 0 {
		out.Status = "CONFIRMED"
		if err = json.Unmarshal(snapshot, &out.Source); err != nil {
			return out, err
		}
	}
	out.QuotationID = strconv.FormatInt(quotation, 10)
	out.ContractID = strconv.FormatInt(contract, 10)
	return out, nil
}
func effectivePair(rates []OfferRate, base, quote string) (decimal.Decimal, time.Time, error) {
	if base == quote {
		return decimal.NewFromInt(1), time.Time{}, nil
	}
	for _, r := range rates {
		if r.Base == base && r.Quote == quote {
			v, e := decimal.NewFromString(r.Value)
			at, te := time.Parse(time.RFC3339Nano, r.At)
			if e == nil && te == nil && v.IsPositive() {
				return v, at, nil
			}
		}
	}
	for _, r := range rates {
		if r.Base == quote && r.Quote == base {
			v, e := decimal.NewFromString(r.Value)
			at, te := time.Parse(time.RFC3339Nano, r.At)
			if e == nil && te == nil && v.IsPositive() {
				return decimal.NewFromInt(1).DivRound(v, 24), at, nil
			}
		}
	}
	return decimal.Zero, time.Time{}, apierr.Invalid("OFFER_FX_REQUIRED", "请先确认所需币种的有效汇率").WithMeta("pair", base+"/"+quote)
}
func prepareOffer(b OfferBody, source OfferInquiry, rates []OfferRate, calculate bool, target string) (OfferBody, error) {
	b.Currency = strings.ToUpper(strings.TrimSpace(b.Currency))
	if !offerCurrency.MatchString(b.Currency) || strings.TrimSpace(b.Customer) == "" {
		return b, apierr.Invalid("OFFER_HEADER", "请填写客户和币种")
	}
	if err := validBusinessDate(b.ValidUntil, "OFFER_DATE", "报价有效期"); err != nil {
		return b, err
	}
	if len(b.Lines) == 0 {
		return b, apierr.Invalid("OFFER_LINES", "至少保留一项成交产品")
	}
	b.Rates = rates
	seen := map[string]bool{}
	qtyByID := map[string]decimal.Decimal{}
	total := decimal.Zero
	for i := range b.Lines {
		line := &b.Lines[i]
		if line.ID == "" || seen[line.ID] || strings.TrimSpace(line.Product) == "" || strings.TrimSpace(line.Unit) == "" {
			return b, apierr.Invalid("OFFER_PRODUCT", "请填写产品、单位，产品行标识不能重复")
		}
		seen[line.ID] = true
		if line.FactoryQuoteID != "" {
			found := false
			for _, q := range source.Quotes {
				if q.ID == line.FactoryQuoteID && q.Kind == "PROCUREMENT" {
					var qb struct {
						Currency string `json:"currency"`
						Prices   []struct {
							ProductID string `json:"productId"`
							Price     string `json:"price"`
						} `json:"prices"`
					}
					if err := json.Unmarshal(q.Body, &qb); err != nil {
						return b, err
					}
					for _, p := range qb.Prices {
						if p.ProductID == line.ID {
							found = true
							line.Calculation.Factory = p.Price
							if line.Calculation.Formula > 0 && (!calculate || target == "*" || line.ID == target) {
								expected := "CNY"
								if line.Calculation.Formula == 2 {
									expected = "USD"
								}
								conversion, _, e := effectivePair(rates, qb.Currency, expected)
								if e != nil {
									return b, e
								}
								price, e := decimal.NewFromString(p.Price)
								if e != nil {
									return b, e
								}
								// Currency conversion can produce a repeating decimal. Keep the
								// internal value within the same eight-decimal precision accepted
								// for user-entered calculation inputs.
								line.Calculation.Factory = price.Mul(conversion).Round(8).String()
							}
						}
					}
				}
			}
			if !found {
				return b, apierr.Invalid("OFFER_SOURCE", "所选工厂报价不包含该产品，请刷新后选择")
			}
		}
		if line.Calculation.Formula > 0 && (!calculate || target == "*" || line.ID == target) {
			rate := ""
			if line.Calculation.Formula != 2 {
				v, _, e := effectivePair(rates, "USD", "CNY")
				if e != nil {
					return b, e
				}
				rate = v.String()
			}
			computed, e := calculateOfferRaw(line.Calculation, rate)
			if e != nil {
				return b, e
			}
			conversion, _, e := effectivePair(rates, "USD", b.Currency)
			if e != nil {
				return b, e
			}
			line.CalculatedPrice = computed.Mul(conversion).StringFixed(2)
			if calculate && (target == "*" || line.ID == target) {
				line.UnitPrice = line.CalculatedPrice
			}
		}
		amount, e := offerLineAmount(line.Quantity, line.UnitPrice)
		if e != nil {
			if calculate && target != "*" && line.ID != target {
				continue
			}
			return b, e
		}
		q, _ := decimal.NewFromString(line.Quantity)
		if q.Exponent() < -4 {
			return b, apierr.Invalid("OFFER_QUANTITY_PRECISION", "数量最多保留四位小数")
		}
		qtyByID[line.ID] = q
		line.Amount = amount
		p, _ := decimal.NewFromString(line.UnitPrice)
		line.UnitPrice = p.StringFixed(2)
		v, _ := decimal.NewFromString(amount)
		total = total.Add(v)
	}
	assigned := map[string]decimal.Decimal{}
	transportIDs := map[string]bool{}
	for index := range b.Transports {
		tr := &b.Transports[index]
		tr.Currency = strings.ToUpper(strings.TrimSpace(tr.Currency))
		if !offerCurrency.MatchString(tr.Currency) {
			return b, apierr.Invalid("OFFER_TRANSPORT_CURRENCY", "请填写运输方案币种")
		}
		if tr.Price != "" {
			amount, e := offerLineAmount("1", tr.Price)
			if e != nil {
				return b, apierr.Invalid("OFFER_TRANSPORT_PRICE", "运输方案费用必须是非负金额")
			}
			tr.Price = amount
		}
		found := false
		for _, sourceQuote := range source.Quotes {
			if sourceQuote.ID == tr.QuoteID && sourceQuote.Kind == "LOGISTICS" {
				found = true
			}
		}
		if !found || transportIDs[tr.QuoteID] {
			return b, apierr.Invalid("OFFER_TRANSPORT", "请选择有效且不重复的运输方案")
		}
		transportIDs[tr.QuoteID] = true
		if tr.Accepted {
			if len(tr.Quantities) == 0 {
				return b, apierr.Invalid("OFFER_TRANSPORT_QUANTITY", "请填写接受方案对应的产品数量")
			}
			for id, value := range tr.Quantities {
				q, e := decimal.NewFromString(value)
				limit, exists := qtyByID[id]
				if e != nil || !q.IsPositive() || !exists {
					return b, apierr.Invalid("OFFER_TRANSPORT_QUANTITY", "运输产品或数量无效")
				}
				assigned[id] = assigned[id].Add(q)
				if assigned[id].GreaterThan(limit) {
					return b, apierr.Invalid("OFFER_TRANSPORT_EXCESS", "各运输方案分配数量合计不能超过成交数量")
				}
			}
		}
	}
	if total.GreaterThan(decimal.RequireFromString("9999999999999999.99")) {
		return b, apierr.Invalid("OFFER_TOTAL_LIMIT", "报价总金额超出支持范围")
	}
	b.Total = total.StringFixed(2)
	return b, nil
}
