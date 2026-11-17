package grpcin

import (
	"testing"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestExpenseReimbursementProtoMapping(t *testing.T) {
	for _, kind := range []string{"TRAVEL", "OFFICE_SUPPLIES", "OTHER"} {
		request := &prv1.CreateTravelReimbursementRequest{}
		if err := protojson.Unmarshal([]byte(`{"expenseType":"`+kind+`","expenseDate":"2026-11-17","purpose":"Supplies","amount":"10.50","currency":"CNY","paymentAccount":"Synthetic"}`), request); err != nil {
			t.Fatal(err)
		}
		in := travelInput(request.GetTripStart(), request.GetTripEnd(), request.GetOrigin(), request.GetDestination(), request.GetPurpose(), request.GetAmount(), request.GetCurrency(), request.GetPaymentAccount(), request.GetNote(), request.GetExpenseType(), request.GetExpenseDate())
		if in.ExpenseType != kind || in.ExpenseDate != "2026-11-17" || in.Amount != "10.50" {
			t.Fatalf("input=%+v", in)
		}
		out := travelProto(app.TravelReimbursement{ExpenseType: in.ExpenseType, ExpenseDate: in.ExpenseDate, Amount: in.Amount})
		if out.GetExpenseType() != kind || out.GetExpenseDate() != in.ExpenseDate || out.GetAmount() != in.Amount {
			t.Fatalf("output=%+v", out)
		}
	}
}
