package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

type fakeNetErr struct{ timeout bool }

func (e fakeNetErr) Error() string   { return "read tcp 1.2.3.4:993: i/o timeout" }
func (e fakeNetErr) Timeout() bool   { return e.timeout }
func (e fakeNetErr) Temporary() bool { return false }

// 登录时线断了不是凭据错。go-imap 把两种失败返回成同一种普通错误，这里列出
// 所有"线路"形态，钉住它们都不会被当成授权码错。
func TestTransportFailureCoversEveryWayALineCanDie(t *testing.T) {
	transport := []error{
		fakeNetErr{timeout: true},
		fmt.Errorf("邮箱拒绝了这个授权码：%w", fakeNetErr{}),
		io.EOF, io.ErrUnexpectedEOF, net.ErrClosed,
		context.DeadlineExceeded, context.Canceled,
		errors.New("imap: connection closed"),
		errors.New("disconnected while idling"),
		errors.New("imap: connection closed during command execution"),
		errors.New("write tcp: broken pipe"),
		errors.New("read: connection reset by peer"),
	}
	for _, e := range transport {
		if !TransportFailure(e) {
			t.Errorf("%v 是线路问题，不该被当成凭据错", e)
		}
	}
	credential := []error{
		errors.New("LOGIN Login error or password error"),
		errors.New("邮箱拒绝了这个授权码：NO [AUTHENTICATIONFAILED] Invalid credentials"),
		errors.New("EXAMINE Unsafe Login. Please contact kefu@188.com"),
	}
	for _, e := range credential {
		if TransportFailure(e) {
			t.Errorf("%v 是服务器的话，不是线路问题", e)
		}
	}
	if TransportFailure(nil) {
		t.Error("nil 不是失败")
	}
}

// Google 令牌接口的应答：只有 invalid_grant 是"重新登录能修好"的。
func TestOnlyARevokedGoogleGrantCountsAsCredentialRejected(t *testing.T) {
	if !googleGrantRevoked(googleOAuthError{code: "invalid_grant", desc: "Token has been expired or revoked."}) {
		t.Error("invalid_grant 是授权没了，该算凭据被拒")
	}
	if !googleGrantRevoked(fmt.Errorf("外面包一层：%w", googleOAuthError{code: "invalid_grant"})) {
		t.Error("包一层也要认得出")
	}
	for _, e := range []error{
		googleOAuthError{code: "invalid_client", desc: "Unauthorized"},
		googleOAuthError{code: "unauthorized_client"},
		fmt.Errorf("无法连接 Google 授权服务器：%w", fakeNetErr{timeout: true}),
		errors.New("Google 应答无法解析：unexpected EOF"),
	} {
		if googleGrantRevoked(e) {
			t.Errorf("%v 不是授权被撤销，劝用户重登修不好：", e)
		}
	}
}

func TestBenignReconnectDelayIsAFloorNotABackoff(t *testing.T) {
	if benignReconnectDelay < time.Second || benignReconnectDelay > 30*time.Second {
		t.Fatalf("重连最小间隔 %v 不合理：太短会空转，太长推送就停了", benignReconnectDelay)
	}
}
