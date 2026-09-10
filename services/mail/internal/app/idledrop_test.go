package app

import (
	"errors"
	"fmt"
	"testing"
)

// 三句话是 go-imap 的原文，库没导出它们，这里钉住：升级库之后要是措辞变了，
// 这条先红，而不是线上告警悄悄变多。
func TestBenignIdleDropRecognisesTheHostHangingUp(t *testing.T) {
	benign := []error{
		errors.New("disconnected while idling"),
		errors.New("imap: connection closed"),
		fmt.Errorf("收取邮件失败：%w", errors.New("imap: connection closed")),
		errors.New("imap: connection closed during command execution"),
	}
	for _, e := range benign {
		if !BenignIdleDrop(e) {
			t.Errorf("%q 是对方挂断，应该算良性", e)
		}
	}
	notBenign := []error{
		nil,
		errors.New("邮箱拒绝了这个授权码：LOGIN Login error or password error"),
		errors.New("打开 INBOX 失败：EXAMINE Unsafe Login"),
		errors.New("dial tcp: i/o timeout"),
	}
	for _, e := range notBenign {
		if BenignIdleDrop(e) {
			t.Errorf("%v 不是挂断，不该算良性", e)
		}
	}
}

// 续命间隔必须比 RFC 的 29 分钟短，也必须比 263 实测的几分钟短。
func TestIdleRestartIsWellUnderEveryKnownCutoff(t *testing.T) {
	if IdleRestartEvery >= 10*60e9 {
		t.Fatalf("263 几分钟就掐线，%v 太长", IdleRestartEvery)
	}
	if IdleRestartEvery < 60e9 {
		t.Fatalf("%v 太短，会变成无意义的刷屏", IdleRestartEvery)
	}
}
