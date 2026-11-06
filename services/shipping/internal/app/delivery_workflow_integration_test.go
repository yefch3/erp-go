package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/shipping/internal/store"
)

type deliveryContractStub struct {
	mu       sync.Mutex
	err      error
	calls    []int64
	deadline bool
	timeout  time.Duration
}

func (g *deliveryContractStub) Check(ctx context.Context, contract int64) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, contract)
	if deadline, ok := ctx.Deadline(); ok {
		g.deadline, g.timeout = true, time.Until(deadline)
	}
	return g.err
}

func TestFirstDispatchRechecksContractOnEveryMovementEntry(t *testing.T) {
	for _, state := range []string{"PAUSED", "TERMINATING", "TERMINATED", "unavailable", "unconfigured"} {
		for _, entry := range []string{"status", "origin-depart", "origin-times", "transit-approach", "transit-arrive", "transit-depart", "destination-arrive", "destination-times", "derived-times", "derived-remove"} {
			t.Run(state+"/"+entry, func(t *testing.T) {
				ctx, pool, tenant := deliveryTestDB(t)
				svc := New(pool)
				schedule := deliverySchedules(t, ctx, pool, tenant, svc, 1)[0]
				op := Operator{ID: 1, Name: "物流"}
				details, err := svc.GetScheduleDetails(ctx, tenant, schedule.ID)
				if err != nil {
					t.Fatal(err)
				}
				origin, destination := details.Route[0], details.Route[1]
				nodes, version, err := svc.AddRouteNode(ctx, tenant, schedule.ID, RouteNodeInput{NodeType: "TRANSIT", PortName: "Busan", InsertAfterNodeID: origin.ID, RouteVersion: schedule.RouteVersion, Reason: "中转"}, op)
				if err != nil {
					t.Fatal(err)
				}
				transit := nodes[1]
				blocked := apierr.Conflict("CONTRACT_EXECUTION_BLOCKED", state)
				var guardErr error = blocked
				if state == "unavailable" {
					guardErr = errors.New("export unavailable")
				}
				contract := &deliveryContractStub{err: guardErr}
				quality := &deliveryQualityStub{}
				svc.UseContractGuard(contract)
				if state == "unconfigured" {
					svc.UseContractGuard(nil)
				}
				svc.UseDeliveryQualityGuard(quality)
				in := ProgressInput{RouteNodeID: origin.ID, RouteVersion: version, Reason: "首次实际发运", ActualTime: "2026-08-12T10:00:00Z"}
				switch entry {
				case "status":
					_, err = svc.UpdateScheduleStatus(ctx, tenant, schedule.ID, "SAILED", in.Reason, op)
				case "origin-depart":
					in.Action = "DEPART"
				case "origin-times":
					in.Action, in.LatestETDAt, in.ActualDepartureAt = "UPDATE_TIMES", "2026-08-12T00:00:00Z", in.ActualTime
				case "transit-approach", "transit-arrive", "transit-depart":
					in.RouteNodeID = transit.ID
					in.Action = map[string]string{"transit-approach": "APPROACH", "transit-arrive": "ARRIVE", "transit-depart": "DEPART"}[entry]
				case "destination-arrive":
					in.RouteNodeID, in.Action = destination.ID, "ARRIVE"
				case "destination-times":
					in.RouteNodeID, in.Action, in.LatestETAAt, in.ActualArrivalAt = destination.ID, "UPDATE_TIMES", "2026-09-07T00:00:00Z", "2026-09-07T10:00:00Z"
				case "derived-times", "derived-remove":
					// A legacy route can have operational evidence before its
					// schedule status was synchronized. Automatic promotion must
					// recheck the contract too; a plan edit is not an exemption.
					if _, e := pool.Exec(ctx, `UPDATE shipping_route_nodes SET node_status='APPROACHING' WHERE tenant_id=$1 AND id=$2`, tenant, destination.ID); e != nil {
						t.Fatal(e)
					}
					in.Action, in.LatestETDAt = "UPDATE_TIMES", "2026-08-12T00:00:00Z"
					if entry == "derived-remove" {
						_, _, err = svc.RemoveRouteNode(ctx, tenant, schedule.ID, transit.ID, "移除未用港口", version, op)
					}
				}
				if entry != "status" && entry != "derived-remove" {
					_, _, _, err = svc.UpdateProgress(ctx, tenant, schedule.ID, in, op)
				}
				validCheck := len(contract.calls) == 1 && contract.calls[0] == 41 && contract.deadline && contract.timeout > 0 && contract.timeout <= 5*time.Second
				if state == "unconfigured" {
					validCheck = len(contract.calls) == 0 && errorCode(err) == "SHIPPING_CONTRACT_EXECUTION_UNAVAILABLE"
				}
				if err == nil || !validCheck || quality.contractID != 0 {
					t.Fatalf("contract was bypassed: err=%v calls=%v quality=%d", err, contract.calls, quality.contractID)
				}
				got, _, e := svc.GetSchedule(ctx, tenant, schedule.ID)
				if e != nil || got.Status != "PLANNED" || got.Atd.Valid || got.Ata.Valid {
					t.Fatalf("blocked movement changed schedule: %+v %v", got, e)
				}
			})
		}
	}
}

func TestHistoricalDispatchedOriginCorrectionsNeedNoNewRelease(t *testing.T) {
	ctx, pool, tenant := deliveryTestDB(t)
	svc := New(pool)
	schedule := deliverySchedules(t, ctx, pool, tenant, svc, 1)[0]
	// A genuine historical sailing can lack both provenance and actual dates.
	// Its SAILED status is existing dispatch evidence; correcting a date must
	// not create another release or require the old contract/QC to be online.
	if _, err := pool.Exec(ctx, `UPDATE shipping_schedules SET status='SAILED',contract_id=NULL,contract_handoff_id=NULL WHERE tenant_id=$1 AND id=$2`, tenant, schedule.ID); err != nil {
		t.Fatal(err)
	}
	svc.UseContractGuard(nil)
	svc.UseDeliveryQualityGuard(nil)
	details, err := svc.GetScheduleDetails(ctx, tenant, schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	op := Operator{ID: 1, Name: "物流"}
	for _, in := range []ProgressInput{
		{RouteNodeID: details.Route[0].ID, Action: "UPDATE_TIMES", LatestETDAt: "2026-08-12T00:00:00Z", ActualDepartureAt: "2026-08-12T10:00:00Z"},
		{RouteNodeID: details.Route[0].ID, Action: "DEPART", ActualTime: "2026-08-12T12:00:00Z"},
		{RouteNodeID: details.Route[1].ID, Action: "ARRIVE", ActualTime: "2026-09-07T10:00:00Z"},
	} {
		in.RouteVersion, in.Reason = schedule.RouteVersion, "更正既有事实"
		if _, _, _, err := svc.UpdateProgress(ctx, tenant, schedule.ID, in, op); err != nil {
			t.Fatalf("historical %s blocked: %v", in.Action, err)
		}
	}
	if _, err := svc.UpdateScheduleStatus(ctx, tenant, schedule.ID, "COMPLETED", "历史业务结案", op); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchedVoyageCanSettleAfterContractStops(t *testing.T) {
	ctx, pool, tenant := deliveryTestDB(t)
	svc := New(pool)
	schedule := deliverySchedules(t, ctx, pool, tenant, svc, 1)[0]
	op := Operator{ID: 1, Name: "物流"}
	svc.UseDeliveryQualityGuard(&deliveryQualityStub{})
	if _, err := svc.UpdateScheduleStatus(ctx, tenant, schedule.ID, "SAILED", "已发运，无旧ATD", op); err != nil {
		t.Fatal(err)
	}
	contract := &deliveryContractStub{err: apierr.Conflict("CONTRACT_EXECUTION_BLOCKED", "TERMINATED")}
	svc.UseContractGuard(contract)
	svc.UseDeliveryQualityGuard(nil)
	for _, state := range []string{"DELAYED", "SAILED", "IN_TRANSIT", "ARRIVED", "COMPLETED"} {
		if _, err := svc.UpdateScheduleStatus(ctx, tenant, schedule.ID, state, "维护既有发运", op); err != nil {
			t.Fatalf("settlement blocked at %s: %v", state, err)
		}
	}
	if len(contract.calls) != 0 {
		t.Fatalf("settlement was treated as new dispatch: %v", contract.calls)
	}
}

type deliveryWorkflowApprovals struct{}

func (deliveryWorkflowApprovals) Submit(context.Context, ApprovalSubmission) (int64, error) {
	return 771, nil
}

func manualDeliverySchedule(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant int64, svc *Service, linked int64, cargo []ContractShippingCargoItem) store.ShippingSchedule {
	t.Helper()
	if svc.contractGuard == nil {
		svc.UseContractGuard(workflowAllowedContract{})
	}
	op := Operator{ID: 1, Name: "物流"}
	v, err := svc.SaveManualShippingOrder(ctx, tenant, ContractHandoff{
		ContractNo: "CT-MANUAL", FinalCurrency: "USD", FinalForwarderID: 1, FinalForwarderName: "Forwarder",
		FinalServiceOption: "Ocean", FinalFreightAmount: "100", FinalETD: "2026-08-12", FinalETA: "2026-09-07",
		PaymentTerms: "Prepaid", PortOfLoading: "Shanghai", PortOfDischarge: "Hamburg", CargoItems: cargo,
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	if linked > 0 {
		v, err = svc.LinkManualShippingContract(ctx, tenant, v.ID, linked, "CT-QUALITY", op)
		if err != nil {
			t.Fatal(err)
		}
	}
	svc.UseApprovals(deliveryWorkflowApprovals{})
	v, err = svc.SubmitFinalRequote(ctx, tenant, v.ID, FinalRequoteInput{}, op)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ApplyRequoteApproval(ctx, tenant, v.ID, v.ApprovalInstanceID, "APPROVED", "批准", nil); err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("d4-contracts/shipping/%d/%d/signed.pdf", tenant, v.ID)
	if _, err := svc.SaveShippingContract(ctx, tenant, v.ID, key, "signed.pdf", "FORWARDER-CONTRACT"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VerifyShippingContract(ctx, tenant, v.ID, op); err != nil {
		t.Fatal(err)
	}
	in := validInput()
	in.ContractHandoffID = v.ID
	schedule, err := svc.CreateSchedule(ctx, tenant, in, op, true)
	if err != nil {
		t.Fatal(err)
	}
	return schedule
}

func TestLinkedManualAndNormalBatchesShareInspectionCoverage(t *testing.T) {
	for _, first := range []string{"manual", "normal", "concurrent"} {
		t.Run(first, func(t *testing.T) {
			ctx, pool, tenant := deliveryTestDB(t)
			svc := New(pool, documentFilesStub{size: 1024, contentType: "application/pdf"})
			// The same real item occurs in two official batches. It is one
			// product mapping, not an ambiguous pair of contract lines.
			normal := deliverySchedules(t, ctx, pool, tenant, svc, 2)[0]
			if _, err := pool.Exec(ctx, `UPDATE contract_shipping_handoff_cargo SET product_code='STEEL-1',specification='1mm' WHERE tenant_id=$1`, tenant); err != nil {
				t.Fatal(err)
			}
			manual := manualDeliverySchedule(t, ctx, pool, tenant, svc, 41, []ContractShippingCargoItem{{ProductName: "钢材", Specification: "1mm", Quantity: "4", UomCode: "MT"}, {ProductName: "钢材", Specification: "1mm", Quantity: "6", UomCode: "MT"}})
			quality := &deliveryQualityStub{limit: "10", err: apierr.Conflict("QUALITY_DELIVERY_BLOCKED", "未检")}
			contract := &deliveryContractStub{}
			svc.UseDeliveryQualityGuard(quality)
			svc.UseContractGuard(contract)
			op := Operator{ID: 1, Name: "物流"}
			if _, err := svc.UpdateScheduleStatus(ctx, tenant, manual.ID, "SAILED", "未检补录货物", op); errorCode(err) != "QUALITY_DELIVERY_BLOCKED" {
				t.Fatalf("manual bypassed inspection: %v", err)
			}
			quality.err = nil
			order := []store.ShippingSchedule{manual, normal}
			if first == "normal" {
				order[0], order[1] = normal, manual
			}
			if first == "concurrent" {
				results := make(chan error, 2)
				start := make(chan struct{})
				for _, schedule := range order {
					go func(id int64) {
						<-start
						_, err := svc.UpdateScheduleStatus(ctx, tenant, id, "SAILED", "并发发运", op)
						results <- err
					}(schedule.ID)
				}
				close(start)
				passed, blocked := 0, 0
				for range 2 {
					if err := <-results; err == nil {
						passed++
					} else if errorCode(err) == "QUALITY_QTY_NOT_PASSED" {
						blocked++
					} else {
						t.Fatal(err)
					}
				}
				if passed != 1 || blocked != 1 {
					t.Fatalf("concurrent coverage reused: %d passed/%d blocked", passed, blocked)
				}
			} else {
				if _, err := svc.UpdateScheduleStatus(ctx, tenant, order[0].ID, "SAILED", "第一批", op); err != nil {
					t.Fatal(err)
				}
				if _, err := svc.UpdateScheduleStatus(ctx, tenant, order[1].ID, "SAILED", "重复占用合格量", op); errorCode(err) != "QUALITY_QTY_NOT_PASSED" {
					t.Fatalf("other source was ignored: %v", err)
				}
			}
			quality.limit = "20"
			for _, schedule := range order {
				got, _, err := svc.GetSchedule(ctx, tenant, schedule.ID)
				if err != nil {
					t.Fatal(err)
				}
				if got.Status == "PLANNED" {
					if _, err := svc.UpdateScheduleStatus(ctx, tenant, schedule.ID, "SAILED", "复检合格后重试", op); err != nil {
						t.Fatal(err)
					}
				}
			}
			if len(quality.lines) != 1 || quality.lines[0].ContractItemID != 101 || quality.lines[0].Qty != "20.0000" {
				t.Fatalf("wrong cumulative real source: %+v", quality.lines)
			}
			v, err := svc.GetContractHandoff(ctx, tenant, *manual.ContractHandoffID)
			if err != nil || v.ContractID != 0 || v.ContractVersionID >= 0 || v.CargoItems[0].ContractItemID >= 0 || manual.ContractID != nil || v.LinkedContractID != 41 {
				t.Fatalf("manual identity changed: %+v schedule=%+v err=%v", v, manual, err)
			}
			for _, id := range contract.calls {
				if id != 41 {
					t.Fatalf("contract check used logistics identity: %d", id)
				}
			}
		})
	}
}

func TestLinkedManualDeliveryRejectsUnknownAndAmbiguousCargo(t *testing.T) {
	for _, problem := range []string{"unlinked", "other-tenant", "name", "spec", "unit", "code", "ambiguous"} {
		t.Run(problem, func(t *testing.T) {
			ctx, pool, tenant := deliveryTestDB(t)
			svc := New(pool, documentFilesStub{size: 1024, contentType: "application/pdf"})
			// For the other-tenant case, the contract snapshot is deliberately
			// absent in this tenant; matching contract numbers are no proof.
			if problem != "other-tenant" {
				_ = deliverySchedules(t, ctx, pool, tenant, svc, 1)
			} else {
				otherCtx, otherPool, otherTenant := deliveryTestDB(t)
				_ = deliverySchedules(t, otherCtx, otherPool, otherTenant, New(otherPool), 1)
			}
			cargo := ContractShippingCargoItem{ProductName: "钢材", Quantity: "10", UomCode: "MT"}
			switch problem {
			case "name":
				cargo.ProductName = "Other steel"
			case "spec":
				cargo.Specification = "2mm"
			case "unit":
				cargo.UomCode = "PCS"
			case "code":
				cargo.ProductCode = "WRONG-CODE"
			case "ambiguous":
				if _, err := pool.Exec(ctx, `INSERT INTO contract_shipping_handoff_cargo (tenant_id,handoff_id,contract_item_id,line_no,product_code,product_name,specification,quantity,uom_code,remark)
				 SELECT tenant_id,handoff_id,202,2,'DIFFERENT-SKU',product_name,specification,quantity,uom_code,remark FROM contract_shipping_handoff_cargo WHERE tenant_id=$1`, tenant); err != nil {
					t.Fatal(err)
				}
			}
			linked := int64(41)
			if problem == "unlinked" {
				linked = 0
			}
			manual := manualDeliverySchedule(t, ctx, pool, tenant, svc, linked, []ContractShippingCargoItem{cargo})
			quality := &deliveryQualityStub{}
			svc.UseDeliveryQualityGuard(quality)
			_, err := svc.UpdateScheduleStatus(ctx, tenant, manual.ID, "SAILED", "核验补录来源", Operator{ID: 1, Name: "物流"})
			if err == nil || quality.contractID != 0 {
				t.Fatalf("unknown source reached release: %v %+v", err, quality)
			}
		})
	}
}

func TestManualHistoryRemainsCountedAfterContractRevision(t *testing.T) {
	ctx, pool, tenant := deliveryTestDB(t)
	svc := New(pool, documentFilesStub{size: 1024, contentType: "application/pdf"})
	_ = deliverySchedules(t, ctx, pool, tenant, svc, 1)
	manual := manualDeliverySchedule(t, ctx, pool, tenant, svc, 41, []ContractShippingCargoItem{{ProductName: "钢材", Quantity: "10", UomCode: "MT"}})
	quality := &deliveryQualityStub{limit: "10"}
	svc.UseDeliveryQualityGuard(quality)
	op := Operator{ID: 1, Name: "物流"}
	if _, err := svc.UpdateScheduleStatus(ctx, tenant, manual.ID, "SAILED", "旧合同补录货已发", op); err != nil {
		t.Fatal(err)
	}
	event := ContractEffective{ContractID: 41, ContractNo: "CT-QUALITY", VersionID: 43, VersionNo: 2, Shipments: []ContractEffectiveShipment{{BatchNo: 1, Currency: "USD", FreightAmount: "0", PortOfLoading: "Shanghai", PortOfDischarge: "Hamburg", AllocationStatus: "ALLOCATED", Items: []ContractEffectiveItem{{ContractItemID: 202, LineNo: 1, ProductName: "钢材", Qty: "20", UomCode: "MT"}}}}}
	if err := svc.HandoffsFromContract(ctx, tenant, event, func(context.Context, pgx.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	second := manualDeliverySchedule(t, ctx, pool, tenant, svc, 41, []ContractShippingCargoItem{{ProductName: "钢材", Quantity: "10", UomCode: "MT"}})
	if _, err := svc.UpdateScheduleStatus(ctx, tenant, second.ID, "SAILED", "改版后复用旧合格量", op); errorCode(err) != "QUALITY_QTY_NOT_PASSED" {
		t.Fatalf("manual history lost across versions: %v", err)
	}
	if len(quality.lines) != 1 || quality.lines[0].ContractItemID != 202 || quality.lines[0].Qty != "20.0000" {
		t.Fatalf("wrong revised cumulative source: %+v", quality.lines)
	}
	quality.limit = "20"
	if _, err := svc.UpdateScheduleStatus(ctx, tenant, second.ID, "SAILED", "补检合格后发运", op); err != nil {
		t.Fatal(err)
	}
}

func TestLinkedManualContractControlsDelegationAndNewSchedules(t *testing.T) {
	for _, state := range []string{"PAUSED", "TERMINATING", "TERMINATED"} {
		for _, entry := range []string{"submit", "schedule"} {
			t.Run(state+"/"+entry, func(t *testing.T) {
				ctx, pool, tenant := deliveryTestDB(t)
				svc := New(pool, documentFilesStub{size: 1024, contentType: "application/pdf"})
				svc.UseContractGuard(workflowAllowedContract{})
				svc.UseApprovals(deliveryWorkflowApprovals{})
				op := Operator{ID: 1, Name: "物流"}
				v, err := svc.SaveManualShippingOrder(ctx, tenant, ContractHandoff{
					ContractNo: "CT-MANUAL", FinalCurrency: "USD", FinalForwarderID: 1, FinalForwarderName: "Forwarder",
					FinalServiceOption: "Ocean", FinalFreightAmount: "100", FinalETD: "2026-08-12", FinalETA: "2026-09-07",
					PaymentTerms: "Prepaid", PortOfLoading: "Shanghai", PortOfDischarge: "Hamburg",
					CargoItems: []ContractShippingCargoItem{{ProductName: "钢材", Quantity: "10", UomCode: "MT"}},
				}, op)
				if err != nil {
					t.Fatal(err)
				}
				v, err = svc.LinkManualShippingContract(ctx, tenant, v.ID, 41, "CT-QUALITY", op)
				if err != nil {
					t.Fatal(err)
				}
				if entry == "schedule" {
					v, err = svc.SubmitFinalRequote(ctx, tenant, v.ID, FinalRequoteInput{}, op)
					if err != nil {
						t.Fatal(err)
					}
					if err := svc.ApplyRequoteApproval(ctx, tenant, v.ID, v.ApprovalInstanceID, "APPROVED", "批准", nil); err != nil {
						t.Fatal(err)
					}
					key := fmt.Sprintf("d4-contracts/shipping/%d/%d/signed.pdf", tenant, v.ID)
					if _, err := svc.SaveShippingContract(ctx, tenant, v.ID, key, "signed.pdf", "F-1"); err != nil {
						t.Fatal(err)
					}
					if _, err := svc.VerifyShippingContract(ctx, tenant, v.ID, op); err != nil {
						t.Fatal(err)
					}
				}
				guard := &deliveryContractStub{err: apierr.Conflict("CONTRACT_EXECUTION_BLOCKED", state)}
				svc.UseContractGuard(guard)
				if entry == "submit" {
					_, err = svc.SubmitFinalRequote(ctx, tenant, v.ID, FinalRequoteInput{}, op)
				} else {
					in := validInput()
					in.ContractHandoffID = v.ID
					_, err = svc.CreateSchedule(ctx, tenant, in, op, true)
				}
				if errorCode(err) != "CONTRACT_EXECUTION_BLOCKED" || len(guard.calls) != 1 || guard.calls[0] != 41 {
					t.Fatalf("linked contract bypassed on %s: %v calls=%v", entry, err, guard.calls)
				}
				guard.err = nil
				if entry == "submit" {
					v, err = svc.SubmitFinalRequote(ctx, tenant, v.ID, FinalRequoteInput{}, op)
					if err != nil || v.Status != "PENDING_APPROVAL" {
						t.Fatalf("resumed delegation failed: %s %v", v.Status, err)
					}
				} else {
					in := validInput()
					in.ContractHandoffID = v.ID
					got, err := svc.CreateSchedule(ctx, tenant, in, op, true)
					if err != nil || got.Status != "PLANNED" || got.ContractID != nil {
						t.Fatalf("resumed manual plan failed: %+v %v", got, err)
					}
				}
			})
		}
	}
}
