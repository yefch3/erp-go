package app

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 智能转换的用量账（计量第一步）。
//
// 这是系统里目前唯一一处按次花真钱的地方：每转换一封客户来信就调一次模型。
// 在此之前只记了用的是哪个模型，没记用了多少——所以「这个月花了多少」
// 「谁花的」「该给客户多少额度」全都答不出来，定任何配额都只能拍脑袋。
//
// **金额在这里算，不在库里存。** token 数是事实，折成多少钱是判断：单价会
// 因为谈下折扣、换模型而变。存进去等于把一个会过期的判断固化成历史，而且
// 改一次单价就得回填全部旧数据。同 A4「付款是事实、核销是判断」。

// ModelPricing 是当下的单价，按每百万 token 计——模型厂就是这么报价的。
// 零值表示没配单价：那就只出 token 数，不出金额。**不猜。**
type ModelPricing struct {
	InputPerMTok  decimal.Decimal
	OutputPerMTok decimal.Decimal
	Currency      string
}

// Configured 说明这份单价能不能拿来算钱。
func (p ModelPricing) Configured() bool {
	return p.InputPerMTok.IsPositive() || p.OutputPerMTok.IsPositive()
}

// ExcelUsageRow 是用量账上的一行：某个月、某个人。
type ExcelUsageRow struct {
	Month        string
	OwnerID      int64
	Runs         int64
	Succeeded    int64
	Failed       int64
	InputTokens  int64
	OutputTokens int64
	// 按当下单价折出来的估算金额。没配单价时是空串——空和 0 是两回事，
	// 一个是「不知道」，一个是「不要钱」。
	EstimatedCost string
	Currency      string
}

// ExcelUsageByMonth 出这个租户的用量账，按月按人。month 传空表示全部月份。
func (s *Service) ExcelUsageByMonth(ctx context.Context, tenantID int64, month string) ([]ExcelUsageRow, error) {
	rows, err := s.q.ExcelUsageByMonth(ctx, store.ExcelUsageByMonthParams{
		TenantID: tenantID, Month: month,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ExcelUsageRow, 0, len(rows))
	for _, r := range rows {
		row := ExcelUsageRow{
			Month: r.Month, OwnerID: r.OwnerID, Runs: r.Runs,
			Succeeded: r.Succeeded, Failed: r.Failed,
			InputTokens: r.InputTokens, OutputTokens: r.OutputTokens,
		}
		if s.pricing.Configured() {
			row.EstimatedCost = estimateCost(r.InputTokens, r.OutputTokens, s.pricing)
			row.Currency = s.pricing.Currency
		}
		out = append(out, row)
	}
	return out, nil
}

// 每百万 token 一个价，所以除以一百万。用 decimal 而不是 float：这是钱，
// 而这个仓库里凡是钱都不走浮点。
var perMillion = decimal.NewFromInt(1_000_000)

func estimateCost(inputTokens, outputTokens int64, p ModelPricing) string {
	in := decimal.NewFromInt(inputTokens).Div(perMillion).Mul(p.InputPerMTok)
	out := decimal.NewFromInt(outputTokens).Div(perMillion).Mul(p.OutputPerMTok)
	// 四位小数：一次转换的成本常常在几分钱量级，两位会把它抹成 0.00，
	// 而「一次几分钱」正是要给业务看的那个数。
	return in.Add(out).StringFixed(4)
}

// EmployeeNames 把用量账上的 owner_id 换成人名。
//
// 账要给人看，而这一层只存 id——人名在 IAM。逐个查而不是批量：一份用量账
// 上不同的人也就几个到几十个，为它加一条批量接口不值得；目录取不到就留空，
// 一个查不到名字的人不该让整张账打不开。
func (s *Service) EmployeeNames(ctx context.Context, tenantID int64, ids []int64) map[int64]string {
	out := make(map[int64]string, len(ids))
	if s.directory == nil {
		return out
	}
	for _, id := range ids {
		if e, err := s.directory.Get(ctx, id); err == nil {
			out[id] = e.Name
		}
	}
	return out
}
