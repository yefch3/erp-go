package app

import (
	"context"
	"fmt"
	"time"

	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// overQuota reports how long this sender must wait, and why.
//
// A mail host is not an API service: exceeding its limit is not a rate-limit
// response you shrug off and retry, it is a mark against the mailbox that can
// end in deferrals or an outright block for everybody in the company. So the
// pacing is ours to do, and it errs towards waiting.
//
// A zero duration means "go ahead". Anything that cannot be determined —
// no account, no host row, a database error — also means "go ahead", because
// the send path already fails loudly on a missing account and a counter
// problem must not become a silent mail outage.
func (s *Service) overQuota(ctx context.Context, tenantID, senderID, accountID int64) (time.Duration, string) {
	// 限额跟着信箱走，不再读租户那一行：263 的套餐上限和 Gmail 的
	// 500/天不是一个量纲，一个人同时用两家时一份租户级配额说不出话。
	//
	// 而「跟着信箱走」要求认的是**这封信的那个箱**。从前这里查的是默认箱，
	// 于是从 163 发的一百封全记在 QQ 的计数上：163 自己的上限一辈子不生效
	// （拿默认箱的额度往严格的那家灌），而第 101 封会被 QQ 的上限拦下来，
	// 提示里写着一个 QQ 从没达到过的数字。
	accountID, err := s.sendingAccount(ctx, tenantID, senderID, accountID)
	if err != nil {
		return 0, ""
	}
	acct, err := s.q.GetMailAccountSecret(ctx, store.GetMailAccountSecretParams{
		TenantID: tenantID, ID: accountID,
	})
	if err != nil {
		return 0, ""
	}
	counts, err := s.q.CountSentInWindow(ctx, store.CountSentInWindowParams{
		TenantID: tenantID, AccountID: acct.ID,
	})
	if err != nil {
		s.log.Warn("could not read send counters, letting the message through",
			"account", acct.ID, "err", err)
		return 0, ""
	}

	if counts.ThisHour >= acct.HourlyQuota {
		// Until the top of the next hour, plus a little, so a fleet of
		// workers does not all resume on the same second.
		wait := time.Until(time.Now().Truncate(time.Hour).Add(time.Hour)) + 30*time.Second
		return wait, fmt.Sprintf("已达本小时发送上限（%d 封），排队等待", acct.HourlyQuota)
	}
	if counts.Last24h >= acct.DailyQuota {
		return time.Hour, fmt.Sprintf("已达 24 小时发送上限（%d 封），排队等待", acct.DailyQuota)
	}
	return 0, ""
}

// countSend records one accepted message against the sender's mailbox.
//
// Best effort and deliberately after the send: a counter that fails to
// increment must never stop mail going out. The cost of an occasional
// undercount is sending slightly over the limit once; the cost of treating
// it as fatal is a stalled queue.
func (s *Service) countSend(ctx context.Context, tenantID, senderID, accountID int64) {
	accountID, err := s.sendingAccount(ctx, tenantID, senderID, accountID)
	if err != nil {
		return
	}
	acct, err := s.q.GetMailAccountSecret(ctx, store.GetMailAccountSecretParams{
		TenantID: tenantID, ID: accountID,
	})
	if err != nil {
		return
	}
	if _, err := s.q.BumpSendCounter(ctx, store.BumpSendCounterParams{
		TenantID: tenantID, AccountID: acct.ID,
	}); err != nil {
		s.log.Warn("could not count a send against the quota", "account", acct.ID, "err", err)
	}
}

// sendingAccount 认这封信是从哪个信箱发出去的。
//
// accountID = 0 只有一种来源：00047 之前入队、还没发出去的那些行。对它们退回
// 老办法（按人查默认箱）——那正是不做这次改动时会反查到的那一个，所以部署
// 那一刻队列里积着的信不会换发件人，也不会卡住。
//
// 队列排空之后这条分支就不再有人走。留着是因为「排空」没有一个能断言的时刻。
func (s *Service) sendingAccount(ctx context.Context, tenantID, senderID, accountID int64) (int64, error) {
	if accountID > 0 {
		return accountID, nil
	}
	return s.defaultAccountIDFor(ctx, tenantID, senderID)
}
