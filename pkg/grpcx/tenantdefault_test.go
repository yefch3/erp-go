package grpcx

import (
	"bytes"
	"context"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// 「没有登录用户就算第一家公司」这句默认值的可见性。
//
// 它已经咬过一次：提单提醒在没登录用户的上下文里问「物流部都有谁」，拿回第一
// 家的人发给了每一家公司。默认值暂时不拆（登录、健康检查合法地没有登录态），
// 但每次在合法名单之外兜底都必须留下一行带方法名的警告——日志安静了才有证据
// 拆掉它。这组测试钉住：警告该响时响、不该响时不响、行为本身不变。
func callThrough(t *testing.T, method string, op Operator, log *slog.Logger) Operator {
	t.Helper()
	// 和 UnaryClientPropagator 同一套规则：有登录态才携带身份声明，签名
	// 永远覆盖发出去的原样。
	md := metadata.MD{}
	if op.TenantID != 0 || op.EmployeeID != 0 || op.Name != "" {
		md = metadata.Pairs(
			mdTenantID, strconv.FormatInt(op.TenantID, 10),
			mdEmployeeID, strconv.FormatInt(op.EmployeeID, 10),
			mdEmployeeName, url.QueryEscape(op.Name),
		)
	}
	md = metadata.Join(md, signed(op, method, time.Now()))
	ctx := metadata.NewIncomingContext(context.Background(), md)
	var seen Operator
	_, err := unaryOperator(log)(ctx, nil,
		&grpc.UnaryServerInfo{FullMethod: method},
		func(ctx context.Context, _ any) (any, error) {
			seen, _ = OperatorFromContext(ctx)
			return nil, nil
		})
	if err != nil {
		t.Fatalf("call refused: %v", err)
	}
	return seen
}

func TestTenantlessBackgroundCallWarnsLoudly(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))

	method := "/erp.iam.v1.AccessService/ListRoleMembers"
	seen := callThrough(t, method, Operator{}, log)

	if seen.TenantID != 1 {
		t.Fatalf("行为不该变：无主调用仍然落到 1 号公司，实际 %d", seen.TenantID)
	}
	out := buf.String()
	if !strings.Contains(out, "defaulted to tenant 1") || !strings.Contains(out, method) {
		t.Fatalf("后台任务忘带公司号时必须留下带方法名的警告，实际日志：%q", out)
	}
}

func TestLoginStaysQuiet(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))

	callThrough(t, "/erp.iam.v1.AuthService/Login", Operator{}, log)

	if strings.Contains(buf.String(), "defaulted") {
		t.Fatalf("登录本来就没有登录态，警告响个不停只会教人忽略它：%q", buf.String())
	}
}

func TestACallWithATenantNeverWarns(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))

	seen := callThrough(t, "/erp.iam.v1.AccessService/ListRoleMembers",
		Operator{TenantID: 2, EmployeeID: 5, Name: "王"}, log)

	if seen.TenantID != 2 {
		t.Fatalf("带了公司号的调用不该被改写，实际 %d", seen.TenantID)
	}
	if strings.Contains(buf.String(), "defaulted") {
		t.Fatalf("带公司号的调用不该触发警告：%q", buf.String())
	}
}
