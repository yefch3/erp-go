// Package pgdb owns PostgreSQL connection setup and the one transaction
// helper every use case goes through. Use-case code never calls Begin or
// Commit directly: the transaction boundary lives in InTx, so it cannot be
// forgotten or half-closed on an error path.
package pgdb

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultMaxConns is the pool ceiling when the DSN does not name one.
//
// pgxpool's own default is max(4, NumCPU), which on the two-core hosts this
// deploys to is four. Four is too few for a service that answers requests and
// runs background workers out of the same pool: the mail service polls
// mailboxes on eight workers while employees are reading their inbox, and at
// four connections the two starve each other - the symptom is not an error
// but page loads that stall behind a sync.
//
// Eight is chosen against the *server's* budget, not this process's appetite.
// Every service shares one Postgres, whose max_connections is 100, and there
// are 11 services: 11 x 8 = 88 leaves room for psql and pg_dump. Raising this
// number means raising max_connections with it, or the eleventh service to
// need its ninth connection is the one that fails.
const DefaultMaxConns = 8

// New opens a pool and verifies connectivity before returning it.
func New(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("pgdb: parse dsn: %w", err)
	}
	// Only when the DSN is silent: a deployment that has measured its own
	// service knows better than this default and must be allowed to say so.
	if !strings.Contains(dsn, "pool_max_conns") {
		cfg.MaxConns = DefaultMaxConns
	}
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("pgdb: create pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pgdb: ping: %w", err)
	}
	return pool, nil
}

// InTx runs fn inside a transaction: commit on nil, rollback on error or
// panic. The panic is re-raised after rollback so recovery interceptors
// still see it.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(ctx context.Context, tx pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("pgdb: begin: %w", err)
	}
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback(ctx)
			panic(r)
		}
	}()
	if err := fn(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("pgdb: commit: %w", err)
	}
	return nil
}
