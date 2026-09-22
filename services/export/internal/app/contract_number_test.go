package app

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sgao19/erp-go/pkg/apierr"
)

func TestContractNumberRetriesAreBounded(t *testing.T) {
	numbers := &historyNumberingStub{}
	err := withContractNumber(context.Background(), numbers, func(string) error {
		return &pgconn.PgError{Code: "23505", ConstraintName: "contracts_tenant_id_contract_no_key"}
	})
	var businessErr *apierr.Error
	if numbers.n != 10 || !errors.As(err, &businessErr) || businessErr.Code != "EX_CONTRACT_NUMBER_CONFLICT" {
		t.Fatalf("unexpected retry limit: %d, %v", numbers.n, err)
	}
}

func TestContractNumberDoesNotRetryOtherFailures(t *testing.T) {
	for _, failure := range []error{
		&pgconn.PgError{Code: "23505", ConstraintName: "contracts_external_no_idx"},
		&pgconn.PgError{Code: "23505", ConstraintName: "contracts_pkey"},
		errors.New("connection lost; commit outcome unknown"),
	} {
		numbers := &historyNumberingStub{}
		err := withContractNumber(context.Background(), numbers, func(string) error { return failure })
		if numbers.n != 1 || err != failure {
			t.Fatalf("unexpected retry: %d, %v", numbers.n, err)
		}
	}
}
