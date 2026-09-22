package app

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sgao19/erp-go/pkg/apierr"
)

func contractConstraint(err error, name string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == name
}

func contractNumberError(err error) error {
	if contractConstraint(err, "contracts_external_no_idx") {
		return apierr.Conflict("EX_EXTERNAL_CONTRACT_NO_TAKEN", "该原合同号已存在，请核对后再保存")
	}
	return err
}

// Each attempt must use a fresh transaction: PostgreSQL aborts the transaction
// on a unique violation. Never retry a customer-supplied reference conflict.
func withContractNumber(ctx context.Context, numbering Numbering, save func(string) error) error {
	for attempt := 0; attempt < 10; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		number, err := numbering.Next(ctx, "CONTRACT")
		if err != nil {
			return err
		}
		err = save(number)
		if !contractConstraint(err, "contracts_tenant_id_contract_no_key") {
			return err
		}
	}
	return apierr.Conflict("EX_CONTRACT_NUMBER_CONFLICT", "系统合同编号连续冲突，请联系管理员检查编号规则；无需修改原合同号")
}
