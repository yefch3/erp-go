package app

import (
	"context"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
)

type ContractGuard interface {
	Check(context.Context, int64) error
}

func (s *Service) UseContractGuard(g ContractGuard) { s.contractGuard = g }

// Check the current contract at confirmation, rather than trusting the state
// when a picking list was created or waiting for an asynchronous control event.
func (s *Service) checkOutboundContract(ctx context.Context, contractID int64) error {
	if contractID <= 0 {
		return apierr.Conflict("IV_CONTRACT_SOURCE_REQUIRED", "出库单缺少有效合同关联，不能确认出库")
	}
	if s.contractGuard == nil {
		return apierr.Conflict("IV_CONTRACT_EXECUTION_UNAVAILABLE", "合同执行状态核验暂不可用，不能确认出库，请稍后重试")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.contractGuard.Check(checkCtx, contractID)
}
