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
//
// Callers that create a record in the same breath should use nextNumber with
// their own transaction instead: the sequence bump then rolls back with a
// failed insert, so a rejected create leaves no hole.
func (s *Service) NextNumber(ctx context.Context, tenantID int64, bizType string) (string, error) {
	return s.nextNumber(ctx, s.q, tenantID, bizType)
}

func (s *Service) nextNumber(ctx context.Context, q *store.Queries, tenantID int64, bizType string) (string, error) {
	if bizType == "" {
		return "", apierr.Invalid("MD_BIZ_TYPE_REQUIRED", "单据类型必填")
	}
	rule, err := q.GetNumberRule(ctx, store.GetNumberRuleParams{TenantID: tenantID, BizType: bizType})
	if errors.Is(err, pgx.ErrNoRows) {
		// 没规则 ≠ 该拒绝：也可能只是这家公司还没被播过种。
		//
		// 编码规则的种子全部写死 tenant_id = 1（00001/00002/…/00018），第二家
		// 起的公司一条规则都没有——第一封邮件、第一张报价单、第一个客户编号，
		// 全部撞在「未配置编码规则」上。这和邮件后台只服务第一家（#216）是
		// 同一个病：按第一家公司的形状写的种子，被当成了所有公司的地基。
		//
		// 修在读的这一侧而不是开户那一侧，因为开户在 iam、规则在这里，跨服务
		// 播种会给每条开户路径（.env、平台页、将来的第三条）各埋一次「忘了
		// 调」。懒播种对路径免疫：谁来都一样。
		rule, err = s.seedDefaultRule(ctx, q, tenantID, bizType)
	}
	if err != nil {
		return "", err
	}

	periodKey := periodKeyFor(rule.Period, time.Now())
	seq, err := q.NextSeq(ctx, store.NextSeqParams{
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

// defaultNumberRules 是每家公司开张即有的编码规则，值与给第一家公司播种的
// 迁移（00001/00002/00003/00004/00005/00015/00016/00018）逐条相同。
//
// 两处必须一致：迁移是第一家的事实，这张表是其余每家的事实。改任何一边都要
// 同时改另一边——不一致的症状是「新公司的单号格式和老公司不一样」，而单号
// 印在发给客户的文件上。
var defaultNumberRules = map[string]struct {
	Prefix string
	Period string
	SeqLen int32
}{
	"QUOTATION":        {"QT", "DAILY", 4},
	"CONTRACT":         {"CT", "MONTHLY", 4},
	"PURCHASE_ORDER":   {"PO", "MONTHLY", 4},
	"SHIPMENT_PLAN":    {"SP", "DAILY", 4},
	"INBOUND":          {"IN", "DAILY", 4},
	"OUTBOUND":         {"OUT", "DAILY", 4},
	"CUSTOMER":         {"CU", "NONE", 4},
	"SUPPLIER":         {"SU", "NONE", 4},
	"PRODUCT":          {"P", "NONE", 5},
	"SHIPMENT":         {"SH", "MONTHLY", 4},
	"CAMPAIGN":         {"EM", "MONTHLY", 4},
	"FACTORY":          {"FA", "NONE", 5},
	"WAREHOUSE":        {"WH", "NONE", 4},
	"SUPPLIER_PAYMENT": {"PAY", "MONTHLY", 4},
}

// seedDefaultRule 给「还没被播过种」的 (公司, 单据类型) 补上默认规则并返回。
//
// DO NOTHING 而不是覆盖，且写完必须**再读**：并发的两个调用者同时走到这里，
// 只有一个插入成功，另一个的 DO NOTHING 之后读到的才是真正生效的那条。人改
// 过的规则也因此永远赢——默认值只填空，不还原。
//
// 不认识的类型照旧拒绝：那不是「没播种」，是调用方写错了类型名，静默造一条
// 规则会把拼写错误固化成一个真实的号段。
func (s *Service) seedDefaultRule(ctx context.Context, q *store.Queries, tenantID int64, bizType string) (store.NumberRule, error) {
	def, known := defaultNumberRules[bizType]
	if !known {
		return store.NumberRule{}, apierr.NotFound("MD_NUMBER_RULE_NOT_FOUND", "该单据类型未配置编码规则").
			WithMeta("biz_type", bizType)
	}
	if _, err := q.InsertNumberRuleIfAbsent(ctx, store.InsertNumberRuleIfAbsentParams{
		TenantID: tenantID, BizType: bizType,
		Prefix: def.Prefix, Period: def.Period, SeqLen: def.SeqLen,
	}); err != nil {
		return store.NumberRule{}, fmt.Errorf("numbering: seed default: %w", err)
	}
	// masterdata 的 Service 没有挂 logger（全服务皆然），这里不为一行日志开先例；
	// 播种的事实可以从 number_rules 表直接看到。
	return q.GetNumberRule(ctx, store.GetNumberRuleParams{TenantID: tenantID, BizType: bizType})
}
