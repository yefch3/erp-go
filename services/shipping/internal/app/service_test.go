package app

import (
	"context"
	"errors"
	"testing"

	"github.com/sgao19/erp-go/pkg/apierr"
)

type pingerStub struct{ err error }

func (p pingerStub) Ping(context.Context) error { return p.err }

func TestModuleStatus(t *testing.T) {
	if err := New(pingerStub{}).ModuleStatus(context.Background()); err != nil {
		t.Fatalf("healthy database reported an error: %v", err)
	}
	want := errors.New("database unavailable")
	if err := New(pingerStub{err: want}).ModuleStatus(context.Background()); !errors.Is(err, want) {
		t.Fatalf("ModuleStatus error = %v, want %v", err, want)
	}
}

func validInput() ScheduleInput {
	return ScheduleInput{
		VesselName: "Ever Given", VoyageNo: "EG001", PortOfLoading: "Shanghai",
		PortOfDischarge: "Hamburg", ETD: "2026-08-10", ETA: "2026-09-05",
		ResponsibleEmployeeID: 7, ResponsibleName: "Test User",
	}
}

func errorCode(err error) string {
	var api *apierr.Error
	if errors.As(err, &api) {
		return api.Code
	}
	return ""
}

func TestValidateScheduleInput(t *testing.T) {
	if _, err := validateInput(validInput()); err != nil {
		t.Fatalf("valid input: %v", err)
	}

	missing := validInput()
	missing.VesselName = ""
	if _, err := validateInput(missing); errorCode(err) != "SHIPPING_REQUIRED_FIELDS" {
		t.Fatalf("missing vessel code = %q, err=%v", errorCode(err), err)
	}

	backwards := validInput()
	backwards.ETA = "2026-08-09"
	if _, err := validateInput(backwards); errorCode(err) != "SHIPPING_ETA_BEFORE_ETD" {
		t.Fatalf("backwards dates code = %q, err=%v", errorCode(err), err)
	}

	invalid := validInput()
	invalid.ETD = "08/10/2026"
	if _, err := validateInput(invalid); errorCode(err) != "SHIPPING_DATE_INVALID" {
		t.Fatalf("invalid date code = %q, err=%v", errorCode(err), err)
	}

	linked := validInput()
	linked.CustomerID, linked.CarrierID = 41, 52
	linked.ATD, linked.ATA = "2026-08-11", "2026-09-04"
	params, err := validateInput(linked)
	if err != nil {
		t.Fatalf("linked input: %v", err)
	}
	if params.CustomerID == nil || *params.CustomerID != 41 || params.CarrierID == nil || *params.CarrierID != 52 {
		t.Fatalf("masterdata links were not retained: customer=%v carrier=%v", params.CustomerID, params.CarrierID)
	}
	if params.Atd.Valid || params.Ata.Valid {
		t.Fatal("ATD/ATA must be recorded through progress tracking, not schedule creation")
	}
}

func TestStatusTransitions(t *testing.T) {
	if !transitions["PLANNED"]["SAILED"] || !transitions["IN_TRANSIT"]["ARRIVED"] || !transitions["ARRIVED"]["COMPLETED"] {
		t.Fatal("expected forward status transitions")
	}
	if transitions["PLANNED"]["COMPLETED"] || transitions["COMPLETED"]["PLANNED"] {
		t.Fatal("status sequence can be skipped or reopened")
	}
}
