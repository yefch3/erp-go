package app

import (
	"testing"

	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

func TestCanCreateFactoryRFQForRework(t *testing.T) {
	const buyerID int64 = 23
	tests := []struct {
		name string
		rows []store.ListProcurementReworkRequestsRow
		want bool
	}{
		{name: "no rework"},
		{name: "resolved task", rows: []store.ListProcurementReworkRequestsRow{{Status: "RESOLVED", AssignedBuyerID: buyerID}}},
		{name: "another buyer", rows: []store.ListProcurementReworkRequestsRow{{Status: "OPEN", AssignedBuyerID: 99}}},
		{name: "assigned buyer", rows: []store.ListProcurementReworkRequestsRow{{Status: "OPEN", AssignedBuyerID: buyerID}}, want: true},
		{name: "unassigned team task", rows: []store.ListProcurementReworkRequestsRow{{Status: "OPEN", AssignedBuyerID: 0}}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canCreateFactoryRFQForRework(tt.rows, buyerID); got != tt.want {
				t.Fatalf("canCreateFactoryRFQForRework()=%v, want %v", got, tt.want)
			}
		})
	}
}
