// Package app contains shipping use cases. D0 exposes readiness only; schedule
// commands and queries deliberately begin in D1.
package app

import "context"

const SchemaVersion int32 = 1

type databasePinger interface {
	Ping(context.Context) error
}

type Service struct {
	db databasePinger
}

func New(db databasePinger) *Service { return &Service{db: db} }

func (s *Service) ModuleStatus(ctx context.Context) error {
	return s.db.Ping(ctx)
}
