package app

import "testing"

func TestExecutionDraftGroupsSeparateBuyersAndOrderTerms(t *testing.T) {
	base := ExecutionDraftLine{BuyerID: 21, SupplierID: 6, Currency: "CNY", ContractNo: "CT-001",
		ExecutionDraftInput: ExecutionDraftInput{Date: "2027-01-02", Terms: "T/T"}}
	tasks := make([]ExecutionDraftLine, 8)
	for index := range tasks {
		tasks[index] = base
		tasks[index].RequirementID = int64(index + 1)
	}
	tasks[2].BuyerID = 22
	tasks[3].SupplierID = 7
	tasks[4].Currency = "USD"
	tasks[5].Terms = "L/C"
	tasks[6].Date = "2027-01-03"
	tasks[7].RequirementID = tasks[0].RequirementID
	groups := groupExecutionDraftLines(tasks)
	if len(groups) != 7 || len(groups[0].Lines) != 2 || groups[0].Lines[1].RequirementID != 2 {
		t.Fatalf("unexpected draft groups: %+v", groups)
	}
}
