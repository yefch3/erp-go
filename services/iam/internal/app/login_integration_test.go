package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// What only a database can prove about login: that two companies can both run
// on the same public mail service, and that an address names exactly one
// account system-wide.
//
// Run with: IAM_TEST_DSN=postgres://erp_iam:...@127.0.0.1:5433/erp_iam

func loginTestPool(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	dsn := os.Getenv("IAM_TEST_DSN")
	if dsn == "" {
		t.Skip("set IAM_TEST_DSN to a migrated PostgreSQL database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, ctx
}

// seedCompany creates a tenant with one department, one employee holding the
// given address, and a login for them. Everything is removed afterwards.
func seedCompany(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	company, email, password string) int64 {
	t.Helper()

	var tenantID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO tenants (name, status) VALUES ($1, 'ACTIVE') RETURNING id`,
		company).Scan(&tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	t.Cleanup(func() {
		// Children first; the foreign keys are real.
		for _, stmt := range []string{
			`DELETE FROM users WHERE tenant_id = $1`,
			`DELETE FROM employees WHERE tenant_id = $1`,
			`DELETE FROM departments WHERE tenant_id = $1`,
			`DELETE FROM tenant_domains WHERE tenant_id = $1`,
			`DELETE FROM tenants WHERE id = $1`,
		} {
			if _, err := pool.Exec(ctx, stmt, tenantID); err != nil {
				t.Errorf("cleanup %q: %v", stmt, err)
			}
		}
	})

	var deptID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO departments (tenant_id, code, name, path, level)
		 VALUES ($1, 'HQ', '总部', '/', 1) RETURNING id`,
		tenantID).Scan(&deptID); err != nil {
		t.Fatalf("seed department: %v", err)
	}

	var employeeID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO employees (tenant_id, code, name, department_id, email,
		                        status, email_verified_at)
		 VALUES ($1, 'E001', $2, $3, $4, 'ACTIVE', now()) RETURNING id`,
		tenantID, company+"的人", deptID, email).Scan(&employeeID); err != nil {
		t.Fatalf("seed employee: %v", err)
	}

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (tenant_id, employee_id, username, password_hash, status)
		 VALUES ($1, $2, $3, $4, 'ACTIVE')`,
		tenantID, employeeID, email, hash); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return tenantID
}

func loginService(pool *pgxpool.Pool) *Service {
	return New(pool, "a-secret-long-enough-for-a-test", time.Hour,
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
}

// The bug this replaces: tenant_domains.domain is a PRIMARY KEY, so the first
// company to register gmail.com owned it and every later one was refused —
// not "routed to the wrong company", but unable to exist. Small exporters
// running on QQ mail or 163 hit this immediately.
func TestTwoCompaniesCanBothRunOnTheSameMailService(t *testing.T) {
	pool, ctx := loginTestPool(t)
	// Unique per run so repeated runs do not collide on the global index.
	stamp := time.Now().UnixNano()
	aliceAddr := fmt.Sprintf("alice%d@gmail.com", stamp)
	bobAddr := fmt.Sprintf("bob%d@gmail.com", stamp)

	tenantA := seedCompany(t, ctx, pool, "甲公司", aliceAddr, "correct-horse-battery")
	tenantB := seedCompany(t, ctx, pool, "乙公司", bobAddr, "correct-horse-battery")
	if tenantA == tenantB {
		t.Fatal("the two companies share an id")
	}

	svc := loginService(pool)

	a, err := svc.Login(ctx, aliceAddr, "correct-horse-battery")
	if err != nil {
		t.Fatalf("甲公司 could not sign in: %v", err)
	}
	b, err := svc.Login(ctx, bobAddr, "correct-horse-battery")
	if err != nil {
		t.Fatalf("乙公司 could not sign in: %v", err)
	}

	// Each must land in their own company, not in whichever one happened to
	// register the domain first.
	if got := tenantOf(t, a.Token); got != tenantA {
		t.Errorf("甲公司's token says tenant %d, want %d", got, tenantA)
	}
	if got := tenantOf(t, b.Token); got != tenantB {
		t.Errorf("乙公司's token says tenant %d, want %d", got, tenantB)
	}
}

// One address, one account, system-wide. This is what lets login skip the
// domain: there is never more than one candidate, so there is no "which
// company did you mean?" step to build.
func TestTheSameAddressCannotExistInTwoCompanies(t *testing.T) {
	pool, ctx := loginTestPool(t)
	shared := fmt.Sprintf("shared%d@gmail.com", time.Now().UnixNano())

	seedCompany(t, ctx, pool, "甲公司", shared, "correct-horse-battery")

	// The second company claiming the same address must be refused by the
	// database, not by application code that could be bypassed.
	var tenantID int64
	err := pool.QueryRow(ctx,
		`INSERT INTO tenants (name, status) VALUES ('乙公司', 'ACTIVE') RETURNING id`).Scan(&tenantID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM employees WHERE tenant_id = $1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM departments WHERE tenant_id = $1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, tenantID)
	}()
	var deptID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO departments (tenant_id, code, name, path, level)
		 VALUES ($1, 'HQ', '总部', '/', 1) RETURNING id`, tenantID).Scan(&deptID); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO employees (tenant_id, code, name, department_id, email, status)
		 VALUES ($1, 'E001', '重名', $2, $3, 'ACTIVE')`, tenantID, deptID, shared)
	if err == nil {
		t.Fatal("the same address was accepted in a second company")
	}
	if !strings.Contains(err.Error(), "employees_email_key") {
		t.Fatalf("refused, but not by the uniqueness rule: %v", err)
	}
}

// A company whose account was suspended must be refused — and the check has
// to survive the move, because it used to hang off the domain lookup that no
// longer happens.
func TestASuspendedCompanyCannotSignIn(t *testing.T) {
	pool, ctx := loginTestPool(t)
	addr := fmt.Sprintf("frozen%d@gmail.com", time.Now().UnixNano())
	tenantID := seedCompany(t, ctx, pool, "停用公司", addr, "correct-horse-battery")

	if _, err := pool.Exec(ctx,
		`UPDATE tenants SET status = 'SUSPENDED' WHERE id = $1`, tenantID); err != nil {
		t.Fatal(err)
	}
	svc := loginService(pool)
	if _, err := svc.Login(ctx, addr, "correct-horse-battery"); err == nil {
		t.Fatal("a suspended company signed in")
	}
}

// An address nobody holds answers exactly like a wrong password. Login is a
// page anyone can reach, and addresses are guessable — 名字@公司域名 — so a
// different answer would turn it into a staff directory.
func TestAnUnknownAddressIsRefusedLikeAWrongPassword(t *testing.T) {
	pool, ctx := loginTestPool(t)
	addr := fmt.Sprintf("real%d@gmail.com", time.Now().UnixNano())
	seedCompany(t, ctx, pool, "甲公司", addr, "correct-horse-battery")
	svc := loginService(pool)

	_, unknown := svc.Login(ctx, "nobody-at-all@gmail.com", "correct-horse-battery")
	_, wrongPass := svc.Login(ctx, addr, "not-the-password")
	if unknown == nil || wrongPass == nil {
		t.Fatal("one of these should have failed")
	}
	if unknown.Error() != wrongPass.Error() {
		t.Fatalf("an unknown address says %q but a wrong password says %q — "+
			"the difference is a staff directory", unknown, wrongPass)
	}
	// And a domain nobody has ever registered behaves the same way: the
	// domain is simply not consulted any more.
	if _, err := svc.Login(ctx, "someone@never-heard-of-it.example",
		"correct-horse-battery"); err == nil || err.Error() != unknown.Error() {
		t.Fatalf("an unheard-of domain answered differently: %v", err)
	}
}

// tenantOf reads the tid claim without needing the signing secret: the payload
// is public, which is the point made throughout this design.
func tenantOf(t *testing.T, token string) int64 {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWT: %d segments", len(parts))
	}
	payload := parts[1]
	if pad := len(payload) % 4; pad != 0 {
		payload += strings.Repeat("=", 4-pad)
	}
	raw, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("payload does not decode: %v", err)
	}
	var claims struct {
		TenantID int64 `json:"tid"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("payload is not the shape we sign: %v", err)
	}
	if claims.TenantID == 0 {
		t.Fatal("the token carries no tenant")
	}
	return claims.TenantID
}
