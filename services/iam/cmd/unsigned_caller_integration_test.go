package main

import (
	"context"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
)

// Walks up to a running iam the way somebody already inside the network
// would: claiming to be employee 1 of tenant 1, with no signature and no
// password. Until the signing change this returned the whole employee list.
//
// Run against a live stack:
//
//	IAM_ADDR=127.0.0.1:9001 go test ./cmd/ -run TestAnUnsignedCaller
//
// The demand is specifically Unauthenticated. An earlier version of this test
// accepted any error, which meant it passed when nothing was listening at all
// — a test that is green because the service is down is worse than no test,
// because it is the one that would have caught this being switched off.
func TestAnUnsignedCallerCannotReadTheEmployeeList(t *testing.T) {
	addr := envOr("IAM_ADDR", "127.0.0.1:9001")
	cc, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Skipf("cannot build a client for %s: %v", addr, err)
	}
	defer cc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	// Exactly what the gateway used to send, minus the signature.
	ctx = metadata.AppendToOutgoingContext(ctx,
		"x-tenant-id", "1",
		"x-employee-id", "1",
		"x-employee-name", "admin",
	)

	resp, err := iamv1.NewDirectoryServiceClient(cc).ListEmployees(ctx,
		&iamv1.ListEmployeesRequest{Page: &commonv1.PageRequest{Page: 1, PageSize: 5}})
	if err == nil {
		t.Fatalf("ACCEPTED: read %d employees with no password and no signature",
			len(resp.GetEmployees()))
	}
	switch status.Code(err) {
	case codes.Unauthenticated:
		// What we came for.
	case codes.Unavailable, codes.DeadlineExceeded:
		t.Skipf("no iam listening on %s, so this proves nothing: %v", addr, err)
	default:
		t.Fatalf("refused, but for the wrong reason: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
