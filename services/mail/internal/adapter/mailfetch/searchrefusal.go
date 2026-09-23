package mailfetch

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-imap/commands"
	"github.com/emersion/go-imap/responses"

	"github.com/sgao19/erp-go/services/mail/internal/app"
)

// messageIDSearchRetryAfter 是一个信箱答了「不支持按 Message-ID 搜索」之后，
// 多久不再问它。
//
// 263 对 `UID SEARCH HEADER Message-Id` 一律回 "can't search that criteria"。
// 从前每次都照问：2026-09-23 生产上一天 54,081 次，全部被拒，而且每次被拒
// 都把连接当坏的扔掉、下一封再重新 TLS 握手登录一遍——一天五万多次登录打
// 在 263 上，只为了得到同一句「不支持」。
//
// 只记这一种拒绝（见 refused）。也不是永久记住：服务商可能哪天加上支持。
// 六小时问一次，一个信箱一天四次，够发现变化，又不至于把同一句话问上几万
// 遍。进程重启就清零，那时也只多问一次。
const messageIDSearchRetryAfter = 6 * time.Hour

// searchRefusals 记着哪些信箱最近拒绝过按 Message-ID 搜索。按账号记，
// 不按服务器记：同一台服务器上的两个箱各问各的一次，代价是多一次往返，
// 换来的是不必去猜「两个箱是不是同一种服务器」。
type searchRefusals struct {
	mu sync.Mutex
	at map[int64]time.Time
}

func newSearchRefusals() *searchRefusals {
	return &searchRefusals{at: map[int64]time.Time{}}
}

// recent 说这个信箱在 now 之前的 messageIDSearchRetryAfter 之内拒绝过。
func (r *searchRefusals) recent(accountID int64, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	at, ok := r.at[accountID]
	return ok && now.Sub(at) < messageIDSearchRetryAfter
}

// remember 记下一次拒绝，返回这是不是「新消息」——之前没记过、或者记录已经
// 过期。只有新消息才值得写一行日志。
func (r *searchRefusals) remember(accountID int64, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	at, ok := r.at[accountID]
	r.at[accountID] = now
	return !ok || now.Sub(at) >= messageIDSearchRetryAfter
}

// searchMessageID 在已选中的文件夹里按 Message-ID 找信，返回匹配的 UID。
//
// 用 Execute 而不是 UidSearch，只为一件事：**把两种失败分开**。UidSearch
// 把「服务器答了 NO」和「连接断了」报成同一种 error，而它们要的处理正好
// 相反——被拒绝时连接完好，下次原样再问也还是被拒；连接断了要扔掉重连，
// 下次很可能就好了。
//
// 发出去的命令和 UidSearch 一模一样（先 CHARSET UTF-8，服务器回 BADCHARSET
// 再用 US-ASCII 重问一次），换的只是看结果的方式。
//
// 返回三样：UID、refusal（服务器答了 NO/BAD，连接可继续用）、err（传输层
// 出错，连接不能再用）。refusal 和 err 至多一个非空。
func searchMessageID(c *client.Client, messageID string) (uids []uint32, refusal, err error) {
	crit := imap.NewSearchCriteria()
	// Angle brackets restored: they are part of the header value, and a
	// server matching literally will not find the message without them.
	crit.Header.Add("Message-Id", asAngled(messageID))
	for _, charset := range []string{"UTF-8", "US-ASCII"} {
		res := new(responses.Search)
		status, err := c.Execute(&commands.Uid{Cmd: &commands.Search{Charset: charset, Criteria: crit}}, res)
		if err != nil {
			return nil, nil, err
		}
		if status == nil {
			// No tagged answer at all: the connection went before the server
			// could reply, which is a transport failure, not a refusal.
			return nil, nil, errors.New("imap: connection closed during search")
		}
		if status.Code == imap.CodeBadCharset && charset == "UTF-8" {
			continue
		}
		if refusal := status.Err(); refusal != nil {
			return nil, refusal, nil
		}
		return res.Ids, nil, nil
	}
	return nil, nil, nil // unreachable: the second charset always returns
}

// refusedRecently 在这个信箱最近说过「不支持」时直接回
// ErrMessageIDSearchRefused，不借连接、不往服务器发任何东西。
func (f *IMAP) refusedRecently(acct app.MailAccount) error {
	if f.refusals.recent(acct.AccountID, time.Now()) {
		return fmt.Errorf("%w（这个信箱最近答过不支持，暂不再问）", app.ErrMessageIDSearchRefused)
	}
	return nil
}

// refused 处理服务器答了 NO/BAD 的那一次搜索。连接两种情况下都是好的。
//
//   - 答的是「不支持这种搜索条件」（263 那句）：记下来，六小时内不再问，回
//     ErrMessageIDSearchRefused。同一个信箱在记录有效期内只写一行日志。
//   - 别的拒绝（一时忙、暂不可用）：普通失败，不记——下次照问，很可能就好
//     了。记下来的话，一台支持搜索的服务器会因为一次抖动被当成不支持六小时。
func (f *IMAP) refused(acct app.MailAccount, folder string, refusal error) error {
	if !app.SearchCriterionUnsupported(refusal) {
		return fmt.Errorf("在 %s 中查找失败：%w", folder, refusal)
	}
	if f.refusals.remember(acct.AccountID, time.Now()) {
		f.log.Warn("mail host cannot search by Message-ID; not asking this mailbox again for a while",
			"account", acct.AccountID, "folder", folder, "retry_after", messageIDSearchRetryAfter.String(),
			"err", refusal)
	}
	return fmt.Errorf("在 %s 中查找失败：%w：%v", folder, app.ErrMessageIDSearchRefused, refusal)
}
