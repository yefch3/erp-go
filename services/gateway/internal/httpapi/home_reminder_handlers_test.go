package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"

	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

type homeAccessStub struct {
	iamv1.AccessServiceClient
	allowed map[string]bool
}

func (s homeAccessStub) CheckPermission(_ context.Context, req *iamv1.CheckPermissionRequest, _ ...grpc.CallOption) (*iamv1.CheckPermissionResponse, error) {
	return &iamv1.CheckPermissionResponse{Allowed: s.allowed[req.GetPermissionCode()]}, nil
}

type homeReceiptStub struct{ exv1.ReceiptServiceClient }

func (homeReceiptStub) ListReceivableReminders(context.Context, *exv1.ListReceivableRemindersRequest, ...grpc.CallOption) (*exv1.ListReceivableRemindersResponse, error) {
	today := time.Now().UTC()
	return &exv1.ListReceivableRemindersResponse{
		UnreadTotal: 2,
		Items: []*exv1.ReceivableReminder{
			{Id: 11, ContractNo: "CT-OVERDUE", Title: "逾期应收", DueDate: today.AddDate(0, 0, -1).Format("2006-01-02"), Unread: true},
			{Id: 12, ContractNo: "CT-SOON", Title: "即将到期", DueDate: today.AddDate(0, 0, 2).Format("2006-01-02"), Unread: true},
		},
	}, nil
}

type homeShippingStub struct {
	shippingv1.ShippingServiceClient
}

func (homeShippingStub) ListArrivalNotifications(context.Context, *shippingv1.ListArrivalNotificationsRequest, ...grpc.CallOption) (*shippingv1.ListArrivalNotificationsResponse, error) {
	return &shippingv1.ListArrivalNotificationsResponse{}, nil
}

func (homeShippingStub) ListBlReminders(context.Context, *shippingv1.ListBlRemindersRequest, ...grpc.CallOption) (*shippingv1.ListBlRemindersResponse, error) {
	return &shippingv1.ListBlRemindersResponse{}, nil
}

func (homeShippingStub) ListOperationalAlerts(context.Context, *shippingv1.ListOperationalAlertsRequest, ...grpc.CallOption) (*shippingv1.ListOperationalAlertsResponse, error) {
	return &shippingv1.ListOperationalAlertsResponse{UnreadCount: 1, Items: []*shippingv1.OperationalAlert{{Id: 31, ScheduleNo: "S-D6-001", Title: "ETD 延后，请联系工厂", DueDate: time.Now().UTC().AddDate(0, 0, 2).Format("2006-01-02")}}}, nil
}

// 首页汇总必须只读取当前员工有来源权限的数据，并返回统一的到期和未读统计。
func TestListHomeRemindersUsesSourcePermissionsAndSummary(t *testing.T) {
	s := &Server{
		Access:   homeAccessStub{allowed: map[string]bool{"export:receipt:read": true}},
		Receipts: homeReceiptStub{},
		Shipping: homeShippingStub{},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/home/reminders?page=1&page_size=10", nil)
	req = req.WithContext(grpcx.WithOperator(req.Context(), grpcx.Operator{TenantID: 1, EmployeeID: 9}))
	recorder := httptest.NewRecorder()
	s.listHomeReminders(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, want := range []string{`"total":3`, `"upcoming":2`, `"overdue":1`, `"unread":3`, `CT-OVERDUE`, `/receivable-due?keyword=CT-OVERDUE`, `SHIPPING_ACTION`, `S-D6-001`} {
		if !strings.Contains(body, want) {
			t.Fatalf("response missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `"source":"ARRIVAL"`) || strings.Contains(body, `"source":"BL"`) {
		t.Fatalf("没有船期权限时不应返回船期来源: %s", body)
	}
}

func TestClassifyHomeReminderDate(t *testing.T) {
	now := time.Date(2026, 8, 27, 18, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		value string
		want  string
	}{
		{"2026-08-26", "OVERDUE"},
		{"2026-08-27", "UPCOMING"},
		{"2026-08-30", "UPCOMING"},
		{"2026-08-31", "REMINDER"},
		{"", "REMINDER"},
	} {
		if got := classifyHomeReminderDate(tc.value, now); got != tc.want {
			t.Fatalf("classifyHomeReminderDate(%q)=%s want=%s", tc.value, got, tc.want)
		}
	}
}

func TestParseHomeReminderIDsRejectsInvalidValues(t *testing.T) {
	if _, ok := parseHomeReminderIDs([]string{"9", "bad"}); ok {
		t.Fatal("非法提醒编号不应被接受")
	}
	if ids, ok := parseHomeReminderIDs([]string{"9", "10"}); !ok || len(ids) != 2 || ids[1] != 10 {
		t.Fatalf("合法提醒编号解析失败: %v %v", ids, ok)
	}
}
