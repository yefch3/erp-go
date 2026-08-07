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
// keyboard now is the mailbox's owner. The proof is deliberately short-lived
// and per-person; it lives in Redis so every gateway replica sees it, and it
// dies on its own rather than needing a logout path.
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

// Check reports whether this token is currently good for this person. The
// token is bound to the identity in the key, so a token lifted from one
// person's session says nothing about anybody else's mailbox.
func (u *UnlockStore) Check(ctx context.Context, tenantID, employeeID int64, token string) bool {
	if token == "" {
		return false
	}
	n, err := u.rdb.Exists(ctx, u.key(tenantID, employeeID, token)).Result()
	return err == nil && n > 0
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
		// A rejection by the mail host is the case this budget exists for: it
		// means our server just spent one bad login against Gmail or 263 on
		// this caller's behalf.
		if wait, spent := s.Throttle.Failed(r.Context(), throttleMailVerify, who); spent {
			s.writeTooManyAttempts(w, wait)
			return
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
