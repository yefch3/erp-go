package grpcout

import (
	"context"
	"strings"
	"time"

	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"google.golang.org/grpc"
)

type ContractGuard struct{ client exv1.ContractServiceClient }

func NewContractGuard(conn *grpc.ClientConn) *ContractGuard {
	return &ContractGuard{client: exv1.NewContractServiceClient(conn)}
}

func (g *ContractGuard) Check(ctx context.Context, id int64) error {
	if id <= 0 {
		return apierr.Conflict("IV_CONTRACT_SOURCE_REQUIRED", "出库单缺少有效合同关联，不能确认出库")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := g.client.CheckContractExecution(checkCtx, &exv1.CheckContractExecutionRequest{Id: id})
	if err != nil {
		return apierr.Conflict("IV_CONTRACT_EXECUTION_UNAVAILABLE", "合同执行状态核验暂不可用，不能确认出库，请稍后重试").Wrap(err)
	}
	if !result.GetAllowed() {
		reason := strings.TrimSpace(result.GetReason())
		if reason == "" {
			reason = "外销合同尚未放行、已暂停或已关闭，不能确认出库"
		}
		return apierr.Conflict("CONTRACT_EXECUTION_BLOCKED", reason)
	}
	return nil
}
