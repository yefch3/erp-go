package app

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"time"
)

// IdleRestartEvery 是 IDLE 多久主动重发一次。
//
// RFC 2177 允许服务器 29 分钟不见动静就挂断，所以从前是 24 分钟。但 263 等
// 不了那么久：生产上四个 263 账号一天掉线 55 次，错误全是 "connection closed"
// 和 "disconnected while idling"——不是我们这边出错，是对方几分钟就把空闲连接
// 掐了。掐一次我们就记一条告警、退避一次（最长 10 分钟），这段时间里推送
// 是停的，只剩两分钟一轮的轮询兜底。
//
// 5 分钟一续，比所有已知服务商的掐线阈值都短，而一次续命只是一条 DONE 加一
// 条 IDLE，不是重新握手。代价可以忽略，换来的是推送不再断断续续。
const IdleRestartEvery = 5 * time.Minute

// benignReconnectDelay 是对方挂断后重连前至少等多久。见 watchMailbox。
const benignReconnectDelay = 5 * time.Second

// BenignIdleDrop 说这次 IDLE 结束是不是「对方挂了电话」。
//
// 三句话都来自 go-imap，而且都没导出成变量，只能按原文认。它们的共同点是：
// 连接没了，但不是拒绝——不是密码错、不是打不开信箱、不是我们发了它不认的
// 命令。263 这类几分钟就掐空闲连接的服务器，一天会制造几十次这种"错误"。
//
// 把它们和真正的失败分开，调用方才能做对两件事：不为它写告警（告警是给人
// 看的，一天几十条同样的话之后没人再看），也不为它退避——退避是给拒绝我们
// 的服务器准备的，对一个正常挂断的服务器退避，只会让推送白白停几分钟。
func BenignIdleDrop(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, s := range []string{
		"disconnected while idling",
		"imap: connection closed",
		"connection closed during command execution",
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// TransportFailure 说这个错误是「线路的问题」，不是「对方拒绝了我们」。
//
// 它和 CredentialRejected 是一对：IMAP 登录失败时，go-imap 给的错误没有类型
// ——服务器说 NO 是 errors.New(原文)，连接在 LOGIN 中途断掉也是一个普通错误。
// 从错误值上分不出「密码错」和「线断了」，只能把线路错误逐一列出来排除掉：
// 网络层的超时/重置、EOF、连接已关闭、上下文取消，以及 go-imap 自己那几句
// 挂断原文（见 BenignIdleDrop）。
//
// 分不清的代价是这个 PR 要消灭的东西反过来：263 在握手中途掐一次线，就会被
// 记成「授权码被拒」，横幅劝人重登。宁可漏判（少一颗按钮），不可误判。
func TransportFailure(err error) bool {
	if err == nil {
		return false
	}
	if BenignIdleDrop(err) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	for _, sentinel := range []error{
		io.EOF, io.ErrUnexpectedEOF, net.ErrClosed,
		context.DeadlineExceeded, context.Canceled,
	} {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	msg := err.Error()
	for _, s := range []string{"i/o timeout", "connection reset", "broken pipe", "EOF"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
