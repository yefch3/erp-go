package app

import (
	"context"
	"encoding/json"
	"testing"
)

type laycanApprovalRecorder struct {
	input ApprovalSubmission
}

func (r *laycanApprovalRecorder) Submit(_ context.Context, in ApprovalSubmission) (int64, error) {
	r.input = in
	return 701, nil
}

func TestD4LaycanPersistsThroughApprovalWithoutBecomingScheduleDates(t *testing.T) {
	ctx, pool, tenant := deliveryTestDB(t)
	svc := New(pool, documentFilesStub{size: 1024, contentType: "application/pdf"})
	svc.UseContractGuard(workflowAllowedContract{})
	approvals := &laycanApprovalRecorder{}
	svc.UseApprovals(approvals)
	op := Operator{ID: 1, Name: "物流"}
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO contract_shipping_handoffs
		(tenant_id,contract_id,contract_no,contract_version_id,version_no,batch_no,currency,status)
		VALUES($1,99,'CT-LAYCAN',1,1,1,'USD','WAITING_REQUOTE') RETURNING id`, tenant).Scan(&id); err != nil {
		t.Fatal(err)
	}

	// Legacy rows remain readable and don't acquire fabricated Laycan dates.
	if _, err := pool.Exec(ctx, `INSERT INTO shipping_execution_requote_options
		(tenant_id,handoff_id,forwarder_id,forwarder_name,service_option,currency,freight_amount,etd,eta,payment_terms)
		VALUES ($1,$2,2,'Legacy','','USD',300,'2026-11-27','2026-12-30','T/T')`, tenant, id); err != nil {
		t.Fatal(err)
	}
	in := FinalRequoteInput{FinalForwarderID: 1, FinalForwarderName: "Forwarder",
		FinalCurrency: "USD", FinalFreightAmount: "250", LaycanStart: "2026-12-01", LaycanEnd: "2026-12-03"}
	if _, err := svc.SaveFinalRequoteDraft(ctx, tenant, id, in, op); err != nil {
		t.Fatal(err)
	}
	options, err := svc.ListShippingRequoteOptions(ctx, tenant, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 2 || options[0].LaycanStart != in.LaycanStart || options[0].LaycanEnd != in.LaycanEnd ||
		options[0].ETD != "" || options[0].ETA != "" || options[0].PaymentTerms != "" ||
		options[1].LaycanStart != "" || options[1].ETD != "2026-11-27" {
		t.Fatalf("unexpected candidates: %+v", options)
	}
	in.OptionID, in.LaycanEnd = options[0].ID, "2026-12-05"
	if _, err := svc.SaveFinalRequoteDraft(ctx, tenant, id, in, op); err != nil {
		t.Fatal(err)
	}
	draft, err := svc.SelectFinalRequoteDraft(ctx, tenant, id, in.OptionID, op)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Status != "DRAFT" || draft.LaycanStart != in.LaycanStart || draft.LaycanEnd != in.LaycanEnd ||
		draft.FinalETD != "" || draft.FinalETA != "" || draft.PaymentTerms != "" {
		t.Fatalf("unexpected selected draft: %+v", draft)
	}
	approved, err := svc.SubmitFinalRequote(ctx, tenant, id, FinalRequoteInput{}, op)
	if err != nil || approved.Status != "PENDING_APPROVAL" || approved.LaycanEnd != in.LaycanEnd {
		t.Fatalf("submit draft: %+v, %v", approved, err)
	}
	var summary map[string]string
	if err := json.Unmarshal([]byte(approvals.input.Summary), &summary); err != nil {
		t.Fatal(err)
	}
	if summary["laycan"] != "2026-12-01 ~ 2026-12-05" || summary["etd"] != "" || summary["payment_terms"] != "" {
		t.Fatalf("unexpected approval summary: %v", summary)
	}
	if err := svc.ApplyRequoteApproval(ctx, tenant, id, approved.ApprovalInstanceID, "APPROVED", "批准", nil); err != nil {
		t.Fatal(err)
	}
	key := shippingContractPrefix(tenant, id) + "signed.pdf"
	if _, err := svc.SaveShippingContract(ctx, tenant, id, key, "signed.pdf", "LC-LAYCAN"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VerifyShippingContract(ctx, tenant, id, op); err != nil {
		t.Fatal(err)
	}
	schedule := validInput()
	schedule.ContractHandoffID = id
	schedule.ETD, schedule.ETA = "", ""
	if _, err := svc.CreateSchedule(ctx, tenant, schedule, op, true); err == nil {
		t.Fatal("formal schedule should still require ETD and ETA")
	}
	schedule.ETD, schedule.ETA = "2026-12-08", "2027-01-10"
	saved, err := svc.CreateSchedule(ctx, tenant, schedule, op, true)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Etd.Time.Format("2006-01-02") != schedule.ETD || saved.Eta.Time.Format("2006-01-02") != schedule.ETA {
		t.Fatalf("formal schedule dates don't match their own input: %v / %v", saved.Etd, saved.Eta)
	}
}
