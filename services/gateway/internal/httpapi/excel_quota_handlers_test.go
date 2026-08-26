package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
)

// 额度这条路上最危险的一处翻译：**「不限」和「一次都不许用」**。
//
// 「不限」是浏览器不带 monthlyRuns（JSON null），「一次都不许用」是带 0。
// 两者在 JSON 里差一个字，后果正好相反：一个是随便用，一个是完全停掉。
// 网关用 *int64 指针区分它们——而指针最容易在某次「顺手改成 int64」的重构
// 里被抹平，抹平之后两种意图会撞成同一个请求，**编译照样通过**。
//
// 这一条就是钉住那个指针。它测的不是我写对没有，是下一个人改错了会不会响。

type quotaRecorder struct {
	mailv1.EmailServiceClient
	set *mailv1.SetExcelQuotaRequest
}

func (r *quotaRecorder) SetExcelQuota(_ context.Context, in *mailv1.SetExcelQuotaRequest, _ ...grpc.CallOption) (*mailv1.SetExcelQuotaResponse, error) {
	r.set = in
	return &mailv1.SetExcelQuotaResponse{}, nil
}

func (r *quotaRecorder) ListExcelQuotas(context.Context, *mailv1.ListExcelQuotasRequest, ...grpc.CallOption) (*mailv1.ListExcelQuotasResponse, error) {
	return &mailv1.ListExcelQuotasResponse{
		CurrentMonth: "2026-08",
		Quotas: []*mailv1.TenantExcelQuota{{
			TenantId: 2, Limited: true, MonthlyRuns: 200, UsedThisMonth: 41,
			InputTokens: 120000, OutputTokens: 30000,
			EstimatedCost: "0.4500", Currency: "USD",
		}},
	}, nil
}

// 平台操作员这道门放行，好让测试能走到真正要测的那段翻译。
type okOperator struct {
	iamv1.PlatformServiceClient
}

func (okOperator) CheckOperator(context.Context, *iamv1.CheckOperatorRequest, ...grpc.CallOption) (*iamv1.CheckOperatorResponse, error) {
	return &iamv1.CheckOperatorResponse{Operator: true}, nil
}

func TestSetExcelQuotaKeepsUnlimitedAndZeroApart(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		wantLimited bool
		wantRuns    int64
	}{
		{
			name:        "不带 monthlyRuns = 恢复不限",
			body:        `{"tenantId":2}`,
			wantLimited: false,
		},
		{
			name:        "monthlyRuns 为 null = 恢复不限",
			body:        `{"tenantId":2,"monthlyRuns":null}`,
			wantLimited: false,
		},
		{
			name:        "monthlyRuns 为 0 = 一次都不许用（和不限正好相反）",
			body:        `{"tenantId":2,"monthlyRuns":0}`,
			wantLimited: true,
			wantRuns:    0,
		},
		{
			name:        "正常上限",
			body:        `{"tenantId":2,"monthlyRuns":200}`,
			wantLimited: true,
			wantRuns:    200,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &quotaRecorder{}
			s := &Server{Emails: rec, Platform: okOperator{}}
			w := httptest.NewRecorder()
			s.setExcelQuota(w, httptest.NewRequest("POST", "/api/platform/excel-quotas", strings.NewReader(c.body)))

			if w.Code != 200 {
				t.Fatalf("HTTP %d：%s", w.Code, w.Body.String())
			}
			if rec.set == nil {
				t.Fatal("请求没有到达邮件服务")
			}
			if rec.set.GetLimited() != c.wantLimited {
				t.Fatalf("limited = %v，该是 %v。"+
					"「不限」和「上限 0」在这里被搞混了，后果正好相反",
					rec.set.GetLimited(), c.wantLimited)
			}
			if c.wantLimited && rec.set.GetMonthlyRuns() != c.wantRuns {
				t.Fatalf("monthlyRuns = %d，该是 %d", rec.set.GetMonthlyRuns(), c.wantRuns)
			}
			if rec.set.GetTenantId() != 2 {
				t.Fatalf("公司号丢了或串了：%d", rec.set.GetTenantId())
			}
		})
	}
}

// 没点名公司就不能定额度——否则一次手滑会落到某家莫名其妙的公司头上。
func TestSetExcelQuotaRefusesWithoutATenant(t *testing.T) {
	for _, body := range []string{`{}`, `{"tenantId":0}`, `{"tenantId":-1,"monthlyRuns":10}`} {
		rec := &quotaRecorder{}
		s := &Server{Emails: rec, Platform: okOperator{}}
		w := httptest.NewRecorder()
		s.setExcelQuota(w, httptest.NewRequest("POST", "/api/platform/excel-quotas", strings.NewReader(body)))
		if w.Code == 200 {
			t.Fatalf("%s 被放行了", body)
		}
		if rec.set != nil {
			t.Fatalf("%s 竟然发到了邮件服务", body)
		}
	}
}

// 平台那条列表**必须**把成本带出来——这是金额现在唯一的去处。
// 同时确认数字走的是普通 JSON（是数字不是字符串），前端才敢当数字用。
func TestListExcelQuotasCarriesCostAsPlainNumbers(t *testing.T) {
	s := &Server{Emails: &quotaRecorder{}, Platform: okOperator{}}
	w := httptest.NewRecorder()
	s.listExcelQuotas(w, httptest.NewRequest("GET", "/api/platform/excel-quotas", nil))
	if w.Code != 200 {
		t.Fatalf("HTTP %d：%s", w.Code, w.Body.String())
	}

	var env struct {
		Data struct {
			CurrentMonth string            `json:"currentMonth"`
			Quotas       []json.RawMessage `json:"quotas"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("响应不是合法 JSON：%v\n%s", err, w.Body.String())
	}
	if env.Data.CurrentMonth != "2026-08" {
		t.Fatalf("月份丢了：%q", env.Data.CurrentMonth)
	}
	if len(env.Data.Quotas) != 1 {
		t.Fatalf("行数 = %d，该是 1", len(env.Data.Quotas))
	}
	row := string(env.Data.Quotas[0])
	for _, want := range []string{`"estimatedCost":"0.4500"`, `"currency":"USD"`} {
		if !strings.Contains(row, want) {
			t.Fatalf("平台列表少了 %s——金额现在只有这一个去处\n实际：%s", want, row)
		}
	}
	// 数字必须是数字。这条路走 encoding/json（不是 protojson），所以
	// "monthlyRuns":200 而不是 "200"；前端就是按这个当数字用的。
	for _, want := range []string{`"monthlyRuns":200`, `"usedThisMonth":41`, `"tenantId":2`} {
		if !strings.Contains(row, want) {
			t.Fatalf("期望 %s 是裸数字，实际：%s", want, row)
		}
	}
}
