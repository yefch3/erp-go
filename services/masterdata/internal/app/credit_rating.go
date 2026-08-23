package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

// 信用评级（E3 第一期）。
//
// 业务的原话是「供应商和客户需要信用度评级，分为 ABCD」。
//
// 这一期只做人工评级，**不做系统建议**。建议要算的是「平均逾期 23 天、
// 近一年 8 笔全部按时」这类数字，而合同和采购单眼下一条都还没跑起来，
// 算出来只会是一排 0——一个凭空的数字比没有数字更坏，因为它看着像结论。
//
// 真正让这套东西不腐烂的不是评级本身，是三件事：
//   - 依据必填。说不出为什么的评级等于没评。
//   - 每改一次留一行。半年后看见「B」，得能查出那是上周判的还是三年前。
//   - 记下评级那一刻系统里的数字。现在大多是空的，但位置留着——数据攒
//     起来之后，历史里能看出「当时依据的数字长这样」，而不是只剩一句话。

// 评级只有四档，故意不给「未评级」一个字母：没评过就是空，空和 D 是两回
// 事——一个是还没看过，一个是看过并且判定为差。
var creditGrades = map[string]bool{"A": true, "B": true, "C": true, "D": true}

const (
	partyCustomer = "CUSTOMER"
	partySupplier = "SUPPLIER"
)

// CreditRatingInput 是一次评级。
type CreditRatingInput struct {
	PartyType string
	PartyID   int64
	Grade     string
	// 为什么给这个分。必填。
	Basis string
	// 评级那一刻系统里的数字。调用方按对象类型装：客户看逾期和欠款，
	// 供应商看交期准时、供货足量、质检合格。允许为空——现在大多确实是空的。
	Evidence     map[string]string
	OperatorID   int64
	OperatorName string
}

// CreditRating 是历史上的一行。
type CreditRating struct {
	ID            int64
	Grade         string
	PreviousGrade string
	Basis         string
	Evidence      map[string]string
	RatedBy       int64
	RatedByName   string
	RatedAt       string
}

// RateCredit 记一次评级，并把当前评级投影回主数据行。
//
// 两件事必须在同一个事务里：历史表是事实来源，主数据行上那两列是给列表
// 筛选和排序用的投影。分开写就会出现「列表显示 B、历史最新一条是 C」，
// 而那种不一致没有人能在事后判断哪个才对。
func (s *Service) RateCredit(ctx context.Context, tenantID int64, in CreditRatingInput) (CreditRating, error) {
	in.PartyType = strings.ToUpper(strings.TrimSpace(in.PartyType))
	if in.PartyType != partyCustomer && in.PartyType != partySupplier {
		return CreditRating{}, apierr.Invalid("MD_CREDIT_PARTY_INVALID", "评级对象只能是客户或供应商")
	}
	if in.PartyID <= 0 {
		return CreditRating{}, apierr.Invalid("MD_CREDIT_PARTY_REQUIRED", "请选择要评级的客户或供应商")
	}
	in.Grade = strings.ToUpper(strings.TrimSpace(in.Grade))
	if !creditGrades[in.Grade] {
		return CreditRating{}, apierr.Invalid("MD_CREDIT_GRADE_INVALID", "信用评级只能是 A、B、C 或 D")
	}
	in.Basis = strings.TrimSpace(in.Basis)
	if in.Basis == "" {
		return CreditRating{}, apierr.Invalid("MD_CREDIT_BASIS_REQUIRED",
			"请填写评级依据——说不出为什么的评级，半年后没有人能判断它还作不作数")
	}
	evidence, err := json.Marshal(orEmptyMap(in.Evidence))
	if err != nil {
		return CreditRating{}, err
	}

	var out CreditRating
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		previous, err := currentGrade(ctx, q, tenantID, in.PartyType, in.PartyID)
		if err != nil {
			return err
		}
		row, err := q.RecordCreditRating(ctx, store.RecordCreditRatingParams{
			TenantID: tenantID, PartyType: in.PartyType, PartyID: in.PartyID,
			Grade: in.Grade, PreviousGrade: previous, Basis: in.Basis, Evidence: evidence,
			RatedBy: in.OperatorID, RatedByName: in.OperatorName,
		})
		if err != nil {
			return err
		}
		affected, err := setGrade(ctx, q, tenantID, in.PartyType, in.PartyID, in.Grade)
		if err != nil {
			return err
		}
		if affected == 0 {
			return notFoundFor(in.PartyType)
		}
		out = CreditRating{
			ID: row.ID, Grade: in.Grade, PreviousGrade: previous, Basis: in.Basis,
			Evidence: orEmptyMap(in.Evidence), RatedBy: in.OperatorID,
			RatedByName: in.OperatorName, RatedAt: tsText(row.RatedAt),
		}
		return nil
	})
	if err != nil {
		return CreditRating{}, err
	}
	return out, nil
}

// ListCreditRatings 返回一个客户或供应商的评级变更史，最近的在前。
func (s *Service) ListCreditRatings(ctx context.Context, tenantID int64, partyType string, partyID int64, limit int32) ([]CreditRating, error) {
	partyType = strings.ToUpper(strings.TrimSpace(partyType))
	if partyType != partyCustomer && partyType != partySupplier {
		return nil, apierr.Invalid("MD_CREDIT_PARTY_INVALID", "评级对象只能是客户或供应商")
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := s.q.ListCreditRatings(ctx, store.ListCreditRatingsParams{
		TenantID: tenantID, PartyType: partyType, PartyID: partyID, RowLimit: limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]CreditRating, 0, len(rows))
	for _, r := range rows {
		out = append(out, CreditRating{
			ID: r.ID, Grade: r.Grade, PreviousGrade: r.PreviousGrade, Basis: r.Basis,
			Evidence: decodeEvidence(r.Evidence), RatedBy: r.RatedBy,
			RatedByName: r.RatedByName, RatedAt: tsText(r.RatedAt),
		})
	}
	return out, nil
}

func currentGrade(ctx context.Context, q *store.Queries, tenantID int64, partyType string, partyID int64) (string, error) {
	if partyType == partyCustomer {
		row, err := q.GetCustomerCreditGrade(ctx, store.GetCustomerCreditGradeParams{TenantID: tenantID, ID: partyID})
		if errors.Is(err, pgx.ErrNoRows) {
			return "", notFoundFor(partyType)
		}
		return row.CreditGrade, err
	}
	row, err := q.GetSupplierCreditGrade(ctx, store.GetSupplierCreditGradeParams{TenantID: tenantID, ID: partyID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", notFoundFor(partyType)
	}
	return row.CreditGrade, err
}

func setGrade(ctx context.Context, q *store.Queries, tenantID int64, partyType string, partyID int64, grade string) (int64, error) {
	if partyType == partyCustomer {
		return q.SetCustomerCreditGrade(ctx, store.SetCustomerCreditGradeParams{TenantID: tenantID, ID: partyID, Grade: grade})
	}
	return q.SetSupplierCreditGrade(ctx, store.SetSupplierCreditGradeParams{TenantID: tenantID, ID: partyID, Grade: grade})
}

func notFoundFor(partyType string) error {
	if partyType == partyCustomer {
		return apierr.NotFound("MD_CUSTOMER_NOT_FOUND", "客户不存在")
	}
	return apierr.NotFound("MD_SUPPLIER_NOT_FOUND", "供应商不存在")
}

// 时间戳统一成 RFC3339 文本再过边界，和这个服务其它读接口一致：日期的
// 时区解释只在一个地方做。
func tsText(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}

func orEmptyMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// 存进去的形状我们自己定，读不出来说明有人手改过库。当作没有依据数字，
// 而不是让整页打不开——评级和依据那句话仍然是有用的。
func decodeEvidence(raw []byte) map[string]string {
	out := map[string]string{}
	if len(raw) == 0 {
		return out
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]string{}
	}
	return out
}
