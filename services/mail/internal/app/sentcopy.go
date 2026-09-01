package app

import (
	"context"
	"strings"
	"time"
)

// saveSentCopy files what we just sent into the mailbox's own 已发送 folder.
//
// Sending a message and keeping a copy of it are two unrelated acts. The SMTP
// conversation hands the message to a relay and ends there; nothing in it puts
// anything into the sender's own mailbox. Gmail joins the two by itself, which
// is why this was never needed until a plain host arrived: 263 relays the mail
// and keeps nothing, so 已发送 stayed empty — in the ERP and in 263's own web
// client alike, because there was genuinely nothing on the server to show.
//
// Best-effort on purpose. By the time this runs the message has been accepted
// by the relay and is on its way; failing the send because the copy could not
// be filed would turn a bookkeeping problem into a lost message.
// accountID 是这封信真正从哪个箱发出去的；0 = 00047 之前入队的行，退回按人
// 查默认箱。
//
// **这一条从前查的就是默认箱，而那正是 00047 说要修的那个毛病。** 默认 QQ、
// 从 163 发：副本被 APPEND 进 QQ 的 已发送，同步回来时带的是 QQ 的
// account_id，于是这封信在 163 的 已发送 里**一行都没有**——两条腿都筛掉了
// 它（服务器副本那条算 QQ 的，ERP 那条被「已经有副本了」的去重干掉）。
//
// 还有两个变种，因为 hostFilesItsOwnSentCopy 是按账号上的 SMTP 主机判的：
// 默认 Gmail、从 163 发 → 提前返回，**一份副本都不存**，163 自己的网页版
// 已发送 也是空的；默认 163、从 Gmail 发 → 往一个根本没发过这封信的箱里
// 塞一份副本。
func (s *Service) saveSentCopy(ctx context.Context, tenantID, senderID, accountID int64, raw []byte) {
	if s.mailbox == nil || len(raw) == 0 {
		return
	}
	accountID, err := s.sendingAccount(ctx, tenantID, senderID, accountID)
	if err != nil {
		s.log.Warn("could not file a copy in the sent folder", "sender", senderID, "err", err)
		return
	}
	acct, err := s.ForAccount(ctx, tenantID, accountID)
	if err != nil {
		s.log.Warn("could not file a copy in the sent folder", "sender", senderID, "err", err)
		return
	}
	s.fileSentCopy(ctx, acct, raw)
}

// fileSentCopy is saveSentCopy once the mailbox is known.
func (s *Service) fileSentCopy(ctx context.Context, acct MailAccount, raw []byte) {
	if s.mailbox == nil || len(raw) == 0 || hostFilesItsOwnSentCopy(acct) {
		return
	}
	folder, err := s.specialFolderOf(ctx, acct, "sent")
	if err != nil {
		s.log.Warn("could not locate the sent folder to file a copy",
			"account", acct.AccountID, "err", err)
		return
	}
	if err := s.mailbox.AppendMessage(ctx, acct, folder, raw, time.Now()); err != nil {
		s.log.Warn("could not file a copy in the sent folder",
			"account", acct.AccountID, "err", err)
	}
}

// hostFilesItsOwnSentCopy says whether the relay we just sent through puts a
// copy into the mailbox's Sent folder without being asked.
//
// Gmail does. Appending there as well would leave two copies of every sent
// mail — and not only in this ERP: the duplicate would be sitting in Gmail
// itself, where we have no business creating it.
//
// This is deliberately a list of exceptions rather than a list of hosts we
// serve, so a mail host nobody has met yet gets the copy. The two mistakes are
// not symmetrical: guessing wrong this way shows a duplicate somebody can
// delete, guessing wrong the other way empties 已发送 with no sign of why.
//
// Matched on the SMTP host because this is a property of the relay, not of the
// address: a company can perfectly well relay through one provider while
// reading its mail somewhere else.
func hostFilesItsOwnSentCopy(acct MailAccount) bool {
	h := strings.ToLower(strings.TrimSpace(acct.Host))
	for _, d := range []string{"gmail.com", "googlemail.com"} {
		// The dot is load-bearing: a plain suffix test also matches
		// notgmail.com, and a company on a domain like that would silently
		// lose every copy of its sent mail.
		if h == d || strings.HasSuffix(h, "."+d) {
			return true
		}
	}
	return false
}
