package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/export/internal/store"
	"strings"
)

func (s *Service) CompleteContract(ctx context.Context, tenant, id int64, op Operator) (string, error) {
	view, err := s.GetContract(ctx, tenant, id, 0)
	if err != nil {
		return "", err
	}
	if err := s.mustOwnContract(ctx, op, view); err != nil {
		return "", err
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		locked, err := q.LockContract(ctx, store.LockContractParams{TenantID: tenant, ID: id})
		if err != nil {
			return err
		}
		if locked.Status == "COMPLETED" {
			return nil
		}
		if locked.Status != "EXECUTING" && locked.Status != "EFFECTIVE" {
			return apierr.Conflict("EX_COMPLETE_STATUS", "只有执行中的合同可以完成")
		}
		_, err = q.SetContractStatus(ctx, store.SetContractStatusParams{TenantID: tenant, ID: id, NewStatus: "COMPLETED", UpdatedBy: op.ID})
		return err
	})
	if err != nil {
		return "", err
	}
	return "COMPLETED", nil
}
func (s *Service) supplementContract(ctx context.Context, tenant, id int64, terms Terms, items []ItemInput, meta ContractEditMeta, op Operator) (ContractView, error) {
	if len(items) > 0 {
		return ContractView{}, apierr.Conflict("EX_SIGNED_LINES_FROZEN", "签署合同的产品、数量和价格不能直接修改")
	}
	if err := validBusinessDate(terms.ReceivableDueDate, "EX_DUE_DATE_INVALID", "应收日期"); err != nil {
		return ContractView{}, err
	}
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		locked, err := q.LockContract(ctx, store.LockContractParams{TenantID: tenant, ID: id})
		if err != nil {
			return err
		}
		if locked.Status != "EXECUTING" && locked.Status != "EFFECTIVE" {
			return apierr.Conflict("EX_SUPPLEMENT_STATUS", "只有执行中的合同可以补充信息")
		}
		_, err = tx.Exec(ctx, `UPDATE contracts SET external_contract_no=$3,receivable_due_date=nullif($4,'')::date,updated_at=now(),updated_by=$5 WHERE tenant_id=$1 AND id=$2`, tenant, id, strings.TrimSpace(meta.ExternalContractNo), terms.ReceivableDueDate, op.ID)
		return err
	})
	if err != nil {
		return ContractView{}, err
	}
	return s.GetContract(ctx, tenant, id, 0)
}

func (s *Service) AcceptedContractOffer(ctx context.Context, tenant, id int64) (string, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT body FROM customer_offers WHERE tenant_id=$1 AND contract_id=$2`, tenant, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var body OfferBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return "", err
	}
	lines := make([]OfferProduct, 0, len(body.Lines))
	for _, line := range body.Lines {
		lines = append(lines, line.OfferProduct)
	}
	out, err := json.Marshal(map[string]any{"customer": body.Customer, "contact": body.Contact, "transports": body.Transports, "lines": lines})
	return string(out), err
}
