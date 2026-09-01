package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

// UnlockStore holds proof that somebody recently demonstrated control of
// their mailbox by logging in to the mail host with their own code.
//
// The token exists because an ERP session must not be enough to read mail: a
// JWT proves somebody logged in this morning, not that the person at the
// keyboard now is the mailbox's owner. It lives in Redis so every gateway
// replica sees it, and dies on its own rather than needing a logout path.
//
// **一个信箱一把，不是一个人一把。**
//
// 从前是一个人一把：验证一次，这个人所有的箱一起开，「退出邮箱」也一起关。
// 一个人只有一个箱的年代那是同一件事；有了多个箱之后，「退出」就变成了
// 一个没法只退一个的按钮——点一下，全退。
//
// 拆到箱这一级，同时保住「切换不用重新输密码」那条规矩：**验证成功时，
// 这个人名下每个箱各发一把**，浏览器全存着。切换箱只是换一把令牌，不问
// 密码；退出只撤当前这一把，别的箱照开。
//
// 箱是记在**值**里，不是键里——键里塞了的话，门那一层拿着令牌反查不到是
// 哪个箱，得把 33 个路由挨个改成带信箱参数。
//
// 它是「闲置多久失效」，不是「验证后多久必失效」——见 Check。
type UnlockStore struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewUnlockStore(addr string, ttl time.Duration) *UnlockStore {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &UnlockStore{rdb: redis.NewClient(&redis.Options{Addr: addr}), ttl: ttl}
}

func (u *UnlockStore) key(tenantID, employeeID int64, token string) string {
	return fmt.Sprintf("erp.mailunlock.t%d.e%d.%s", tenantID, employeeID, token)
}

// Grant mints a fresh token for one **mailbox**. Random rather than derived:
// a derived token could be reconstructed by anything that knows the inputs,
// and the whole point is that only this browser session holds it.
//
// accountID 存在值里。accountAll 表示「这个人的全部箱」——那是**旧令牌**
// 的语义，见 legacyAllMailboxes。
func (u *UnlockStore) Grant(ctx context.Context, tenantID, employeeID, accountID int64) (string, int, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", 0, err
	}
	token := hex.EncodeToString(b)
	v := strconv.FormatInt(accountID, 10)
	if err := u.rdb.Set(ctx, u.key(tenantID, employeeID, token), v, u.ttl).Err(); err != nil {
		return "", 0, err
	}
	return token, int(u.ttl.Seconds()), nil
}

// accountAll 是「这把令牌不限信箱」。
//
// 只有两种情况会出现：换版本之前发出去、还没到期的那些旧令牌（值是 "1"），
// 以及一个箱都没绑的人拿到的那把通行证。
const accountAll int64 = 0

// legacyAllMailboxes 认出换版本之前发出去的旧令牌。
//
// 旧令牌的值写死是 "1"，而新令牌的值是信箱 id。这两者会撞：id 恰好是 1 的
// 那个信箱，它的新令牌看起来和旧令牌一模一样。撞了的后果只是「这把令牌
// 被当成不限信箱」——比让全公司在部署那一刻集体重新输一次授权码轻，而且
// 12 小时之内旧令牌就全过期了，这个歧义跟着一起消失。
func legacyAllMailboxes(v string) bool { return v == "1" }

// Check reports whether this token is currently good for this person, and
// extends it while it is being used. The token is bound to the identity in
// the key, so a token lifted from one person's session says nothing about
// anybody else's mailbox.
//
// 用着就续期，和 ERP 登录会话同一个规矩（renewIfHalfSpent）。从前这里只问
// 「还在吗」，从不续期，于是有效期是从验证那一刻起的一段固定时长——不管
// 当天用得多勤，到点必掉，天天如此。会话会滑而邮箱不滑，是两套凭证的不
// 一致，不是安全设计。
//
// 续期的门槛也照会话来：用掉一半才续，不是每个请求都写一次 Redis。邮件
// 页面每次翻页都会打这里，逐次续期就是把一次读变成一次写。
// 第二个返回值是这把令牌开的是哪个信箱，accountAll 表示不限。
func (u *UnlockStore) Check(ctx context.Context, tenantID, employeeID int64, token string) (int64, bool) {
	if token == "" {
		return 0, false
	}
	key := u.key(tenantID, employeeID, token)
	// 一次拿到值和剩余时间。分两次读会有一个窗口：读完值、还没读 TTL，
	// 键就过期了，于是一把已经死掉的令牌被当成活的。
	v, err := u.rdb.Get(ctx, key).Result()
	if err != nil {
		return 0, false
	}
	acct := accountAll
	if !legacyAllMailboxes(v) {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, false
		}
		acct = n
	}
	left, err := u.rdb.TTL(ctx, key).Result()
	if err != nil {
		return 0, false
	}
	// 还剩一半以上：什么都不做，这是绝大多数请求走的路。
	if left > 0 && left >= u.ttl/2 {
		return acct, true
	}
	// 到这里有三种情况：将要过期、键不存在、键没有到期时间。TTL 用负数
	// 哨兵表示后两种，而那个负数的单位随客户端实现而变——照着哨兵的数值
	// 判断，是把正确性押在库的内部约定上。
	//
	// EXPIRE 自己就能把话说清：键不存在时它不设置任何东西并返回 false。
	// 所以「续期」和「这把钥匙还在不在」是同一个答案。
	ok, err := u.rdb.Expire(ctx, key, u.ttl).Result()
	if err != nil || !ok {
		return 0, false
	}
	return acct, true
}

// Revoke kills one token now rather than waiting out its TTL. Signing out
// of the mailbox must mean the token is dead server-side — deleting it from
// the browser alone would leave a copied token alive.
func (u *UnlockStore) Revoke(ctx context.Context, tenantID, employeeID int64, token string) {
	if token == "" {
		return
	}
	_ = u.rdb.Del(ctx, u.key(tenantID, employeeID, token)).Err()
}

const mailUnlockHeader = "X-Mail-Unlock"

// writeUnlockJSON wraps a plain value in the standard envelope. The proto
// writer cannot help here because these responses have no proto message.
func writeUnlockJSON(w http.ResponseWriter, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "encode", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(envelope{Success: true, Data: data})
}

// requireMailUnlock guards the routes that expose mail content.
//
// Fails closed: no store configured means the mailbox stays locked, because
// a gate that silently opens when its backing store is missing is not a gate.
func (s *Server) requireMailUnlock(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		op, _ := grpcx.OperatorFromContext(r.Context())
		if s.Unlock == nil {
			s.writeError(w, http.StatusForbidden, "MAIL_LOCKED", "请先验证邮箱授权码")
			return
		}
		acct, ok := s.Unlock.Check(r.Context(), op.TenantID, op.EmployeeID, r.Header.Get(mailUnlockHeader))
		if !ok {
			s.writeError(w, http.StatusForbidden, "MAIL_LOCKED", "请先验证邮箱授权码")
			return
		}
		// 令牌开的是哪个箱，放进上下文。读邮件那几个接口用它，而不是用
		// 请求里的 accountId 参数——**那个参数是调用方说的，令牌不是**。
		// 退出了 A 之后，浏览器手上就没有 A 的令牌了；要是还认参数，
		// 拿 B 的令牌配一个 accountId=A 照样读得到，退出就白退了。
		next.ServeHTTP(w, r.WithContext(withUnlockedAccount(r.Context(), acct)))
	})
}

// unlockedAccountKey 是上下文里那把令牌对应的信箱。
type unlockedAccountKey struct{}

func withUnlockedAccount(ctx context.Context, accountID int64) context.Context {
	return context.WithValue(ctx, unlockedAccountKey{}, accountID)
}

// unlockedAccount 取出这次请求解开的是哪个信箱。
//
// 返回 accountAll（0）表示不限——旧令牌，或者一个箱都没绑的人。调用方拿到
// 0 时照旧行为走（默认箱 / 全部），这样换版本那一刻不会有人被挡在外面。
func unlockedAccount(ctx context.Context) int64 {
	if v, ok := ctx.Value(unlockedAccountKey{}).(int64); ok {
		return v
	}
	return accountAll
}

// verifyMailbox is the unlock itself: the typed credentials go to the
// notification service, which logs in to the mail host with them. Only a live
// login mints a token — there is no code path that grants one from stored
// state, except the explicit "nothing to protect" answer for people with no
// mailbox bound. With an email in the body this is also the binding: the
// service stores the pair only after the login succeeded.
func (s *Server) verifyMailbox(w http.ResponseWriter, r *http.Request) {
	// **地址现在是请求体里的字段。这是一次有意的放宽，不是回退。**
	//
	// 从前这里没有 email 字段，注释写着 "must never become one again"，还有
	// 一条读源码的测试守着（throttle_test.go）。那条约束防的是「以
	// alice@thecompany.com 登录，却绑一个私人信箱」——下游全都不一致：ERP
	// 说是这个人发的，客户看到的是另一个地址。
	//
	// 业务口径改了：一个人可以绑多个信箱，而且**不必是公司域名的**
	// （ERP 账号是 263 的人，邮箱这边可以只绑 Gmail）。老约束和这个需求
	// 直接冲突，所以它退役，同一批换上这几条：
	//
	//   - 归属：employeeID 永远来自登录令牌，请求体里没有「绑给谁」这一项，
	//     所以只能绑到自己名下
	//   - 唯一：一个地址只能属于一个人（UNIQUE (tenant_id, email)）
	//   - 活体：必须先真的登录成功才写库，错的授权码覆盖不了能用的凭据
	//   - 主机：服务器地址由**服务端**按服务商代号查表，不收调用方给的主机名
	//   - 限流：见下面 who 那一行——现在按「人 + 目标地址」计费
	//   - 留痕：每一次绑定成功和失败都写 mail_binding_log（00045）
	//
	// 换句话说：原来靠「一个字段不存在」保证的事，现在靠一张表和四道检查
	// 保证，而且比原来问得更细——原来答不出「这个地址是谁绑的」，因为答案
	// 恒等于「他自己那个」。
	var body struct {
		Secret       string `json:"secret"`
		Email        string `json:"email"`
		Provider     string `json:"provider"`
		SMTPHost     string `json:"smtpHost"`
		SMTPPort     int32  `json:"smtpPort"`
		SMTPSecurity string `json:"smtpSecurity"`
		IMAPHost     string `json:"imapHost"`
		IMAPPort     int32  `json:"imapPort"`
		IMAPSecurity string `json:"imapSecurity"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "GATEWAY_BAD_JSON", "请求体不是合法的 JSON")
		return
	}
	op, _ := grpcx.OperatorFromContext(r.Context())
	email := strings.ToLower(strings.TrimSpace(body.Email))
	// 没给地址就是「复验已经绑好的那个」——Google 那扇门没有码可输。
	// 它不再默认成登录地址：那个默认正是老约束的实现方式，而登录地址现在
	// 可能一个信箱都不对应。
	if email == "" && strings.TrimSpace(body.Secret) != "" {
		s.writeError(w, http.StatusBadRequest, "MAIL_ADDRESS_REQUIRED", "请输入邮箱地址")
		return
	}

	// **两个预算，都要过。**
	//
	// 一开始只写了「人 + 地址」那一个，那是错的：地址成了可变字段之后，
	// 按人+地址计费等于把「一个人五次」稀释成「一个人每个地址五次」——
	// 换一串地址就换一份预算，比原来更弱。
	//
	// 这个预算保护的是**我们服务器的 IP 在邮件服务商那里的信誉**（每一次
	// 尝试都是一次真的登录，从我们的机器打出去）。那个资源是按人算的，
	// 换不换地址都一样花。所以按人那一份必须留着。
	//
	// 地址那一份是**额外**收紧：对着同一个信箱猛试，五次就该停，不该
	// 借着"我还有别的地址没试"继续。
	perPerson := fmt.Sprintf("t%d.e%d", op.TenantID, op.EmployeeID)
	perTarget := fmt.Sprintf("t%d.e%d.%s", op.TenantID, op.EmployeeID, email)
	for _, who := range []string{perPerson, perTarget} {
		if wait, blocked := s.Throttle.Blocked(r.Context(), throttleMailVerify, who); blocked {
			s.writeTooManyAttempts(w, wait)
			return
		}
	}

	resp, err := s.Emails.VerifyMailAccess(r.Context(), &mailv1.VerifyMailAccessRequest{
		Secret: body.Secret, Email: email, Provider: body.Provider,
		SmtpHost: body.SMTPHost, SmtpPort: body.SMTPPort, SmtpSecurity: body.SMTPSecurity,
		ImapHost: body.IMAPHost, ImapPort: body.IMAPPort, ImapSecurity: body.IMAPSecurity,
	})
	if err != nil {
		// Not charged. This is the mail service or the network failing, not
		// the caller guessing — and nothing was spent against the mail host,
		// which is the resource this budget protects.
		s.writeGRPCError(w, err)
		return
	}
	if !resp.GetOk() {
		// Charged only when the mail host is what refused, because that is the
		// only failure that spent anything: one bad login against Gmail or 263
		// on this caller's behalf. Everything the service decides on its own —
		// no host configured, an undecryptable stored code, a missing address —
		// never left our network, and billing it to the person meant an
		// internal fault answered their next five attempts with 429 instead of
		// the real reason.
		if resp.GetHostRejected() {
			// 两份都扣。只扣一份的话，没扣的那份就是免费的那条路。
			// 用完的那份里等得最久的决定 Retry-After——报短了，人照着重试
			// 还是 429，那个数字就成了假消息。
			var longest time.Duration
			for _, who := range []string{perPerson, perTarget} {
				if wait, spent := s.Throttle.Failed(r.Context(), throttleMailVerify, who); spent && wait > longest {
					longest = wait
				}
			}
			if longest > 0 {
				s.writeTooManyAttempts(w, longest)
				return
			}
		}
		// The mail host's own words: "wrong code" from Gmail beats any
		// paraphrase we could write.
		s.writeError(w, http.StatusForbidden, "MAIL_VERIFY_FAILED", resp.GetDetail())
		return
	}
	// 成功才清零，而且只清**这次真的验过**的那两个键。
	//
	// 注意这里已经在 resp.Ok 之后：空授权码那条路（「复验已绑的」）也会走到
	// 这儿。它对应的是一次真实的 OAuth 复验或者"一个信箱都没绑"的放行，
	// 两者都不是失败，所以清零是对的——而拿别人的地址空手来试的那条路，
	// 现在在服务层就被拒了，根本到不了这里。
	for _, who := range []string{perPerson, perTarget} {
		s.Throttle.Passed(r.Context(), throttleMailVerify, who)
	}
	// **一次验证，这个人名下每个箱各发一把。**
	//
	// 这是「切过去不用重新输密码」和「退出一个一个退」两条要求的交汇处：
	// 全发下来，切换就只是换一把令牌；一把一把发，退出才撤得掉一个而不
	// 动别的。
	//
	// 有意为之的一点：拿**任何一个**箱的授权码验一次，全部箱都开了。
	// 也就是说用私人邮箱的密码解锁，同时解开了公司箱的邮件。这条在
	// 多信箱这套东西定方案时就定了——安全边界是「人」，不是「箱」。
	boxes, err := s.Emails.ListMyMailboxes(r.Context(), &mailv1.ListMyMailboxesRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	type minted struct {
		AccountID int64  `json:"accountId"`
		Email     string `json:"email"`
		Token     string `json:"token"`
	}
	out := make([]minted, 0, len(boxes.GetAccounts()))
	expires := 0
	for _, b := range boxes.GetAccounts() {
		tok, exp, err := s.Unlock.Grant(r.Context(), op.TenantID, op.EmployeeID, b.GetId())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "MAIL_UNLOCK_STORE", "无法保存验证状态，请重试")
			return
		}
		out = append(out, minted{AccountID: b.GetId(), Email: b.GetEmail(), Token: tok})
		expires = exp
	}
	// 一个箱都没绑的人也要拿到一把——活动、草稿那几个不碰邮件内容的页面
	// 要进得去。不限信箱，因为没有信箱可限。
	token := ""
	if len(out) == 0 {
		tok, exp, err := s.Unlock.Grant(r.Context(), op.TenantID, op.EmployeeID, accountAll)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "MAIL_UNLOCK_STORE", "无法保存验证状态，请重试")
			return
		}
		token, expires = tok, exp
	} else {
		// token 这个字段给还没认识 tokens 的旧前端用：给它刚验过的那个箱
		// 那一把，实在对不上就给第一把。部署顺序是后端先发前端后发，
		// 这几分钟里旧前端不能被锁在外面。
		token = out[0].Token
		for _, m := range out {
			if m.AccountID == resp.GetAccountId() {
				token = m.Token
				break
			}
		}
	}
	writeUnlockJSON(w, map[string]any{
		"token":     token,
		"tokens":    out,
		"expiresIn": expires,
		"detail":    resp.GetDetail(),
		// 真正绑上的是哪一个。地址由调用方给之后，请求里写的和落库的可能
		// 不同（大小写、首尾空格），界面要说得出「你绑好了哪一个」。
		"accountId": resp.GetAccountId(),
		"email":     resp.GetEmail(),
	})
}

// lockMailbox 是退出：**只退当前这一个信箱**。
//
// 请求头里那把令牌对应哪个箱，就撤哪个箱。别的箱的令牌是各自独立的键，
// 一个都不动——所以退出 263 之后，Gmail 那边照样开着。
//
// 从前一个人只有一把令牌，所以「退出」只能是全退。
func (s *Server) lockMailbox(w http.ResponseWriter, r *http.Request) {
	op, _ := grpcx.OperatorFromContext(r.Context())
	if s.Unlock != nil {
		s.Unlock.Revoke(r.Context(), op.TenantID, op.EmployeeID, r.Header.Get(mailUnlockHeader))
	}
	writeUnlockJSON(w, map[string]any{"locked": true})
}

// lockAllMailboxes 是「全部退出」：这个人所有箱的令牌一起撤。
//
// 共用电脑走人时用的那一个。请求体里带着浏览器手上的全部令牌——服务端不
// 保存「这个人有哪些令牌」的索引（键里含令牌本身，没法反查），所以只能
// 由持有者报上来。报漏了的那把仍然会自己到期。
func (s *Server) lockAllMailboxes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Tokens []string `json:"tokens"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "GATEWAY_BAD_JSON", "请求体不是合法的 JSON")
		return
	}
	op, _ := grpcx.OperatorFromContext(r.Context())
	if s.Unlock != nil {
		// 请求头里那把也算上：旧前端只有它，不会往请求体里放东西。
		for _, tok := range append(body.Tokens, r.Header.Get(mailUnlockHeader)) {
			s.Unlock.Revoke(r.Context(), op.TenantID, op.EmployeeID, tok)
		}
	}
	writeUnlockJSON(w, map[string]any{"locked": true})
}

// mailLockStatus lets a page ask before rendering, so the gate appears
// immediately rather than as a burst of failed requests.
func (s *Server) mailLockStatus(w http.ResponseWriter, r *http.Request) {
	op, _ := grpcx.OperatorFromContext(r.Context())
	unlocked, acct := false, accountAll
	if s.Unlock != nil {
		acct, unlocked = s.Unlock.Check(r.Context(), op.TenantID, op.EmployeeID,
			r.Header.Get(mailUnlockHeader))
	}
	// 一并回这把令牌开的是哪个箱：页面渲染前就知道自己站在哪个箱上，
	// 不用等列表回来才知道。
	writeUnlockJSON(w, map[string]any{"unlocked": unlocked, "accountId": acct})
}
