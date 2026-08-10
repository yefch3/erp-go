// Package authtoken holds the JWT format shared by the issuer (iam) and
// every validator (gateway). It lives in pkg because two services must
// agree on it; the signing secret still comes from each service's config.
package authtoken

import (
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the token payload: which tenant, which employee.
type Claims struct {
	TenantID     int64  `json:"tid"`
	EmployeeName string `json:"name"`
	// When this person last typed their password, carried unchanged through
	// every renewal.
	//
	// Not the same thing as IssuedAt, and the difference is the whole reason
	// it exists. Renewal mints a token with a fresh IssuedAt; if revocation
	// compared against that, a session could renew its way past the cutoff
	// and become permanently immune — the 10-second snapshot window in the
	// gateway is long enough to slip through once, and once is enough. This
	// value never moves, so no number of renewals escapes it.
	//
	// Zero on tokens minted before this field existed; callers fall back to
	// IssuedAt, which for those tokens is the same moment.
	AuthTime int64 `json:"auth_time,omitempty"`
	// The company address this person logged in with.
	//
	// Carried so the mailbox they bind can be taken from their identity
	// rather than from a form field. A field would be a field somebody can
	// change, and the one thing the gate must not allow is binding a mailbox
	// that is not the one they signed in as.
	Email string `json:"eml"`
	jwt.RegisteredClaims
}

// EmployeeID parses the subject claim.
func (c *Claims) EmployeeID() int64 {
	id, _ := strconv.ParseInt(c.Subject, 10, 64)
	return id
}

func Issue(secret string, ttl time.Duration, tenantID, employeeID int64, name, email string) (string, error) {
	now := time.Now()
	claims := Claims{
		TenantID:     tenantID,
		EmployeeName: name,
		Email:        email,
		AuthTime:     now.Unix(),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(employeeID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			Issuer:    "erp-iam",
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// Renew mints a token carrying the same identity with the clock restarted.
//
// Everything the caller could act on is copied from the old token rather than
// looked up again: the point of renewal is that it costs nothing, and a
// directory round trip on the way through would put a database call back into
// the one path built to avoid them. The consequence is that a rename or a
// change of address only shows up after the next real sign-in, which is the
// same as today — those values were already frozen at login.
//
// AuthTime is carried over untouched. That is the property the whole design
// rests on; see the field's comment.
func Renew(secret string, ttl time.Duration, c *Claims) (string, error) {
	now := time.Now()
	authTime := c.AuthTime
	if authTime == 0 {
		// Minted before the field existed. Its IssuedAt *is* the sign-in
		// moment, because nothing renewed back then.
		if c.IssuedAt != nil {
			authTime = c.IssuedAt.Time.Unix()
		} else {
			// No usable origin. Stamping "now" would hand out a token that
			// outruns any revocation, so refuse and let the person sign in.
			return "", fmt.Errorf("token has no issue time to carry forward")
		}
	}
	claims := Claims{
		TenantID:     c.TenantID,
		EmployeeName: c.EmployeeName,
		Email:        c.Email,
		AuthTime:     authTime,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   c.Subject,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			Issuer:    "erp-iam",
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// SignedInAt is the moment the password was typed: AuthTime when present,
// IssuedAt for tokens that predate it. This is what revocation compares
// against — never IssuedAt directly, which moves on every renewal.
func (c *Claims) SignedInAt() time.Time {
	if c == nil {
		return time.Time{}
	}
	if c.AuthTime > 0 {
		return time.Unix(c.AuthTime, 0)
	}
	if c.IssuedAt != nil {
		return c.IssuedAt.Time
	}
	return time.Time{}
}

// HalfSpent reports whether the token is past the midpoint of its life, which
// is when renewal becomes worthwhile.
//
// Half rather than "nearly expired": renewing at the last moment means a
// session dies whenever the final request happens to fail, and renewing on
// every request means signing a token that is thrown away seconds later.
func (c *Claims) HalfSpent(now time.Time) bool {
	if c == nil || c.IssuedAt == nil || c.ExpiresAt == nil {
		return false
	}
	issued, expires := c.IssuedAt.Time, c.ExpiresAt.Time
	if !expires.After(issued) {
		return false
	}
	return now.After(issued.Add(expires.Sub(issued) / 2))
}

func Parse(secret, token string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}
