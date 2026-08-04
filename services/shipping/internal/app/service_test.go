package app

import (
	"context"
	"errors"
	"testing"
)

type pingerStub struct{ err error }

func (p pingerStub) Ping(context.Context) error { return p.err }

func TestModuleStatus(t *testing.T) {
	if err := New(pingerStub{}).ModuleStatus(context.Background()); err != nil {
		t.Fatalf("healthy database reported an error: %v", err)
	}
	want := errors.New("database unavailable")
	if err := New(pingerStub{err: want}).ModuleStatus(context.Background()); !errors.Is(err, want) {
		t.Fatalf("ModuleStatus error = %v, want %v", err, want)
	}
}
