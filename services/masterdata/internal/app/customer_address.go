package app

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

var customerAddressTypes = map[string]bool{
	"REGISTERED": true,
	"OFFICE":     true,
	"BILLING":    true,
	"SHIPPING":   true,
}

type CustomerAddressInput struct {
	AddressType, CountryCode, State, City, PostalCode, AddressLine string
	IsDefault                                                      bool
	SortOrder                                                      int32
	OperatorID                                                     int64
	OperatorName                                                   string
}

// normalizeAndValidateAddress 统一地址类型和国家代码，保证 REST、gRPC 和数据库
// 三层使用相同规则，避免同一地址在不同入口得到不同结果。
func (in *CustomerAddressInput) normalizeAndValidateAddress() error {
	in.AddressType = strings.ToUpper(strings.TrimSpace(in.AddressType))
	if !customerAddressTypes[in.AddressType] {
		return apierr.Invalid("MD_CUSTOMER_ADDRESS_TYPE_INVALID", "地址类型无效")
	}
	code, err := normaliseCountry(in.CountryCode)
	if err != nil {
		return err
	}
	in.CountryCode = code
	in.AddressLine = strings.TrimSpace(in.AddressLine)
	if in.AddressLine == "" {
		return apierr.Invalid("MD_CUSTOMER_ADDRESS_REQUIRED", "详细地址必填")
	}
	if in.SortOrder < 0 {
		return apierr.Invalid("MD_CUSTOMER_ADDRESS_SORT_INVALID", "地址排序不能小于 0")
	}
	return nil
}

func (s *Service) ListCustomerAddresses(ctx context.Context, tenantID, customerID int64, status string) ([]store.CustomerAddress, error) {
	if _, _, err := s.GetCustomer(ctx, tenantID, customerID); err != nil {
		return nil, err
	}
	if status != "ALL" {
		status = ""
	}
	return s.q.ListCustomerAddresses(ctx, store.ListCustomerAddressesParams{
		TenantID: tenantID, CustomerID: customerID, Status: status,
	})
}

// CreateCustomerAddress 在一个事务中取消同类型旧默认地址并新增地址，
// 唯一索引作为并发情况下的最后一道保护。
func (s *Service) CreateCustomerAddress(ctx context.Context, tenantID, customerID int64, in CustomerAddressInput) (store.CustomerAddress, error) {
	if err := in.normalizeAndValidateAddress(); err != nil {
		return store.CustomerAddress{}, err
	}
	if _, _, err := s.GetCustomer(ctx, tenantID, customerID); err != nil {
		return store.CustomerAddress{}, err
	}
	var out store.CustomerAddress
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if in.IsDefault {
			if err := q.ClearDefaultCustomerAddress(ctx, store.ClearDefaultCustomerAddressParams{
				TenantID: tenantID, CustomerID: customerID, AddressType: in.AddressType,
				OperatorID: in.OperatorID,
			}); err != nil {
				return err
			}
		}
		var err error
		out, err = q.CreateCustomerAddress(ctx, store.CreateCustomerAddressParams{
			TenantID: tenantID, CustomerID: customerID, AddressType: in.AddressType,
			CountryCode: in.CountryCode, State: in.State, City: in.City,
			PostalCode: in.PostalCode, AddressLine: in.AddressLine,
			IsDefault: in.IsDefault, SortOrder: in.SortOrder, OperatorID: in.OperatorID,
		})
		if err != nil {
			return err
		}
		return recordCustomerChange(ctx, q, tenantID, customerID, "CREATE", "ADDRESS", "新增客户地址", nil, out, in.OperatorID, in.OperatorName)
	})
	return out, err
}

func (s *Service) UpdateCustomerAddress(ctx context.Context, tenantID, customerID, id int64, in CustomerAddressInput) (store.CustomerAddress, error) {
	if err := in.normalizeAndValidateAddress(); err != nil {
		return store.CustomerAddress{}, err
	}
	var out store.CustomerAddress
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		before, err := q.GetCustomerAddress(ctx, store.GetCustomerAddressParams{
			TenantID: tenantID, CustomerID: customerID, ID: id,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apierr.NotFound("MD_CUSTOMER_ADDRESS_NOT_FOUND", "客户地址不存在")
			}
			return err
		}
		if in.IsDefault {
			if err := q.ClearDefaultCustomerAddress(ctx, store.ClearDefaultCustomerAddressParams{
				TenantID: tenantID, CustomerID: customerID, AddressType: in.AddressType,
				OperatorID: in.OperatorID,
			}); err != nil {
				return err
			}
		}
		out, err = q.UpdateCustomerAddress(ctx, store.UpdateCustomerAddressParams{
			TenantID: tenantID, CustomerID: customerID, ID: id,
			AddressType: in.AddressType, CountryCode: in.CountryCode,
			State: in.State, City: in.City, PostalCode: in.PostalCode,
			AddressLine: in.AddressLine, IsDefault: in.IsDefault,
			SortOrder: in.SortOrder, OperatorID: in.OperatorID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("MD_CUSTOMER_ADDRESS_NOT_FOUND", "客户地址不存在或已停用")
		}
		if err != nil {
			return err
		}
		return recordCustomerChange(ctx, q, tenantID, customerID, "UPDATE", "ADDRESS", "更新客户地址", before, out, in.OperatorID, in.OperatorName)
	})
	return out, err
}

func (s *Service) DeactivateCustomerAddress(ctx context.Context, tenantID, customerID, id, operatorID int64, operatorName ...string) error {
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		before, err := q.GetCustomerAddress(ctx, store.GetCustomerAddressParams{TenantID: tenantID, CustomerID: customerID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("MD_CUSTOMER_ADDRESS_NOT_FOUND", "客户地址不存在或已停用")
		}
		if err != nil {
			return err
		}
		n, err := q.DeactivateCustomerAddress(ctx, store.DeactivateCustomerAddressParams{TenantID: tenantID, CustomerID: customerID, ID: id, OperatorID: operatorID})
		if err != nil {
			return err
		}
		if n == 0 {
			return apierr.NotFound("MD_CUSTOMER_ADDRESS_NOT_FOUND", "客户地址不存在或已停用")
		}
		name := ""
		if len(operatorName) > 0 {
			name = operatorName[0]
		}
		return recordCustomerChange(ctx, q, tenantID, customerID, "DEACTIVATE", "ADDRESS", "停用客户地址", before, nil, operatorID, name)
	})
}
