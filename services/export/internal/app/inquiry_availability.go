package app

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

func (s *Service) SetInquiryAvailability(ctx context.Context, tenantID, caseID int64, available bool) error {
	if caseID <= 0 {
		return apierr.Invalid("INQUIRY_REQUIRED", "询盘不存在")
	}
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::bigint::text || ':inquiry:' || $2::bigint::text,0))`, tenantID, caseID); err != nil {
			return err
		}
		if available {
			_, err := tx.Exec(ctx, `DELETE FROM inquiry_withdrawal_fences WHERE tenant_id=$1 AND case_id=$2`, tenantID, caseID)
			return err
		}
		var protected bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM quotations q WHERE q.tenant_id=$1 AND q.source_sourcing_case_id=$2 AND (q.status='ACCEPTED' OR EXISTS(SELECT 1 FROM contracts c WHERE c.tenant_id=q.tenant_id AND c.quotation_id=q.id)))`, tenantID, caseID).Scan(&protected); err != nil {
			return err
		}
		if protected {
			return apierr.Conflict("INQUIRY_CONTRACT_EXISTS", "客户已确认或合同已形成，不能撤回询盘")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO inquiry_withdrawal_fences(tenant_id,case_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, tenantID, caseID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM quotations WHERE tenant_id=$1 AND source_sourcing_case_id=$2`, tenantID, caseID)
		return err
	})
}
