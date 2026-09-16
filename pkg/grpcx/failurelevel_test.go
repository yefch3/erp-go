package grpcx

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sgao19/erp-go/pkg/apierr"
)

// 监控那条规则是「某个服务 5 分钟内打了 ERROR 就告警」（deploy/aws/05-alerts.sh），
// 所以这一档的含义必须是「有东西坏了」。这组用例钉住哪些不算坏。
//
// 依据是实测：告警上线第一天（2026-09-15）响了六次，一次真故障都没有——
// 一半是业务拒绝，一次是我们自己在部署。
func TestOnlyRealFailuresAreLoggedAsErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want slog.Level
	}{
		{
			// 「该激活链接已经使用过，请直接登录」——系统正常工作。
			"业务拒绝（还没转成状态码的）",
			apierr.Invalid("IAM_ACTIVATE_USED", "该激活链接已经使用过，请直接登录"),
			slog.LevelWarn,
		},
		{
			"业务拒绝（已经是状态码的）",
			apierr.ToStatus(apierr.NotFound("MAIL_EXCEL_ATTACHMENT_NOT_FOUND", "附件不存在或未保存")),
			slog.LevelWarn,
		},
		{
			// 客户端关了页面。
			"取消（context 的哨兵）",
			context.Canceled,
			slog.LevelWarn,
		},
		{
			"取消（gRPC 状态码）",
			status.Error(codes.Canceled, "context canceled"),
			slog.LevelWarn,
		},
		{
			// 换版本那几秒，gRPC 客户端给的就是这句，而且**不带 Canceled 码**。
			// 2026-09-15 一次部署就是靠这句把网关和采购同时点着的。
			"换版本时连接被关掉",
			errors.New("grpc: the client connection is closing"),
			slog.LevelWarn,
		},
		{
			// 这些还是要响。
			"真的内部错误",
			errors.New("pq: connection refused"),
			slog.LevelError,
		},
		{
			"下游连不上",
			status.Error(codes.Unavailable, "connection refused"),
			slog.LevelError,
		},
		{
			"超时",
			status.Error(codes.DeadlineExceeded, "context deadline exceeded"),
			slog.LevelError,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := failureLevel(c.err); got != c.want {
				t.Fatalf("failureLevel = %v, want %v", got, c.want)
			}
		})
	}
}

// 业务拒绝不该再打「unhandled error」那一行。它不是没处理的错，它正是处理过
// 的结果；而那一行是 ERROR，会把告警点着。
//
// 失败本身不会不见：rpc 那一行照记，带着 biz_code。
func TestBusinessRefusalLeavesNoUnhandledErrorLine(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	refusal := apierr.Invalid("IAM_ACTIVATE_USED", "该激活链接已经使用过，请直接登录")

	_, err := unaryError(log)(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/erp.iam.v1.AuthService/PeekInvitation"},
		func(context.Context, any) (any, error) { return nil, refusal })

	if apierr.CodeFromStatus(err) != "IAM_ACTIVATE_USED" {
		t.Fatalf("业务码该原样交出去，得到 %v", err)
	}
	if strings.Contains(buf.String(), "unhandled error") {
		t.Fatalf("业务拒绝不该记成没处理的错：\n%s", buf.String())
	}
}

// 取消不该说成 Internal——那是在告诉调用方「服务器坏了」，而真相是这次调用
// 被取消了（人关了页面，或者进程在换版本）。
func TestCancellationIsNotDressedUpAsAnInternalError(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	_, err := unaryError(log)(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/erp.procurement.v1.SourcingService/InquiryWorkspace"},
		func(context.Context, any) (any, error) {
			return nil, errors.New("grpc: the client connection is closing")
		})

	if status.Code(err) != codes.Canceled {
		t.Fatalf("该是 Canceled，得到 %v", status.Code(err))
	}
	if strings.Contains(buf.String(), `"level":"ERROR"`) {
		t.Fatalf("换版本掐断的调用不该记 ERROR：\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "call cancelled") {
		t.Fatalf("但要留痕，好查：\n%s", buf.String())
	}
}
