package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

func TestSubmissionRequiresEveryContractPartyField(t *testing.T) {
	for _, field := range []string{"buyer name", "buyer address", "seller name", "seller address"} {
		t.Run(field, func(t *testing.T) {
			v := view("DRAFT", "DRAFT", "2026-12-01", 1)
			switch field {
			case "buyer name":
				v.Version.BuyerName = " \t"
			case "buyer address":
				v.Version.BuyerAddress = " \n"
			case "seller name":
				v.Version.SellerName = "　"
			case "seller address":
				v.Version.SellerAddress = ""
			}
			if err := readyToSubmit(v); apierr.CodeFromError(err) != "EX_CONTRACT_PARTIES_REQUIRED" {
				t.Fatalf("missing %s was not rejected: %v", field, err)
			}
		})
	}
}

func TestEntryDefaultsRequireTrustedIdentityAndExposeOnlyConfiguredSeller(t *testing.T) {
	svc := New(nil, Deps{Scopes: d2OfferScopes{}, Seller: Seller{Name: "Configured seller", Address: "Configured address"}})
	ctx := grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: 17, EmployeeID: 1})
	defaults, err := svc.ContractWorkflow(ctx, 17, WorkflowCommand{Action: "entry_defaults"}, Operator{ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	values := defaults.(map[string]string)
	if values["sellerName"] != "Configured seller" || values["sellerAddress"] != "Configured address" {
		t.Fatal(values)
	}
	_, err = svc.ContractWorkflow(context.Background(), 17, WorkflowCommand{Action: "entry_defaults"}, Operator{ID: 1})
	if err == nil {
		t.Fatal("anonymous request obtained defaults")
	}
	svc.seller = Seller{}
	defaults, err = svc.ContractWorkflow(ctx, 17, WorkflowCommand{Action: "entry_defaults"}, Operator{ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range defaults.(map[string]string) {
		if strings.TrimSpace(value) != "" {
			t.Fatal("blank configuration produced an implicit seller")
		}
	}
}
