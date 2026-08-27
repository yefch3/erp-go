package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

// 采不到汇率时**必须留下痕迹**。
//
// 降级本身是对的：钱已经走了，不能因为 fx 打盹就拒绝记账，而 0 表示「没采到」
// 比编一个汇率诚实。但原来这里一声不吭——fx 挂一天，那天所有付款和采购发票的
// 本位币金额都是 0，账上悄悄多出一批洞，要等有人翻汇兑损益报表才发现，那时候
// 已经不知道是哪天、哪些行了。
// 和 supplierfx_integration_test.go 里的 stubRates 是两回事：那个缺币种时
// 返回 1（“USD 自己报自己”），**永远不失败**，所以它测不出这里要测的东西。
// 这个缺币种就报错，因为要测的正是“取不到会怎样”。
type failingRates struct {
	byCurrency map[string]decimal.Decimal
}

func (s failingRates) Latest(_ context.Context, currency string) (Rate, error) {
	if r, ok := s.byCurrency[currency]; ok {
		return Rate{Rate: r}, nil
	}
	return Rate{}, errors.New("fx unavailable")
}

func TestMissingFxRateIsLogged(t *testing.T) {
	cases := []struct {
		name     string
		rates    map[string]decimal.Decimal
		wantWhy  string
		wantRate string
	}{
		{
			// 外币取不到。
			name:     "外币没有汇率",
			rates:    map[string]decimal.Decimal{"CNY": decimal.NewFromInt(7)},
			wantWhy:  "取不到USD的汇率",
			wantRate: "0",
		},
		{
			// **本位币取不到最要紧**：每一笔外币单据都要它，一旦缺失，
			// 那段时间里所有付款和发票的本位币金额会一起归零。
			name:     "本位币没有汇率",
			rates:    map[string]decimal.Decimal{"USD": decimal.NewFromInt(1)},
			wantWhy:  "取不到本位币CNY的汇率",
			wantRate: "0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			svc := New(nil, Deps{
				Rates: failingRates{byCurrency: tc.rates},
				Log:   slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})),
			})
			rate, base := svc.fxSnapshot(context.Background(), "USD", decimal.NewFromInt(100))
			if rate.String() != tc.wantRate || !base.IsZero() {
				t.Fatalf("采不到就该记 0（不是编一个汇率）：rate=%s base=%s", rate, base)
			}
			logged := buf.String()
			if !strings.Contains(logged, tc.wantWhy) {
				t.Fatalf("日志里没说清楚缺的是哪个汇率。想要 %q，实际:\n%s", tc.wantWhy, logged)
			}
			// 光说「失败了」不够，要能按这条日志把受影响的行捞出来补。
			for _, want := range []string{"USD", "CNY", "汇兑损益"} {
				if !strings.Contains(logged, want) {
					t.Fatalf("日志里少了 %q，事后对不上号。实际:\n%s", want, logged)
				}
			}
		})
	}
}

// 本位币和记账币种相同时不该去问 fx，也不该报警——那是正常情况。
func TestSameCurrencyNeedsNoRate(t *testing.T) {
	var buf bytes.Buffer
	svc := New(nil, Deps{
		Rates: failingRates{}, // 一个汇率都没有
		Log:   slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})
	rate, base := svc.fxSnapshot(context.Background(), "CNY", decimal.NewFromInt(100))
	if !rate.Equal(decimal.NewFromInt(1)) || !base.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("本位币记账应该是 1:1，实际 rate=%s base=%s", rate, base)
	}
	if buf.Len() != 0 {
		t.Fatalf("正常情况不该报警，实际:\n%s", buf.String())
	}
}
