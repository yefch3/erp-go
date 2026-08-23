package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
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
// keyboard now is the mailbox's owner. The proof is per-person, lives in
// Redis so every gateway replica sees it, and dies on its own rather than
// needing a logout path.
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

// Grant mints a fresh token for this person. Random rather than derived: a
// derived token could be reconstructed by anything that knows the inputs,
// and the whole point is that only this browser session holds it.
func (u *UnlockStore) Grant(ctx context.Context, tenantID, employeeID int64) (string, int, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", 0, err
	}
	token := hex.EncodeToString(b)
	if err := u.rdb.Set(ctx, u.key(tenantID, employeeID, token), "1", u.ttl).Err(); err != nil {
		return "", 0, err
	}
	return token, int(u.ttl.Seconds()), nil
}

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
func (u *UnlockStore) Check(ctx context.Context, tenantID, employeeID int64, token string) bool {
	if token == "" {
		return false
	}
	key := u.key(tenantID, employeeID, token)
	left, err := u.rdb.TTL(ctx, key).Result()
	if err != nil {
		return false
	}
	// 还剩一半以上：什么都不做，这是绝大多数请求走的路。
	if left > 0 && left >= u.ttl/2 {
		return true
	}
	// 到这里有三种情况：将要过期、键不存在、键没有到期时间。TTL 用负数
	// 哨兵表示后两种，而那个负数的单位随客户端实现而变——照着哨兵的数值
	// 判断，是把正确性押在库的内部约定上。
	//
	// EXPIRE 自己就能把话说清：键不存在时它不设置任何东西并返回 false。
	// 所以「续期」和「这把钥匙还在不在」是同一个答案。
	ok, err := u.rdb.Expire(ctx, key, u.ttl).Result()
	return err == nil && ok
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
		if s.Unlock == nil || !s.Unlock.Check(r.Context(), op.TenantID, op.EmployeeID, r.Header.Get(mailUnlockHeader)) {
			s.writeError(w, http.StatusForbidden, "MAIL_LOCKED", "请先验证邮箱授权码")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// verifyMailbox is the unlock itself: the typed credentials go to the
// notification service, which logs in to the mail host with them. Only a live
// login mints a token — there is no code path that grants one from stored
// state, except the explicit "nothing to protect" answer for people with no
// mailbox bound. With an email in the body this is also the binding: the
// service stores the pair only after the login succeeded.
func (s *Server) verifyMailbox(w http.ResponseWriter, r *http.Request) {
	// Only the secret. The address is not a field here and must never become
	// one again: it comes from the token, which means the mailbox somebody
	// binds is necessarily the one they signed in as.
	//
	// It used to be in the body, and that allowed the thing this whole design
	// exists to prevent — signing in as alice@thecompany.com and binding a
	// personal mailbox. Everything downstream then disagreed: the ERP said one
	// person sent the mail and the customer saw another address, and mail the
	// company does not control started flowing through it. Validating the
	// field would have worked too; removing it is better, because a field that
	// does not exist cannot be got wrong later.
	var body struct {
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "GATEWAY_BAD_JSON", "请求体不是合法的 JSON")
		return
	}
	// Metered by the employee, who is already authenticated here — a better
	// identity than the login route gets, and the right one: the budget being
	// spent is this person's, and the cost of overspending it is that the mail
	// host blocks the address the whole company sends from.
	op, _ := grpcx.OperatorFromContext(r.Context())
	if op.Email == "" {
		// An employee row with no address. They cannot bind anything until
		// somebody gives them one, and saying so is better than letting them
		// type an address that would then be ignored.
		s.writeError(w, http.StatusForbidden, "MAIL_NO_WORK_ADDRESS",
			"你的账号还没有公司邮箱地址，请联系管理员")
		return
	}
	who := fmt.Sprintf("t%d.e%d", op.TenantID, op.EmployeeID)
	if wait, blocked := s.Throttle.Blocked(r.Context(), throttleMailVerify, who); blocked {
		s.writeTooManyAttempts(w, wait)
		return
	}

	resp, err := s.Emails.VerifyMailAccess(r.Context(), &mailv1.VerifyMailAccessRequest{
		Secret: body.Secret, Email: op.Email,
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
			if wait, spent := s.Throttle.Failed(r.Context(), throttleMailVerify, who); spent {
				s.writeTooManyAttempts(w, wait)
				return
			}
		}
		// The mail host's own words: "wrong code" from Gmail beats any
		// paraphrase we could write.
		s.writeError(w, http.StatusForbidden, "MAIL_VERIFY_FAILED", resp.GetDetail())
		return
	}
	s.Throttle.Passed(r.Context(), throttleMailVerify, who)
	token, expires, err := s.Unlock.Grant(r.Context(), op.TenantID, op.EmployeeID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "MAIL_UNLOCK_STORE", "无法保存验证状态，请重试")
		return
	}
	writeUnlockJSON(w, map[string]any{
		"token":     token,
		"expiresIn": expires,
		"detail":    resp.GetDetail(),
	})
}

// lockMailbox is the sign-out: the current token dies server-side and the
// gate reappears on the next visit.
func (s *Server) lockMailbox(w http.ResponseWriter, r *http.Request) {
	op, _ := grpcx.OperatorFromContext(r.Context())
	if s.Unlock != nil {
		s.Unlock.Revoke(r.Context(), op.TenantID, op.EmployeeID, r.Header.Get(mailUnlockHeader))
	}
	writeUnlockJSON(w, map[string]any{"locked": true})
}

// mailLockStatus lets a page ask before rendering, so the gate appears
// immediately rather than as a burst of failed requests.
func (s *Server) mailLockStatus(w http.ResponseWriter, r *http.Request) {
	op, _ := grpcx.OperatorFromContext(r.Context())
	unlocked := s.Unlock != nil &&
		s.Unlock.Check(r.Context(), op.TenantID, op.EmployeeID, r.Header.Get(mailUnlockHeader))
	writeUnlockJSON(w, map[string]any{"unlocked": unlocked})
}
