package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/sgao19/erp-go/pkg/apierr"
)

type outboundContractStub struct {
	err     error
	checked []int64
}

func (g *outboundContractStub) Check(_ context.Context, id int64) error {
	g.checked = append(g.checked, id)
	return g.err
}

func TestOutboundRechecksContractBeforeStockAndQualityChanges(t *testing.T) {
	f := newOutboundQualityFixture(t, "PURCHASE", false)
	quality := &deliveryQualityStub{}
	f.svc.UseDeliveryQualityGuard(quality)
	before := f.snapshot(t)
	for _, state := range []string{"PAUSED", "TERMINATING", "TERMINATED", "COMPLETED", "CANCELLED", "DELETED", "DRAFT", "PENDING_SIGN", "UNRELEASED"} {
		t.Run(state, func(t *testing.T) {
			guard := &outboundContractStub{err: apierr.Conflict("CONTRACT_EXECUTION_BLOCKED", state)}
			f.svc.UseContractGuard(guard)
			if err := f.svc.ConfirmOutbound(f.ctx, f.tenant, f.outbound, Operator{ID: 1}); apierr.CodeFromError(err) != "CONTRACT_EXECUTION_BLOCKED" {
				t.Fatalf("blocked contract shipped: %v", err)
			}
			if !reflect.DeepEqual(guard.checked, []int64{71}) {
				t.Fatalf("did not check the persisted contract: %v", guard.checked)
			}
			if after := f.snapshot(t); after != before {
				t.Fatalf("blocked contract changed stock/reservations/outbox: %+v -> %+v", before, after)
			}
		})
	}
	for _, guard := range []ContractGuard{nil, &outboundContractStub{err: errors.New("export unavailable")}} {
		f.svc.UseContractGuard(guard)
		if err := f.svc.ConfirmOutbound(f.ctx, f.tenant, f.outbound, Operator{ID: 1}); err == nil {
			t.Fatal("unverifiable contract shipped")
		}
		if after := f.snapshot(t); after != before {
			t.Fatalf("unavailable guard changed stock: %+v -> %+v", before, after)
		}
	}
	if len(quality.checked) != 0 {
		t.Fatalf("contract refusal should precede quality requests: %v", quality.checked)
	}
	// The original picking list is still usable after the contract is resumed.
	guard := &outboundContractStub{}
	f.svc.UseContractGuard(guard)
	if err := f.svc.ConfirmOutbound(f.ctx, f.tenant, f.outbound, Operator{ID: 1}); err != nil {
		t.Fatalf("resumed contract blocked: %v", err)
	}
	after := f.snapshot(t)
	if after.status != "CONFIRMED" || after.onHand != "7.0000" || after.ledgerCount != 1 || after.eventCount != 1 {
		t.Fatalf("resumed contract did not ship exactly once: %+v", after)
	}
	if err := f.svc.ConfirmOutbound(f.ctx, f.tenant, f.outbound, Operator{ID: 1}); err == nil {
		t.Fatal("duplicate confirmation accepted")
	}
	if again := f.snapshot(t); again != after {
		t.Fatalf("duplicate confirmation changed stock: %+v -> %+v", after, again)
	}
}

func TestPausedContractStillAllowsCancellingAnUnshippedPickingList(t *testing.T) {
	f := newOutboundQualityFixture(t, "PURCHASE", false)
	guard := &outboundContractStub{err: apierr.Conflict("CONTRACT_EXECUTION_BLOCKED", "合同已暂停")}
	f.svc.UseContractGuard(guard)
	if err := f.svc.CancelOutbound(f.ctx, f.tenant, f.outbound, "合同暂停，取消未出库单", Operator{ID: 1}); err != nil {
		t.Fatal(err)
	}
	if len(guard.checked) != 0 {
		t.Fatalf("cancellation was treated as new delivery: %v", guard.checked)
	}
	after := f.snapshot(t)
	if after.status != "CANCELLED" || after.onHand != "10.0000" || after.locked != "0.0000" || after.shipped != "0.0000" || after.ledgerCount != 0 || after.eventCount != 0 {
		t.Fatalf("cancellation did not preserve goods: %+v", after)
	}
}
