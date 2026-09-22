package grpcout

import (
	"context"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"google.golang.org/grpc"
)

type ContractGuard struct{ client exv1.ContractServiceClient }

func NewContractGuard(conn *grpc.ClientConn) *ContractGuard {
	return &ContractGuard{client: exv1.NewContractServiceClient(conn)}
}
func (g *ContractGuard) Check(ctx context.Context, id int64) error {
	if id == 0 {
		return nil
	}
	r, err := g.client.CheckContractExecution(ctx, &exv1.CheckContractExecutionRequest{Id: id})
	if err != nil {
		return err
	}
	if !r.GetAllowed() {
		return apierr.Conflict("CONTRACT_EXECUTION_BLOCKED", r.GetReason())
	}
	return nil
}
