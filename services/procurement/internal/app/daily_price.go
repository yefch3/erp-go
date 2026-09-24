package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/shopspring/decimal"
)

// DailyPriceCommand is deliberately closed by the gRPC adapter. One save is
// one transaction, so Tab-editing twenty cells never leaves a half-day behind.
type DailyPriceCommand struct {
	Action          string             `json:"action"`
	Date            string             `json:"date"`
	From            string             `json:"from"`
	To              string             `json:"to"`
	ProductID       int64              `json:"productId"`
	SupplierIDs     []int64            `json:"supplierIds"`
	SpreadProductID int64              `json:"spreadProductId"`
	Kind            string             `json:"kind"`
	ID              int64              `json:"id"`
	MasterID        int64              `json:"masterId"`
	Name            string             `json:"name"`
	Note            string             `json:"note"`
	SortOrder       int32              `json:"sortOrder"`
	Active          *bool              `json:"active"`
	Prices          []DailyPriceInput  `json:"prices"`
	Spreads         []DailySpreadInput `json:"spreads"`
}
type DailyPriceInput struct {
	ProductID  int64  `json:"productId"`
	SupplierID int64  `json:"supplierId"`
	Price      string `json:"price"`
	Remark     string `json:"remark"`
}
type DailySpreadInput struct {
	ProductID int64  `json:"productId"`
	Spot      string `json:"spot"`
	Futures   string `json:"futures"`
}
type DailyDimension struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	MasterID  int64  `json:"masterId"`
	Name      string `json:"name"`
	Note      string `json:"note"`
	SortOrder int32  `json:"sortOrder"`
	Active    bool   `json:"active"`
}
type DailyPriceRecord struct {
	ID            int64     `json:"id"`
	Date          string    `json:"date"`
	ProductID     int64     `json:"productId"`
	ProductName   string    `json:"productName"`
	SupplierID    int64     `json:"supplierId"`
	SupplierName  string    `json:"supplierName"`
	Price         string    `json:"price"`
	PreviousPrice string    `json:"previousPrice"`
	Remark        string    `json:"remark"`
	CreatedBy     int64     `json:"createdBy"`
	CreatedByName string    `json:"createdByName"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedBy     int64     `json:"updatedBy"`
	UpdatedByName string    `json:"updatedByName"`
	UpdatedAt     time.Time `json:"updatedAt"`
}
type DailySpreadRecord struct {
	ID            int64     `json:"id"`
	Date          string    `json:"date"`
	ProductID     int64     `json:"productId"`
	ProductName   string    `json:"productName"`
	Spot          *string   `json:"spot"`
	Futures       *string   `json:"futures"`
	Basis         *string   `json:"basis"`
	BasisPercent  *string   `json:"basisPercent"`
	CreatedBy     int64     `json:"createdBy"`
	CreatedByName string    `json:"createdByName"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedBy     int64     `json:"updatedBy"`
	UpdatedByName string    `json:"updatedByName"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func dailyNumber(s string) (*decimal.Decimal, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	v, err := decimal.NewFromString(s)
	if err != nil || v.IsNegative() || v.Exponent() < -4 || v.GreaterThan(decimal.NewFromInt(99999999999999)) {
		return nil, apierr.Invalid("DAILY_PRICE_NUMBER", "价格必须是非负数，最多四位小数")
	}
	return &v, nil
}

func basisValues(spot, futures *string) (*string, *string) {
	if spot == nil || futures == nil {
		return nil, nil
	}
	s, e1 := decimal.NewFromString(*spot)
	f, e2 := decimal.NewFromString(*futures)
	if e1 != nil || e2 != nil {
		return nil, nil
	}
	b := s.Sub(f).String()
	if f.IsZero() {
		return &b, nil
	}
	p := s.Sub(f).Div(f).Mul(decimal.NewFromInt(100)).Round(2).StringFixed(2)
	return &b, &p
}

func (s *Service) DailyPrice(ctx context.Context, tenant int64, op Operator, in DailyPriceCommand) (any, error) {
	if tenant <= 0 || op.ID <= 0 {
		return nil, apierr.Unauthorized("DAILY_PRICE_IDENTITY", "请先登录")
	}
	permission := "procurement:daily-price:read"
	switch in.Action {
	case "savePrices", "saveSpreads":
		permission = "procurement:daily-price:write"
	case "configure", "deleteDimension":
		permission = "procurement:daily-price:manage"
	case "deletePrice", "deleteSpread":
		permission = "procurement:daily-price:delete"
	case "config", "day", "trend":
	default:
		return nil, apierr.Invalid("DAILY_PRICE_ACTION", "不支持的每日基价操作")
	}
	if s.permissions == nil {
		return nil, apierr.Internal("DAILY_PRICE_AUTH", "权限服务不可用")
	}
	allowed, err := s.permissions.Allowed(ctx, op.ID, permission)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, apierr.Permission("DAILY_PRICE_FORBIDDEN", "没有执行此操作的权限")
	}
	switch in.Action {
	case "config":
		return s.dailyConfig(ctx, tenant)
	case "day":
		if err := validBusinessDate(in.Date, "DAILY_PRICE_DATE", "日期"); err != nil {
			return nil, err
		}
		if in.Date == "" {
			return nil, apierr.Invalid("DAILY_PRICE_DATE", "请选择日期")
		}
		return s.dailyDay(ctx, tenant, in.Date)
	case "trend":
		if err := validDailyRange(in.From, in.To); err != nil {
			return nil, err
		}
		if len(in.SupplierIDs) > 12 {
			return nil, apierr.Invalid("DAILY_PRICE_SUPPLIERS", "最多比较 12 家供应商")
		}
		return s.dailyTrend(ctx, tenant, in)
	case "savePrices", "saveSpreads":
		return s.saveDaily(ctx, tenant, op, in)
	case "configure":
		return s.configureDaily(ctx, tenant, in)
	case "deleteDimension":
		return s.deleteDailyDimension(ctx, tenant, in.ID)
	case "deletePrice", "deleteSpread":
		return s.deleteDaily(ctx, tenant, op, in)
	default:
		return nil, apierr.Invalid("DAILY_PRICE_ACTION", "不支持的每日基价操作")
	}
}

func validDailyRange(from, to string) error {
	if err := validBusinessDate(from, "DAILY_PRICE_RANGE", "开始日期"); err != nil {
		return err
	}
	if err := validBusinessDate(to, "DAILY_PRICE_RANGE", "结束日期"); err != nil {
		return err
	}
	a, _ := time.Parse("2006-01-02", from)
	b, _ := time.Parse("2006-01-02", to)
	if from == "" || to == "" || a.After(b) || b.Sub(a) > 366*5*24*time.Hour {
		return apierr.Invalid("DAILY_PRICE_RANGE", "日期范围无效或超过五年")
	}
	return nil
}

func (s *Service) dailyConfig(ctx context.Context, tenant int64) (any, error) {
	// The daily sheet needs rows and columns on first use. These are editable
	// market labels, not product or supplier master records and not price data.
	// A tenant that has already set up its own sheet is left untouched.
	_, err := s.pool.Exec(ctx, `INSERT INTO daily_price_dimensions(tenant_id,kind,name,note,sort_order)
		SELECT $1, defaults.kind, defaults.name, defaults.note, defaults.sort_order
		FROM (VALUES
			('PRODUCT','热卷','',0),('PRODUCT','冷卷','',1),('PRODUCT','镀锌','',2),('PRODUCT','SAE1006/1008 6.5mm','',3),
			('SUPPLIER','东钢','出厂价',0),('SUPPLIER','纵横','出厂价',1),('SUPPLIER','新天钢','出厂价',2),('SUPPLIER','智融','出厂价',3),('SUPPLIER','神龙','鲅鱼圈',4),('SUPPLIER','澳森','天津港',5),
			('SPREAD','铝','',0),('SPREAD','锌','',1),('SPREAD','螺纹钢','',2),('SPREAD','热卷','',3),('SPREAD','焦煤','',4),('SPREAD','铁矿石','',5)
		) AS defaults(kind,name,note,sort_order)
		WHERE NOT EXISTS (SELECT 1 FROM daily_price_dimensions WHERE tenant_id=$1)
		ON CONFLICT (tenant_id,kind,name) DO NOTHING`, tenant)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,kind,COALESCE(master_id,0),name,note,sort_order,active FROM daily_price_dimensions WHERE tenant_id=$1 ORDER BY kind,sort_order,id`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DailyDimension{}
	for rows.Next() {
		var d DailyDimension
		if err := rows.Scan(&d.ID, &d.Kind, &d.MasterID, &d.Name, &d.Note, &d.SortOrder, &d.Active); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return map[string]any{"dimensions": out}, rows.Err()
}

func (s *Service) configureDaily(ctx context.Context, tenant int64, in DailyPriceCommand) (any, error) {
	if in.Kind != "PRODUCT" && in.Kind != "SUPPLIER" && in.Kind != "SPREAD" {
		return nil, apierr.Invalid("DAILY_PRICE_KIND", "配置类型无效")
	}
	name := strings.TrimSpace(in.Name)
	note := strings.TrimSpace(in.Note)
	if len([]rune(note)) > 200 || (in.Kind != "SUPPLIER" && note != "") {
		return nil, apierr.Invalid("DAILY_PRICE_NOTE", "报价说明最多 200 字且仅用于钢厂")
	}
	if in.ID == 0 {
		if in.MasterID < 0 {
			return nil, apierr.Invalid("DAILY_PRICE_MASTER", "主数据 ID 无效")
		}
		if in.MasterID > 0 {
			if in.Kind == "PRODUCT" {
				if s.products == nil {
					return nil, apierr.Internal("DAILY_PRICE_PRODUCT", "产品服务不可用")
				}
				p, err := s.products.Get(ctx, in.MasterID)
				if err != nil {
					return nil, err
				}
				if p.ID != in.MasterID || p.Status != "ACTIVE" {
					return nil, apierr.Invalid("DAILY_PRICE_PRODUCT", "产品不存在或已停用")
				}
				name = p.Name
			} else if in.Kind == "SUPPLIER" {
				p, err := s.suppliers.Get(ctx, in.MasterID)
				if err != nil {
					return nil, err
				}
				if p.ID != in.MasterID || p.Status != "ACTIVE" {
					return nil, apierr.Invalid("DAILY_PRICE_SUPPLIER", "供应商不存在或已停用")
				}
				name = p.Name
			} else {
				return nil, apierr.Invalid("DAILY_PRICE_MASTER", "基差品种不使用主数据 ID")
			}
			// Re-selecting an existing master record should restore its display row,
			// including after a master-data rename, rather than colliding on ID.
			ct, err := s.pool.Exec(ctx, `UPDATE daily_price_dimensions SET active=TRUE,sort_order=$4,updated_at=now() WHERE tenant_id=$1 AND kind=$2 AND master_id=$3`, tenant, in.Kind, in.MasterID, in.SortOrder)
			if err != nil {
				return nil, err
			}
			if ct.RowsAffected() > 0 {
				return s.dailyConfig(ctx, tenant)
			}
		}
		if name == "" || len([]rune(name)) > 200 {
			return nil, apierr.Invalid("DAILY_PRICE_NAME", "请输入 1 至 200 字的名称")
		}
		_, err := s.pool.Exec(ctx, `INSERT INTO daily_price_dimensions(tenant_id,kind,master_id,name,note,sort_order) VALUES($1,$2,NULLIF($3,0),$4,$5,$6) ON CONFLICT (tenant_id,kind,name) DO UPDATE SET active=TRUE,master_id=COALESCE(daily_price_dimensions.master_id,EXCLUDED.master_id),sort_order=EXCLUDED.sort_order,updated_at=now()`, tenant, in.Kind, in.MasterID, name, note, in.SortOrder)
		if err != nil {
			return nil, err
		}
	} else {
		if in.Active == nil {
			return nil, apierr.Invalid("DAILY_PRICE_ACTIVE", "缺少显示状态")
		}
		ct, err := s.pool.Exec(ctx, `UPDATE daily_price_dimensions SET active=$3,sort_order=$4,note=CASE WHEN $5='SUPPLIER' THEN $6 ELSE note END,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND kind=$5`, tenant, in.ID, *in.Active, in.SortOrder, in.Kind, note)
		if err != nil {
			return nil, err
		}
		if ct.RowsAffected() == 0 {
			return nil, apierr.NotFound("DAILY_PRICE_CONFIG_NOT_FOUND", "配置不存在")
		}
	}
	return s.dailyConfig(ctx, tenant)
}

func (s *Service) deleteDailyDimension(ctx context.Context, tenant, id int64) (any, error) {
	if id <= 0 {
		return nil, apierr.Invalid("DAILY_PRICE_DIMENSION_ID", "品种或钢厂 ID 无效")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var kind string
	if err = tx.QueryRow(ctx, `SELECT kind FROM daily_price_dimensions WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id).Scan(&kind); err == pgx.ErrNoRows {
		return nil, apierr.NotFound("DAILY_PRICE_DIMENSION_NOT_FOUND", "品种或钢厂不存在")
	} else if err != nil {
		return nil, err
	}
	var used bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM daily_base_prices WHERE tenant_id=$1 AND (product_id=$2 OR supplier_id=$2)) OR EXISTS(SELECT 1 FROM daily_basis_spreads WHERE tenant_id=$1 AND product_id=$2)`, tenant, id).Scan(&used); err != nil {
		return nil, err
	}
	if used {
		return nil, apierr.Conflict("DAILY_PRICE_DIMENSION_USED", "该品种或钢厂已有历史数据，不能删除；可以关闭显示")
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM daily_price_dimensions WHERE tenant_id=$1`, tenant).Scan(&count); err != nil {
		return nil, err
	}
	if count <= 1 {
		return nil, apierr.Conflict("DAILY_PRICE_DIMENSION_LAST", "请至少保留一个品种或钢厂")
	}
	if _, err = tx.Exec(ctx, `DELETE FROM daily_price_dimensions WHERE tenant_id=$1 AND id=$2`, tenant, id); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"deleted": true, "kind": kind}, nil
}

func (s *Service) dailyDay(ctx context.Context, tenant int64, date string) (any, error) {
	prices := []DailyPriceRecord{}
	previous := map[string]string{}
	previousRows, err := s.pool.Query(ctx, `SELECT DISTINCT ON(product_id,supplier_id) product_id,supplier_id,price::text
      FROM daily_base_prices WHERE tenant_id=$1 AND price_date<$2::date AND deleted_at IS NULL
      ORDER BY product_id,supplier_id,price_date DESC`, tenant, date)
	if err != nil {
		return nil, err
	}
	for previousRows.Next() {
		var productID, supplierID int64
		var price string
		if err = previousRows.Scan(&productID, &supplierID, &price); err != nil {
			previousRows.Close()
			return nil, err
		}
		previous[fmt.Sprintf("%d:%d", productID, supplierID)] = price
	}
	err = previousRows.Err()
	previousRows.Close()
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT p.id,p.price_date::text,p.product_id,d.name,p.supplier_id,v.name,p.price::text,
      COALESCE((SELECT q.price::text FROM daily_base_prices q WHERE q.tenant_id=p.tenant_id AND q.product_id=p.product_id AND q.supplier_id=p.supplier_id AND q.price_date<p.price_date AND q.deleted_at IS NULL ORDER BY q.price_date DESC LIMIT 1),''),
      p.remark,p.created_by,p.created_by_name,p.created_at,p.updated_by,p.updated_by_name,p.updated_at
      FROM daily_base_prices p JOIN daily_price_dimensions d ON d.tenant_id=p.tenant_id AND d.id=p.product_id
      JOIN daily_price_dimensions v ON v.tenant_id=p.tenant_id AND v.id=p.supplier_id
      WHERE p.tenant_id=$1 AND p.price_date=$2::date AND p.deleted_at IS NULL ORDER BY d.sort_order,v.sort_order`, tenant, date)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p DailyPriceRecord
		if err = rows.Scan(&p.ID, &p.Date, &p.ProductID, &p.ProductName, &p.SupplierID, &p.SupplierName, &p.Price, &p.PreviousPrice, &p.Remark, &p.CreatedBy, &p.CreatedByName, &p.CreatedAt, &p.UpdatedBy, &p.UpdatedByName, &p.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		prices = append(prices, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	spreads := []DailySpreadRecord{}
	rows, err = s.pool.Query(ctx, `SELECT p.id,p.price_date::text,p.product_id,d.name,p.spot::text,p.futures::text,p.created_by,p.created_by_name,p.created_at,p.updated_by,p.updated_by_name,p.updated_at
      FROM daily_basis_spreads p JOIN daily_price_dimensions d ON d.tenant_id=p.tenant_id AND d.id=p.product_id
      WHERE p.tenant_id=$1 AND p.price_date=$2::date AND p.deleted_at IS NULL ORDER BY d.sort_order`, tenant, date)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p DailySpreadRecord
		if err = rows.Scan(&p.ID, &p.Date, &p.ProductID, &p.ProductName, &p.Spot, &p.Futures, &p.CreatedBy, &p.CreatedByName, &p.CreatedAt, &p.UpdatedBy, &p.UpdatedByName, &p.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		p.Basis, p.BasisPercent = basisValues(p.Spot, p.Futures)
		spreads = append(spreads, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return map[string]any{"prices": prices, "spreads": spreads, "previous": previous}, nil
}

func (s *Service) dailyTrend(ctx context.Context, tenant int64, in DailyPriceCommand) (any, error) {
	prices := []DailyPriceRecord{}
	rows, err := s.pool.Query(ctx, `SELECT p.id,p.price_date::text,p.product_id,d.name,p.supplier_id,v.name,p.price::text,
      COALESCE((SELECT q.price::text FROM daily_base_prices q WHERE q.tenant_id=p.tenant_id AND q.product_id=p.product_id AND q.supplier_id=p.supplier_id AND q.price_date<p.price_date AND q.deleted_at IS NULL ORDER BY q.price_date DESC LIMIT 1),''),
      p.remark,p.created_by,p.created_by_name,p.created_at,p.updated_by,p.updated_by_name,p.updated_at
      FROM daily_base_prices p JOIN daily_price_dimensions d ON d.tenant_id=p.tenant_id AND d.id=p.product_id
      JOIN daily_price_dimensions v ON v.tenant_id=p.tenant_id AND v.id=p.supplier_id
      WHERE p.tenant_id=$1 AND p.price_date BETWEEN $2::date AND $3::date AND p.product_id=$4 AND p.supplier_id=ANY($5) AND p.deleted_at IS NULL ORDER BY p.price_date`, tenant, in.From, in.To, in.ProductID, in.SupplierIDs)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p DailyPriceRecord
		if err = rows.Scan(&p.ID, &p.Date, &p.ProductID, &p.ProductName, &p.SupplierID, &p.SupplierName, &p.Price, &p.PreviousPrice, &p.Remark, &p.CreatedBy, &p.CreatedByName, &p.CreatedAt, &p.UpdatedBy, &p.UpdatedByName, &p.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		prices = append(prices, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	spreads := []DailySpreadRecord{}
	rows, err = s.pool.Query(ctx, `SELECT p.id,p.price_date::text,p.product_id,d.name,p.spot::text,p.futures::text,p.created_by,p.created_by_name,p.created_at,p.updated_by,p.updated_by_name,p.updated_at
      FROM daily_basis_spreads p JOIN daily_price_dimensions d ON d.tenant_id=p.tenant_id AND d.id=p.product_id
      WHERE p.tenant_id=$1 AND p.price_date BETWEEN $2::date AND $3::date AND p.product_id=$4 AND p.deleted_at IS NULL ORDER BY p.price_date`, tenant, in.From, in.To, in.SpreadProductID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p DailySpreadRecord
		if err = rows.Scan(&p.ID, &p.Date, &p.ProductID, &p.ProductName, &p.Spot, &p.Futures, &p.CreatedBy, &p.CreatedByName, &p.CreatedAt, &p.UpdatedBy, &p.UpdatedByName, &p.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		p.Basis, p.BasisPercent = basisValues(p.Spot, p.Futures)
		spreads = append(spreads, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return map[string]any{"prices": prices, "spreads": spreads}, nil
}

func (s *Service) dailyEditor(ctx context.Context, op Operator) (Visibility, error) {
	if s.scopes == nil {
		return Visibility{EmployeeIDs: []int64{op.ID}}, nil
	}
	return s.scopes.VisibleEmployees(ctx, op.ID, "daily_base_price")
}

func (s *Service) saveDaily(ctx context.Context, tenant int64, op Operator, in DailyPriceCommand) (any, error) {
	if err := validBusinessDate(in.Date, "DAILY_PRICE_DATE", "日期"); err != nil {
		return nil, err
	}
	if in.Date == "" {
		return nil, apierr.Invalid("DAILY_PRICE_DATE", "请选择日期")
	}
	if len(in.Prices) > 1000 || len(in.Spreads) > 500 {
		return nil, apierr.Invalid("DAILY_PRICE_BATCH", "一次保存的数据过多")
	}
	visibility, err := s.dailyEditor(ctx, op)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if in.Action == "savePrices" {
		seen := map[string]bool{}
		for _, v := range in.Prices {
			key := fmt.Sprintf("%d:%d", v.ProductID, v.SupplierID)
			if seen[key] {
				return nil, apierr.Invalid("DAILY_PRICE_DUPLICATE", "同一产品和供应商重复提交")
			}
			seen[key] = true
			if v.ProductID <= 0 || v.SupplierID <= 0 || len([]rune(v.Remark)) > 500 {
				return nil, apierr.Invalid("DAILY_PRICE_INPUT", "产品、供应商或备注无效")
			}
			var valid bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM daily_price_dimensions d JOIN daily_price_dimensions v ON v.tenant_id=d.tenant_id WHERE d.tenant_id=$1 AND d.id=$2 AND d.kind='PRODUCT' AND v.id=$3 AND v.kind='SUPPLIER')`, tenant, v.ProductID, v.SupplierID).Scan(&valid); err != nil {
				return nil, err
			}
			if !valid {
				return nil, apierr.Invalid("DAILY_PRICE_DIMENSION", "产品或供应商不属于当前公司")
			}
			var owner int64
			err = tx.QueryRow(ctx, `SELECT created_by FROM daily_base_prices WHERE tenant_id=$1 AND price_date=$2::date AND product_id=$3 AND supplier_id=$4 FOR UPDATE`, tenant, in.Date, v.ProductID, v.SupplierID).Scan(&owner)
			if err != nil && err != pgx.ErrNoRows {
				return nil, err
			}
			if err == nil && !ownerVisible(visibility, owner) {
				return nil, apierr.Permission("DAILY_PRICE_OWNER", "只能修改本人权限范围内的价格")
			}
			price, e := dailyNumber(v.Price)
			if e != nil {
				return nil, e
			}
			if price == nil {
				if err == nil {
					_, err = tx.Exec(ctx, `UPDATE daily_base_prices SET deleted_at=now(),updated_by=$5,updated_by_name=$6,updated_at=now() WHERE tenant_id=$1 AND price_date=$2::date AND product_id=$3 AND supplier_id=$4`, tenant, in.Date, v.ProductID, v.SupplierID, op.ID, op.Name)
				} else {
					err = nil
				}
			} else {
				ct, execErr := tx.Exec(ctx, `INSERT INTO daily_base_prices(tenant_id,price_date,product_id,supplier_id,price,remark,created_by,created_by_name,updated_by,updated_by_name) VALUES($1,$2::date,$3,$4,$5,$6,$7,$8,$7,$8) ON CONFLICT(tenant_id,price_date,product_id,supplier_id) DO UPDATE SET price=EXCLUDED.price,remark=EXCLUDED.remark,updated_by=EXCLUDED.updated_by,updated_by_name=EXCLUDED.updated_by_name,created_by=CASE WHEN daily_base_prices.deleted_at IS NOT NULL THEN EXCLUDED.created_by ELSE daily_base_prices.created_by END,created_by_name=CASE WHEN daily_base_prices.deleted_at IS NOT NULL THEN EXCLUDED.created_by_name ELSE daily_base_prices.created_by_name END,created_at=CASE WHEN daily_base_prices.deleted_at IS NOT NULL THEN now() ELSE daily_base_prices.created_at END,updated_at=now(),deleted_at=NULL WHERE daily_base_prices.deleted_at IS NOT NULL OR $9 OR daily_base_prices.created_by=ANY($10)`, tenant, in.Date, v.ProductID, v.SupplierID, *price, strings.TrimSpace(v.Remark), op.ID, op.Name, visibility.All, visibility.EmployeeIDs)
				err = execErr
				if err == nil && ct.RowsAffected() == 0 {
					return nil, apierr.Permission("DAILY_PRICE_OWNER", "只能修改本人权限范围内的价格")
				}
			}
			if err != nil {
				return nil, err
			}
		}
	} else {
		seen := map[int64]bool{}
		for _, v := range in.Spreads {
			if v.ProductID <= 0 || seen[v.ProductID] {
				return nil, apierr.Invalid("DAILY_SPREAD_DUPLICATE", "基差品种重复或无效")
			}
			seen[v.ProductID] = true
			var valid bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM daily_price_dimensions WHERE tenant_id=$1 AND id=$2 AND kind='SPREAD')`, tenant, v.ProductID).Scan(&valid); err != nil {
				return nil, err
			}
			if !valid {
				return nil, apierr.Invalid("DAILY_SPREAD_DIMENSION", "基差品种不属于当前公司")
			}
			var owner int64
			err = tx.QueryRow(ctx, `SELECT created_by FROM daily_basis_spreads WHERE tenant_id=$1 AND price_date=$2::date AND product_id=$3 FOR UPDATE`, tenant, in.Date, v.ProductID).Scan(&owner)
			if err != nil && err != pgx.ErrNoRows {
				return nil, err
			}
			if err == nil && !ownerVisible(visibility, owner) {
				return nil, apierr.Permission("DAILY_SPREAD_OWNER", "只能修改本人权限范围内的基差")
			}
			spot, e := dailyNumber(v.Spot)
			if e != nil {
				return nil, e
			}
			future, e := dailyNumber(v.Futures)
			if e != nil {
				return nil, e
			}
			if spot == nil && future == nil {
				if err == nil {
					_, err = tx.Exec(ctx, `UPDATE daily_basis_spreads SET deleted_at=now(),updated_by=$4,updated_by_name=$5,updated_at=now() WHERE tenant_id=$1 AND price_date=$2::date AND product_id=$3`, tenant, in.Date, v.ProductID, op.ID, op.Name)
				} else {
					err = nil
				}
			} else {
				ct, execErr := tx.Exec(ctx, `INSERT INTO daily_basis_spreads(tenant_id,price_date,product_id,spot,futures,created_by,created_by_name,updated_by,updated_by_name) VALUES($1,$2::date,$3,$4,$5,$6,$7,$6,$7) ON CONFLICT(tenant_id,price_date,product_id) DO UPDATE SET spot=EXCLUDED.spot,futures=EXCLUDED.futures,updated_by=EXCLUDED.updated_by,updated_by_name=EXCLUDED.updated_by_name,created_by=CASE WHEN daily_basis_spreads.deleted_at IS NOT NULL THEN EXCLUDED.created_by ELSE daily_basis_spreads.created_by END,created_by_name=CASE WHEN daily_basis_spreads.deleted_at IS NOT NULL THEN EXCLUDED.created_by_name ELSE daily_basis_spreads.created_by_name END,created_at=CASE WHEN daily_basis_spreads.deleted_at IS NOT NULL THEN now() ELSE daily_basis_spreads.created_at END,updated_at=now(),deleted_at=NULL WHERE daily_basis_spreads.deleted_at IS NOT NULL OR $8 OR daily_basis_spreads.created_by=ANY($9)`, tenant, in.Date, v.ProductID, spot, future, op.ID, op.Name, visibility.All, visibility.EmployeeIDs)
				err = execErr
				if err == nil && ct.RowsAffected() == 0 {
					return nil, apierr.Permission("DAILY_SPREAD_OWNER", "只能修改本人权限范围内的基差")
				}
			}
			if err != nil {
				return nil, err
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.dailyDay(ctx, tenant, in.Date)
}

func (s *Service) deleteDaily(ctx context.Context, tenant int64, op Operator, in DailyPriceCommand) (any, error) {
	if in.ID <= 0 {
		return nil, apierr.Invalid("DAILY_PRICE_ID", "记录 ID 无效")
	}
	table := "daily_base_prices"
	if in.Action == "deleteSpread" {
		table = "daily_basis_spreads"
	}
	ct, err := s.pool.Exec(ctx, fmt.Sprintf(`UPDATE %s SET deleted_at=now(),updated_by=$3,updated_by_name=$4,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, table), tenant, in.ID, op.ID, op.Name)
	if err != nil {
		return nil, err
	}
	if ct.RowsAffected() == 0 {
		return nil, apierr.NotFound("DAILY_PRICE_NOT_FOUND", "记录不存在")
	}
	return map[string]any{"deleted": true}, nil
}
