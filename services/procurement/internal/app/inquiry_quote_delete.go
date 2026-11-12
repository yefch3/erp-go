package app

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

func (s *Service) deleteInquiryQuote(ctx context.Context, tenant int64, op Operator, in InquiryCommand) (InquiryResult, error) {
	id, quoteID := inquiryID(in.ID), inquiryID(in.QuoteID)
	if id <= 0 || quoteID <= 0 {
		return InquiryResult{}, apierr.Invalid("INQUIRY_ID", "请选择要删除的报价")
	}
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var revision int64
		var status, handoff string
		if err := tx.QueryRow(ctx, `SELECT inquiry_revision,status,handoff_status FROM sourcing_cases WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, tenant, id).Scan(&revision, &status, &handoff); err != nil {
			if err == pgx.ErrNoRows {
				return apierr.NotFound("INQUIRY_NOT_FOUND", "询盘不存在")
			}
			return err
		}
		if inquiryState(status, handoff) != "INQUIRING" || status == "CANCELLED" || revision != in.Revision {
			return apierr.Conflict("INQUIRY_CHANGED", "询盘已撤回或变化，请刷新后重试")
		}
		var author, version int64
		var kind string
		var submitted bool
		var body []byte
		if err := tx.QueryRow(ctx, `SELECT created_by,version,kind,submitted_at IS NOT NULL,body FROM inquiry_quotes WHERE tenant_id=$1 AND case_id=$2 AND id=$3 AND inquiry_revision=$4 AND deleted_at IS NULL FOR UPDATE`, tenant, id, quoteID, revision).Scan(&author, &version, &kind, &submitted, &body); err != nil {
			if err == pgx.ErrNoRows {
				return apierr.Conflict("INQUIRY_QUOTE_CHANGED", "报价已修改或删除，请刷新")
			}
			return err
		}
		// Match the existing edit policy: private drafts belong to their author,
		// while submitted quotes can be maintained by the department's writers.
		if kind != in.View || (!submitted && author != op.ID) {
			return apierr.Permission("INQUIRY_QUOTE_AUTHOR", "未提交报价仅填写人可删除，且只能删除本部门报价")
		}
		if version != in.QuoteVersion {
			return apierr.Conflict("INQUIRY_QUOTE_CHANGED", "报价已由其他人修改，请刷新后再删除")
		}
		if _, err := tx.Exec(ctx, `UPDATE inquiry_quotes SET deleted_at=now(),version=version+1,updated_by=$4,updated_by_name=$5,updated_at=now() WHERE tenant_id=$1 AND case_id=$2 AND id=$3`, tenant, id, quoteID, op.ID, op.Name); err != nil {
			return err
		}
		before, err := json.Marshal(map[string]any{"quoteId": in.QuoteID, "kind": kind, "version": version, "body": json.RawMessage(body)})
		if err != nil {
			return err
		}
		return s.q.WithTx(tx).CreateSourcingChange(ctx, store.CreateSourcingChangeParams{
			TenantID: tenant, CaseID: id, Section: "INQUIRY_QUOTE", Action: "DELETED", EntityID: quoteID,
			Summary: "删除工厂报价", BeforeJson: before, AfterJson: []byte(`{"deleted":true}`), OperatorID: op.ID, OperatorName: op.Name,
		})
	})
	if err != nil {
		return InquiryResult{}, err
	}
	s.nudge(ctx, tenant, inquiryChanged)
	v, err := s.readInquiry(ctx, tenant, id, op, in.View)
	return InquiryResult{Item: v}, err
}
