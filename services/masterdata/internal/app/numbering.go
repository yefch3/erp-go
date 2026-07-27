package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

// NextNumber issues the next document number for a business type, e.g.
// "QT-20260727-0001". Uniqueness is guaranteed by a single atomic upsert on
// number_sequences: concurrent callers serialize on the row lock. Numbers
// may have gaps (a caller whose transaction later fails keeps its number
// consumed) - that is intentional; uniqueness matters, gaplessness does not.
func (s *Service) NextNumber(ctx context.Context, tenantID int64, bizType string) (string, error) {
	if bizType == "" {
		return "", apierr.Invalid("MD_BIZ_TYPE_REQUIRED", "单据类型必填")
	}
	rule, err := s.q.GetNumberRule(ctx, store.GetNumberRuleParams{TenantID: tenantID, BizType: bizType})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", apierr.NotFound("MD_NUMBER_RULE_NOT_FOUND", "该单据类型未配置编码规则").
				WithMeta("biz_type", bizType)
		}
		return "", err
	}

	periodKey := periodKeyFor(rule.Period, time.Now())
	seq, err := s.q.NextSeq(ctx, store.NextSeqParams{
		TenantID: tenantID, BizType: bizType, PeriodKey: periodKey,
	})
	if err != nil {
		return "", fmt.Errorf("numbering: next seq: %w", err)
	}

	if periodKey == "" {
		return fmt.Sprintf("%s-%0*d", rule.Prefix, rule.SeqLen, seq), nil
	}
	return fmt.Sprintf("%s-%s-%0*d", rule.Prefix, periodKey, rule.SeqLen, seq), nil
}

func (s *Service) ListNumberRules(ctx context.Context, tenantID int64) ([]store.NumberRule, error) {
	return s.q.ListNumberRules(ctx, tenantID)
}

func periodKeyFor(period string, now time.Time) string {
	switch period {
	case "DAILY":
		return now.Format("20060102")
	case "MONTHLY":
		return now.Format("200601")
	case "YEARLY":
		return now.Format("2006")
	default: // NONE
		return ""
	}
}
