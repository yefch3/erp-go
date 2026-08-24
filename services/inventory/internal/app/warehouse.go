package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/inventory/internal/store"
)

// WarehouseContactInput 表示仓库的内部负责人或外部联系人。
type WarehouseContactInput struct {
	ContactType string
	EmployeeID  int64
	Name        string
	Phone       string
	Email       string
	IsPrimary   bool
	Status      string
}

// WarehouseInput 是仓库档案的统一写入模型，供创建和编辑共用。
type WarehouseInput struct {
	Code, Name, ProfileType, Address string
	CountryCode, City, Timezone      string
	AccountingMode, Status, Reason   string
	Contacts                         []WarehouseContactInput
}

type WarehouseProfile struct {
	store.GetWarehouseRow
	Contacts []store.ListWarehouseContactsRow
}

type WarehouseSettingsInput struct {
	UsageMode           string
	AllowDirectDelivery bool
	AllowInventory      bool
	DefaultWarehouseID  int64
}

// GetWarehouseProfile 读取仓库档案及联系人，避免页面分别请求后产生不一致。
func (s *Service) GetWarehouseProfile(ctx context.Context, tenantID, id int64) (WarehouseProfile, error) {
	row, err := s.q.GetWarehouse(ctx, store.GetWarehouseParams{TenantID: tenantID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return WarehouseProfile{}, apierr.NotFound("WAREHOUSE_NOT_FOUND", "仓库不存在")
	}
	if err != nil {
		return WarehouseProfile{}, err
	}
	contacts, err := s.q.ListWarehouseContacts(ctx, store.ListWarehouseContactsParams{TenantID: tenantID, WarehouseID: id})
	if err != nil {
		return WarehouseProfile{}, err
	}
	return WarehouseProfile{GetWarehouseRow: row, Contacts: contacts}, nil
}

// CreateWarehouse 创建仓库档案、联系人和首条变更记录。
func (s *Service) CreateWarehouse(ctx context.Context, tenantID int64, in WarehouseInput, op Operator) (WarehouseProfile, error) {
	in, err := normalizeWarehouseInput(in)
	if err != nil {
		return WarehouseProfile{}, err
	}
	var id int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		id, err = q.CreateWarehouse(ctx, createWarehouseParams(tenantID, in))
		if err != nil {
			return err
		}
		if err = replaceWarehouseContacts(ctx, q, tenantID, id, in.Contacts); err != nil {
			return err
		}
		after, _ := json.Marshal(in)
		return q.CreateWarehouseHistory(ctx, store.CreateWarehouseHistoryParams{
			TenantID: tenantID, WarehouseID: id, Action: "CREATE", BeforeData: []byte("{}"), AfterData: after,
			Reason: in.Reason, ChangedBy: op.ID, ChangedByName: op.Name,
		})
	})
	if err != nil {
		return WarehouseProfile{}, err
	}
	return s.GetWarehouseProfile(ctx, tenantID, id)
}

// UpdateWarehouse 更新完整档案，并保存修改前后快照以供审计。
func (s *Service) UpdateWarehouse(ctx context.Context, tenantID, id int64, in WarehouseInput, op Operator) (WarehouseProfile, error) {
	in, err := normalizeWarehouseInput(in)
	if err != nil {
		return WarehouseProfile{}, err
	}
	before, err := s.GetWarehouseProfile(ctx, tenantID, id)
	if err != nil {
		return WarehouseProfile{}, err
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.UpdateWarehouse(ctx, store.UpdateWarehouseParams{
			Code: in.Code, Name: in.Name, WhType: profileToWHType(in.ProfileType), Address: in.Address,
			Status: in.Status, ProfileType: in.ProfileType, CountryCode: in.CountryCode, City: in.City,
			Timezone: in.Timezone, AccountingMode: in.AccountingMode, TenantID: tenantID, ID: id,
		}); err != nil {
			return err
		}
		if err := replaceWarehouseContacts(ctx, q, tenantID, id, in.Contacts); err != nil {
			return err
		}
		beforeJSON, _ := json.Marshal(before)
		afterJSON, _ := json.Marshal(in)
		return q.CreateWarehouseHistory(ctx, store.CreateWarehouseHistoryParams{
			TenantID: tenantID, WarehouseID: id, Action: "UPDATE", BeforeData: beforeJSON, AfterData: afterJSON,
			Reason: in.Reason, ChangedBy: op.ID, ChangedByName: op.Name,
		})
	})
	if err != nil {
		return WarehouseProfile{}, err
	}
	return s.GetWarehouseProfile(ctx, tenantID, id)
}

func (s *Service) GetWarehouseSettings(ctx context.Context, tenantID int64) (store.GetWarehouseSettingsRow, error) {
	row, err := s.q.GetWarehouseSettings(ctx, tenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.GetWarehouseSettingsRow{UsageMode: "USE_WAREHOUSE", AllowDirectDelivery: true, AllowInventory: true}, nil
	}
	return row, err
}

// UpdateWarehouseSettings 保存公司级仓库使用模式；无仓库模式会自动关闭库存核算。
func (s *Service) UpdateWarehouseSettings(ctx context.Context, tenantID int64, in WarehouseSettingsInput, op Operator) (store.GetWarehouseSettingsRow, error) {
	if !oneOf(in.UsageMode, "NO_WAREHOUSE", "USE_WAREHOUSE", "SELECT_PER_ORDER") {
		return store.GetWarehouseSettingsRow{}, apierr.Invalid("WAREHOUSE_USAGE_MODE_INVALID", "仓库使用模式无效")
	}
	if in.UsageMode == "NO_WAREHOUSE" {
		in.AllowInventory = false
		in.DefaultWarehouseID = 0
	}
	if in.DefaultWarehouseID != 0 {
		warehouse, err := s.GetWarehouseProfile(ctx, tenantID, in.DefaultWarehouseID)
		if err != nil {
			return store.GetWarehouseSettingsRow{}, err
		}
		if warehouse.Status != "ACTIVE" {
			return store.GetWarehouseSettingsRow{}, apierr.Conflict("WAREHOUSE_DEFAULT_INACTIVE", "默认仓库必须处于启用状态")
		}
	}
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		params := store.UpsertWarehouseSettingsParams{
			TenantID: tenantID, UsageMode: in.UsageMode, AllowDirectDelivery: in.AllowDirectDelivery,
			AllowInventory: in.AllowInventory, DefaultWarehouseID: in.DefaultWarehouseID, UpdatedBy: op.ID,
		}
		if err := q.UpsertWarehouseSettings(ctx, params); err != nil {
			return err
		}
		return q.CreateWarehouseSettingsHistory(ctx, store.CreateWarehouseSettingsHistoryParams{
			TenantID: tenantID, UsageMode: in.UsageMode, AllowDirectDelivery: in.AllowDirectDelivery,
			AllowInventory: in.AllowInventory, DefaultWarehouseID: in.DefaultWarehouseID, ChangedBy: op.ID,
		})
	})
	if err != nil {
		return store.GetWarehouseSettingsRow{}, err
	}
	return s.GetWarehouseSettings(ctx, tenantID)
}

func normalizeWarehouseInput(in WarehouseInput) (WarehouseInput, error) {
	in.Code, in.Name = strings.TrimSpace(in.Code), strings.TrimSpace(in.Name)
	if in.Code == "" || in.Name == "" {
		return in, apierr.Invalid("WAREHOUSE_REQUIRED", "仓库编码和名称不能为空")
	}
	if !oneOf(in.ProfileType, "OWN", "PORT", "THIRD_PARTY") {
		return in, apierr.Invalid("WAREHOUSE_PROFILE_TYPE_INVALID", "仓库类型无效")
	}
	if in.Timezone == "" {
		in.Timezone = "UTC"
	}
	if in.AccountingMode == "" {
		in.AccountingMode = "SYNC_INVENTORY"
	}
	if !oneOf(in.AccountingMode, "DOCUMENT_ONLY", "SYNC_INVENTORY") {
		return in, apierr.Invalid("WAREHOUSE_ACCOUNTING_MODE_INVALID", "库存核算模式无效")
	}
	if in.Status == "" {
		in.Status = "ACTIVE"
	}
	if !oneOf(in.Status, "ACTIVE", "INACTIVE") {
		return in, apierr.Invalid("WAREHOUSE_STATUS_INVALID", "仓库状态无效")
	}
	for i := range in.Contacts {
		if !oneOf(in.Contacts[i].ContactType, "OWNER", "CONTACT") || strings.TrimSpace(in.Contacts[i].Name) == "" {
			return in, apierr.Invalid("WAREHOUSE_CONTACT_INVALID", "联系人类型和姓名不能为空")
		}
		if in.Contacts[i].Status == "" {
			in.Contacts[i].Status = "ACTIVE"
		}
	}
	return in, nil
}

func createWarehouseParams(tenantID int64, in WarehouseInput) store.CreateWarehouseParams {
	return store.CreateWarehouseParams{TenantID: tenantID, Code: in.Code, Name: in.Name,
		WhType: profileToWHType(in.ProfileType), Address: in.Address, Status: in.Status,
		ProfileType: in.ProfileType, CountryCode: in.CountryCode, City: in.City,
		Timezone: in.Timezone, AccountingMode: in.AccountingMode}
}

func replaceWarehouseContacts(ctx context.Context, q *store.Queries, tenantID, warehouseID int64, contacts []WarehouseContactInput) error {
	if err := q.DeleteWarehouseContacts(ctx, store.DeleteWarehouseContactsParams{TenantID: tenantID, WarehouseID: warehouseID}); err != nil {
		return err
	}
	for _, c := range contacts {
		if err := q.CreateWarehouseContact(ctx, store.CreateWarehouseContactParams{
			TenantID: tenantID, WarehouseID: warehouseID, ContactType: c.ContactType, EmployeeID: c.EmployeeID,
			Name: strings.TrimSpace(c.Name), Phone: strings.TrimSpace(c.Phone), Email: strings.TrimSpace(c.Email),
			IsPrimary: c.IsPrimary, Status: c.Status,
		}); err != nil {
			return err
		}
	}
	return nil
}

func profileToWHType(profile string) string {
	if profile == "PORT" {
		return "PORT_TERMINAL"
	}
	return "PHYSICAL"
}

func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}
