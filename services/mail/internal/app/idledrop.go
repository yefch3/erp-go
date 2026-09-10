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

// 一台服务器把 IDLE 掐得太勤时，就别再跟它用 IDLE 了。
//
// 实测（生产，11 小时）：
//
//	263    4 个账号   116 次掉线   最短 66 秒一次
//	163    1 个账号     5 次
//	126    1 个账号     0 次
//	Gmail  2 个账号     0 次
//
// 263 每 66 秒掐一次，而我们 5 分钟才主动续一次——那个闹钟永远轮不到响，
// 每一圈都是「对方先挂断、我们再重拨」。功能上没坏（掉了就重连，收信照常），
// 代价是那个信箱每分钟重新登录一次，一天一千多次。QQ 明确会限制登录频率，
// 别的服务商也没有理由喜欢这个。
//
// 所以：连续几圈都撑不过一分钟，就认定这台服务器的 IDLE 没有意义，停掉推送，
// 交给两分钟一轮的轮询。**代价是新信最多晚两分钟到**，换掉每分钟一次的重新
// 登录——而那两分钟本来就是没人在看的箱的待遇。
//
// 判断按账号存在内存里，**同时落一份到库里**（mail_accounts.push_mode）。
//
// 这里原本写着「不落库是有意的——这是一台服务器此刻的脾气，不是一条需要长期
// 记住的事实」。那句话没说错，但它漏了两件事，所以 2026-09-09 反转了：
//
//   一、没法查。「这个箱现在走推送还是轮询」只存在内存里，问一次就得翻日志，
//       而那几行日志只在有人正开着邮件页时才产生——没人看的时候这个循环
//       根本不跑，一条都没有。业务问起来只能靠推断。
//   二、每次重启忘光。每部署一次就重新学一遍：先被掐三圈（约三分钟、三次
//       重新登录）才退回轮询。
//
// 「此刻的脾气」那句仍然成立，所以落的不是永久结论：连判断时间一起存，过了
// 冷静期照样重新试 IDLE。存的是「上次学到的」，不是「从此就是这样」。
const (
	// idleTooShort 是「这一圈根本没撑住」的界线。263 实测 66 秒，留一点余量。
	idleTooShort = 90 * time.Second
	// idleGiveUpAfter 是连续几圈都太短就放弃。三圈≈三分钟，足够把「偶尔一次
	// 网络抖动」和「这台服务器就是这样」分开。
	idleGiveUpAfter = 3
	// idleRetryAfter 是放弃之后隔多久再试一次 IDLE。服务器会改配置，网络会
	// 变好；一直不试就等于永远回不去推送。
	idleRetryAfter = 30 * time.Minute
)

// ErrPushUnsupported 说这台服务器根本不支持 IDLE，别再为它挂连接。
//
// 这不是失败，是一个事实：263 就是这样（能力列表里没有 IDLE）。适配器不能
// 让 go-imap 悄悄退化成「挂着连接每 60 秒 NOOP」——那比普通轮询更贵，连接
// 一断就要重新握手加登录。收到这个就把这个箱交给两分钟一轮的轮询。
var ErrPushUnsupported = errors.New("mail: host does not support IMAP push")

// 落库的三态。空串是「还没学到」——新绑的箱、以及这一列刚加上时的存量。
const (
	PushModeIdle = "IDLE"
	PushModePoll = "POLL"
)

// idleHealth 记着每个账号的 IDLE 撑得住撑不住。
type idleHealth struct {
	shortRuns int
	// 放弃推送到什么时候为止；零值表示没放弃。
	quietUntil time.Time
	// 库里那一列此刻是什么。只在结论**变了**的时候才写库：IDLE 正常时每
	// 二十几分钟就是一圈，圈圈都写等于把一次读变成一次写，而结论几乎不变。
	stored string
}

// seed 用库里存的结论开局，省掉「重启之后重新被掐三圈才想起来」那三分钟。
//
// checkedAt 是上次学到的时刻。冷静期从那时算起，不是从进程启动算起——否则
// 每重启一次就把冷静期重新拉满，一台已经判定没用的服务器会被无限期地不再
// 尝试，而它可能早就改好了。
func (h *idleHealth) seed(mode string, checkedAt time.Time, now time.Time) {
	h.stored = mode
	if mode != PushModePoll || checkedAt.IsZero() {
		return
	}
	if until := checkedAt.Add(idleRetryAfter); now.Before(until) {
		h.quietUntil = until
	}
}

// modeNow 是此刻该记进库里的结论。
//
// 按「正在冷静期」判断，不按「上一圈成没成」：一圈 IDLE 因为收到新信而正常
// 结束也叫成功，但那不代表这台服务器的 IDLE 可用——它可能三圈里有两圈是被
// 掐断的。冷静期才是「我们已经放弃推送」的那个信号。
func (h *idleHealth) modeNow(now time.Time) string {
	if h.idleWorthTrying(now) {
		return PushModeIdle
	}
	return PushModePoll
}

// changed 说此刻的结论和库里存的是不是不一样；一样就别写。
func (h *idleHealth) changed(now time.Time) (mode string, differs bool) {
	m := h.modeNow(now)
	return m, m != h.stored
}

// noteIdleRun 记一圈 IDLE 的结果，并回答「下一圈还用不用 IDLE」。
//
// lasted 是这一圈从开始到结束的时长，benign 是不是被对方挂断的。只有「被挂断
// 且撑得太短」才算一次不合格：正常收到新信而结束的一圈，哪怕很短也是成功。
func (h *idleHealth) noteIdleRun(now time.Time, lasted time.Duration, benign bool) (useIdle bool) {
	if !benign || lasted >= idleTooShort {
		h.shortRuns = 0
		return true
	}
	h.shortRuns++
	if h.shortRuns < idleGiveUpAfter {
		return true
	}
	h.quietUntil = now.Add(idleRetryAfter)
	h.shortRuns = 0
	return false
}

// idleWorthTrying 说现在该不该再开一圈 IDLE。
func (h *idleHealth) idleWorthTrying(now time.Time) bool {
	return h.quietUntil.IsZero() || !now.Before(h.quietUntil)
}

// idlePauseCheckEvery 是冷静期里多久醒一次看看该不该回去用 IDLE。
// 醒来只看一眼时钟，不碰网络。
const idlePauseCheckEvery = time.Minute
