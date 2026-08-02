package app

import (
	"context"
	"fmt"
	"time"

	"github.com/sgao19/erp-go/services/notification/internal/store"
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
func (s *Service) overQuota(ctx context.Context, tenantID, senderID int64) (time.Duration, string) {
	host, err := s.q.GetMailHost(ctx, tenantID)
	if err != nil {
		return 0, ""
	}
	acct, err := s.q.GetMailAccountSecret(ctx, store.GetMailAccountSecretParams{
		TenantID: tenantID, EmployeeID: senderID,
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

	if counts.ThisHour >= host.HourlyQuota {
		// Until the top of the next hour, plus a little, so a fleet of
		// workers does not all resume on the same second.
		wait := time.Until(time.Now().Truncate(time.Hour).Add(time.Hour)) + 30*time.Second
		return wait, fmt.Sprintf("已达本小时发送上限（%d 封），排队等待", host.HourlyQuota)
	}
	if counts.Last24h >= host.DailyQuota {
		return time.Hour, fmt.Sprintf("已达 24 小时发送上限（%d 封），排队等待", host.DailyQuota)
	}
	return 0, ""
}

// countSend records one accepted message against the sender's mailbox.
//
// Best effort and deliberately after the send: a counter that fails to
// increment must never stop mail going out. The cost of an occasional
// undercount is sending slightly over the limit once; the cost of treating
// it as fatal is a stalled queue.
func (s *Service) countSend(ctx context.Context, tenantID, senderID int64) {
	acct, err := s.q.GetMailAccountSecret(ctx, store.GetMailAccountSecretParams{
		TenantID: tenantID, EmployeeID: senderID,
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
