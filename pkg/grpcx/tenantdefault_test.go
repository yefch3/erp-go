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

// 「没有登录用户就算第一家公司」已经拆掉了：白名单之外，没带公司号的调用
// 一律当场拒绝。
//
// 那句默认值咬过一次：提单提醒在没登录用户的上下文里问「物流部都有谁」，
// 拿回第一家的人发给了每一家公司——不报错、不留痕。拒绝把这种错变成当场、
// 大声、指名道姓的失败。白名单是逐个查证过的「合法无登录」：登录/激活/找回
// 密码（身份由令牌证明）、邮件图片与已读回执（取的人是收件的陌生人）、健康
// 探针。这组测试钉住：名单外拒绝且留痕、名单内放行且不再补 1 号、带了公司
// 号的原样通过。
func callThrough(t *testing.T, method string, op Operator, log *slog.Logger) Operator {
	t.Helper()
	seen, err := tryCall(t, method, op, log)
	if err != nil {
		t.Fatalf("call refused: %v", err)
	}
	return seen
}

// tryCall 是 callThrough 的可失败版本：拒绝本身就是被测行为。
func tryCall(t *testing.T, method string, op Operator, log *slog.Logger) (Operator, error) {
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
	return seen, err
}

func TestTenantlessBackgroundCallIsRefused(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))

	method := "/erp.iam.v1.AccessService/ListRoleMembers"
	_, err := tryCall(t, method, Operator{}, log)

	if err == nil {
		t.Fatal("没带公司号的后台调用通过了——它接下来就要拿第一家公司的数据干活")
	}
	out := buf.String()
	if !strings.Contains(out, "refused a call with no tenant") || !strings.Contains(out, method) {
		t.Fatalf("拒绝必须留下带方法名的日志，否则半夜坏掉的任务查无对证：%q", out)
	}
}

// 白名单内的公开方法放行，且公司号保持 0——不再有人被悄悄补成 1 号。
// 这两个方法取的人是收件的陌生人，定位凭据是令牌；0 号意味着万一将来有人
// 误读公司号，拿到的是空集而不是第一家公司的数据。
func TestPublicMailMethodsPassWithoutATenant(t *testing.T) {
	for _, method := range []string{
		"/erp.mail.v1.EmailService/FetchImage",
		"/erp.mail.v1.EmailService/RecordOpen",
		// 超大附件的下载。漏掉它的表现是：整个功能在生产上一点用没有——
		// 公开路由每次都被这道拦截拒在服务层之前，而对外看到的是一个
		// 404，和「token 不存在」长得一模一样，从外面完全看不出区别。
		// 这条测试就是为了让下一个公开方法不必再靠翻日志才发现。
		"/erp.mail.v1.EmailService/FetchAttachmentLink",
	} {
		var buf bytes.Buffer
		log := slog.New(slog.NewTextHandler(&buf, nil))
		seen, err := tryCall(t, method, Operator{}, log)
		if err != nil {
			t.Fatalf("%s 被拒了——收件人拉不到图片、已读回执全断：%v", method, err)
		}
		if seen.TenantID != 0 {
			t.Fatalf("%s 的公司号被补成了 %d——该保持 0", method, seen.TenantID)
		}
	}
}

func TestLoginPassesWithoutATenant(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))

	seen := callThrough(t, "/erp.iam.v1.AuthService/Login", Operator{}, log)

	if seen.TenantID != 0 {
		t.Fatalf("登录的公司号被补成了 %d——登录前没有公司可言", seen.TenantID)
	}
	if strings.Contains(buf.String(), "refused") {
		t.Fatalf("登录不该有拒绝日志：%q", buf.String())
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
	if strings.Contains(buf.String(), "refused") {
		t.Fatalf("带公司号的调用不该触发拒绝日志：%q", buf.String())
	}
}
