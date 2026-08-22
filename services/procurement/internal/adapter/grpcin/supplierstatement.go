package grpcin

import (
	"context"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
)

// The reconciliation view rides on the order handler like invoices and
// payments do: it reads the same book, behind the same boundary.

func (h *OrderHandler) ListSupplierStatements(ctx context.Context, req *prv1.ListSupplierStatementsRequest) (*prv1.ListSupplierStatementsResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	items, err := h.svc.ListSupplierStatements(ctx, grpcx.TenantID(ctx),
		req.GetKeyword(), app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.SupplierStatement, 0, len(items))
	for _, v := range items {
		out = append(out, statementProto(v))
	}
	return &prv1.ListSupplierStatementsResponse{Items: out}, nil
}

func (h *OrderHandler) GetSupplierStatement(ctx context.Context, req *prv1.GetSupplierStatementRequest) (*prv1.GetSupplierStatementResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	summary, lines, err := h.svc.GetSupplierStatement(ctx, grpcx.TenantID(ctx),
		req.GetSupplierId(), req.GetCurrency(), app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	outLines := make([]*prv1.StatementLine, 0, len(lines))
	for _, l := range lines {
		outLines = append(outLines, &prv1.StatementLine{
			At: l.At, Type: l.Type, Ref: l.Ref, Against: l.Against,
			Amount: l.Amount, Balance: l.Balance, Note: l.Note,
		})
	}
	return &prv1.GetSupplierStatementResponse{Summary: statementProto(summary), Lines: outLines}, nil
}

func statementProto(v app.SupplierStatement) *prv1.SupplierStatement {
	return &prv1.SupplierStatement{
		SupplierId: v.SupplierID, SupplierName: v.SupplierName, Currency: v.Currency,
		OrderedAmount: v.OrderedAmount, ReceivedAmount: v.ReceivedAmount,
		ExceptionAmount: v.ExceptionAmount, InvoicedAmount: v.InvoicedAmount,
		PaidAmount: v.PaidAmount, AdvanceAmount: v.AdvanceAmount,
		UnallocatedAmount: v.UnallocatedAmount, Balance: v.Balance,
		OverdueCount: v.OverdueCount, OverdueAmount: v.OverdueAmount,
		BaseCurrency: v.BaseCurrency, InvoicedBase: v.InvoicedBase,
		PaidBase: v.PaidBase, FxGainLoss: v.FxGainLoss,
	}
}
