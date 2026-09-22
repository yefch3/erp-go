package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

type BulkOwnerAssignment struct {
	EmployeeID   int64
	EmployeeName string
}

func normalizeBulkOwnerInput(entityIDs []int64, owners []BulkOwnerAssignment, action string) ([]int64, []BulkOwnerAssignment, string, error) {
	action = strings.ToUpper(strings.TrimSpace(action))
	if action != "ADD" && action != "REMOVE" {
		return nil, nil, "", apierr.Invalid("MD_BULK_OWNER_ACTION_INVALID", "批量负责人操作只能是 ADD 或 REMOVE")
	}
	entitySeen := map[int64]bool{}
	cleanEntities := make([]int64, 0, len(entityIDs))
	for _, id := range entityIDs {
		if id <= 0 || entitySeen[id] {
			continue
		}
		entitySeen[id] = true
		cleanEntities = append(cleanEntities, id)
	}
	ownerSeen := map[int64]bool{}
	cleanOwners := make([]BulkOwnerAssignment, 0, len(owners))
	for _, owner := range owners {
		owner.EmployeeName = strings.TrimSpace(owner.EmployeeName)
		if owner.EmployeeID <= 0 || owner.EmployeeName == "" || ownerSeen[owner.EmployeeID] {
			continue
		}
		ownerSeen[owner.EmployeeID] = true
		cleanOwners = append(cleanOwners, owner)
	}
	if len(cleanEntities) == 0 || len(cleanOwners) == 0 {
		return nil, nil, "", apierr.Invalid("MD_BULK_OWNER_REQUIRED", "请选择需要修改的资料和负责人")
	}
	if len(cleanEntities) > 100 {
		return nil, nil, "", apierr.Invalid("MD_BULK_OWNER_TOO_MANY_ENTITIES", "每次最多处理 100 条资料")
	}
	if len(cleanOwners) > 50 {
		return nil, nil, "", apierr.Invalid("MD_BULK_OWNER_TOO_MANY_OWNERS", "每次最多选择 50 名负责人")
	}
	return cleanEntities, cleanOwners, action, nil
}

func (s *Service) BatchUpdateCustomerOwners(ctx context.Context, tenantID int64, customerIDs []int64, owners []BulkOwnerAssignment, action string, operatorID int64, operatorName string) (int32, error) {
	if customerAccessEmployee(ctx) != 0 {
		return 0, apierr.Permission("MD_CUSTOMER_OWNER_MANAGE_DENIED", "只有最高权限用户可以批量管理客户负责人")
	}
	customerIDs, owners, action, err := normalizeBulkOwnerInput(customerIDs, owners, action)
	if err != nil {
		return 0, err
	}
	var changed int32
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM customers WHERE tenant_id=$1 AND id=ANY($2)`, tenantID, customerIDs).Scan(&count); err != nil {
			return err
		}
		if count != len(customerIDs) {
			return apierr.NotFound("MD_CUSTOMER_NOT_FOUND", "部分客户不存在或不属于当前公司")
		}
		q := s.q.WithTx(tx)
		for _, customerID := range customerIDs {
			for _, owner := range owners {
				var affected int64
				if action == "ADD" {
					tag, execErr := tx.Exec(ctx, `INSERT INTO customer_owners
						(tenant_id,customer_id,employee_id,employee_name,responsibility_code,is_primary,created_by,updated_by)
						SELECT $1,$2,$3,$4,'SALES',false,$5,$5
						WHERE NOT EXISTS (SELECT 1 FROM customer_owners WHERE tenant_id=$1 AND customer_id=$2 AND employee_id=$3 AND status='ACTIVE')`,
						tenantID, customerID, owner.EmployeeID, owner.EmployeeName, operatorID)
					err, affected = execErr, tag.RowsAffected()
				} else {
					tag, execErr := tx.Exec(ctx, `UPDATE customer_owners SET status='INACTIVE',is_primary=false,
						end_date=COALESCE(end_date,CURRENT_DATE),updated_by=$4,updated_at=now()
						WHERE tenant_id=$1 AND customer_id=$2 AND employee_id=$3 AND status='ACTIVE'`,
						tenantID, customerID, owner.EmployeeID, operatorID)
					err, affected = execErr, tag.RowsAffected()
				}
				if err != nil {
					return err
				}
				if affected == 0 {
					continue
				}
				changed += int32(affected)
				verb := "批量添加负责人："
				if action == "REMOVE" {
					verb = "批量移除负责人："
				}
				if err := recordCustomerChange(ctx, q, tenantID, customerID, action, "OWNER", verb+owner.EmployeeName, nil, owner, operatorID, operatorName); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return changed, err
}

func (s *Service) BatchUpdateSupplierOwners(ctx context.Context, tenantID int64, supplierIDs []int64, owners []BulkOwnerAssignment, action string, operatorID int64, operatorName string) (int32, error) {
	if supplierAccessEmployee(ctx) != 0 {
		return 0, apierr.Permission("MD_SUPPLIER_OWNER_MANAGE_DENIED", "只有最高权限用户可以批量管理供应商负责人")
	}
	supplierIDs, owners, action, err := normalizeBulkOwnerInput(supplierIDs, owners, action)
	if err != nil {
		return 0, err
	}
	var changed int32
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM suppliers WHERE tenant_id=$1 AND id=ANY($2)`, tenantID, supplierIDs).Scan(&count); err != nil {
			return err
		}
		if count != len(supplierIDs) {
			return apierr.NotFound("MD_SUPPLIER_NOT_FOUND", "部分供应商不存在或不属于当前公司")
		}
		q := s.q.WithTx(tx)
		for _, supplierID := range supplierIDs {
			for _, owner := range owners {
				var affected int64
				if action == "ADD" {
					tag, execErr := tx.Exec(ctx, `INSERT INTO supplier_owners
						(tenant_id,supplier_id,employee_id,employee_name,responsibility_code,is_primary,created_by,updated_by)
						SELECT $1,$2,$3,$4,'PROCUREMENT',false,$5,$5
						WHERE NOT EXISTS (SELECT 1 FROM supplier_owners WHERE tenant_id=$1 AND supplier_id=$2 AND employee_id=$3 AND status='ACTIVE')`,
						tenantID, supplierID, owner.EmployeeID, owner.EmployeeName, operatorID)
					err, affected = execErr, tag.RowsAffected()
				} else {
					tag, execErr := tx.Exec(ctx, `UPDATE supplier_owners SET status='INACTIVE',is_primary=false,updated_by=$4,updated_at=now()
						WHERE tenant_id=$1 AND supplier_id=$2 AND employee_id=$3 AND status='ACTIVE'`,
						tenantID, supplierID, owner.EmployeeID, operatorID)
					err, affected = execErr, tag.RowsAffected()
				}
				if err != nil {
					return err
				}
				if affected == 0 {
					continue
				}
				changed += int32(affected)
				verb := "批量添加负责人："
				if action == "REMOVE" {
					verb = "批量移除负责人："
				}
				if err := recordSupplierChange(ctx, q, tenantID, supplierID, action, "OWNER", fmt.Sprintf("%s%s", verb, owner.EmployeeName), nil, owner, operatorID, operatorName); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return changed, err
}
