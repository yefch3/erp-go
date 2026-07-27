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
	jwt.RegisteredClaims
}

// EmployeeID parses the subject claim.
func (c *Claims) EmployeeID() int64 {
	id, _ := strconv.ParseInt(c.Subject, 10, 64)
	return id
}

func Issue(secret string, ttl time.Duration, tenantID, employeeID int64, name string) (string, error) {
	now := time.Now()
	claims := Claims{
		TenantID:     tenantID,
		EmployeeName: name,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(employeeID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			Issuer:    "erp-iam",
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
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
