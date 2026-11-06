package grpcout

import (
	"context"
	"errors"
	"testing"
	"time"

	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"google.golang.org/grpc"
)

type outboundContractClientStub struct {
	exv1.ContractServiceClient
	result   *exv1.CheckContractExecutionResponse
	err      error
	request  *exv1.CheckContractExecutionRequest
	deadline time.Time
}

func (c *outboundContractClientStub) CheckContractExecution(ctx context.Context, req *exv1.CheckContractExecutionRequest, _ ...grpc.CallOption) (*exv1.CheckContractExecutionResponse, error) {
	c.request = req
	c.deadline, _ = ctx.Deadline()
	return c.result, c.err
}

func TestOutboundContractClientFailsClosedAndBoundsCalls(t *testing.T) {
	for _, test := range []struct {
		name   string
		result *exv1.CheckContractExecutionResponse
		err    error
		code   string
	}{
		{"released", &exv1.CheckContractExecutionResponse{Allowed: true}, nil, ""},
		{"paused", &exv1.CheckContractExecutionResponse{Reason: "合同暂停"}, nil, "CONTRACT_EXECUTION_BLOCKED"},
		{"missing response", nil, nil, "CONTRACT_EXECUTION_BLOCKED"},
		{"rpc failure", nil, errors.New("unavailable"), "IV_CONTRACT_EXECUTION_UNAVAILABLE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &outboundContractClientStub{result: test.result, err: test.err}
			guard := &ContractGuard{client: client}
			before := time.Now()
			err := guard.Check(context.Background(), 71)
			if apierr.CodeFromError(err) != test.code || (test.code == "" && err != nil) {
				t.Fatalf("unexpected release result: %v", err)
			}
			if client.request.GetId() != 71 || client.deadline.IsZero() || client.deadline.Before(before) || client.deadline.After(before.Add(6*time.Second)) {
				t.Fatalf("wrong target or unbounded call: request=%v deadline=%v", client.request, client.deadline)
			}
		})
	}
	client := &outboundContractClientStub{}
	if err := (&ContractGuard{client: client}).Check(context.Background(), 0); apierr.CodeFromError(err) != "IV_CONTRACT_SOURCE_REQUIRED" || client.request != nil {
		t.Fatalf("invalid contract was sent to export: %v", err)
	}
}
