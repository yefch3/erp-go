package app

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sgao19/erp-go/services/shipping/internal/store"
)

func routeTime(value string) pgtype.Timestamptz {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func TestValidateRouteTimelineRejectsLaterPortBeforePreviousDeparture(t *testing.T) {
	nodes := []store.ShippingRouteNode{
		{SequenceNo: 1, PortName: "北京", LatestEtdAt: routeTime("2026-08-05T00:00:00Z")},
		{SequenceNo: 2, PortName: "天津", LatestEtaAt: routeTime("2026-08-04T04:00:00Z"), LatestEtdAt: routeTime("2026-08-06T04:00:00Z")},
	}
	if code := errorCode(validateRouteTimeline(nodes, true, true)); code != "SHIPPING_ROUTE_TIMELINE_INVALID" {
		t.Fatalf("timeline error code = %q", code)
	}
}

func TestValidateRouteTimelineAcceptsChronologicalRoute(t *testing.T) {
	nodes := []store.ShippingRouteNode{
		{SequenceNo: 1, PortName: "北京", LatestEtdAt: routeTime("2026-08-05T00:00:00Z")},
		{SequenceNo: 2, PortName: "天津", LatestEtaAt: routeTime("2026-08-05T04:00:00Z"), LatestEtdAt: routeTime("2026-08-06T04:00:00Z")},
		{SequenceNo: 3, PortName: "新加坡", LatestEtaAt: routeTime("2026-08-10T04:00:00Z")},
	}
	if err := validateRouteTimeline(nodes, true, true); err != nil {
		t.Fatalf("chronological route rejected: %v", err)
	}
}

func TestActualProgressIgnoresLegacyEstimateError(t *testing.T) {
	nodes := []store.ShippingRouteNode{
		{SequenceNo: 1, PortName: "北京", LatestEtdAt: routeTime("2026-08-05T00:00:00Z"), ActualDepartureAt: routeTime("2026-08-05T01:00:00Z")},
		{SequenceNo: 2, PortName: "天津", LatestEtaAt: routeTime("2026-08-04T04:00:00Z"), ActualArrivalAt: routeTime("2026-08-05T03:00:00Z")},
		{SequenceNo: 3, PortName: "亚特兰大", ActualArrivalAt: routeTime("2026-08-19T00:00:00Z")},
	}
	if err := validateRouteTimeline(nodes, false, true); err != nil {
		t.Fatalf("actual progress was blocked by an unrelated estimate error: %v", err)
	}
}

func TestDeriveProgressKeepsHistoryAndSelectsNextPort(t *testing.T) {
	nodes := []store.ShippingRouteNode{
		{ID: 1, PortName: "上海", NodeType: "ORIGIN", NodeStatus: "DEPARTED"},
		{ID: 2, PortName: "釜山", NodeType: "TRANSIT", NodeStatus: "DEPARTED"},
		{ID: 3, PortName: "汉堡", NodeType: "DESTINATION", NodeStatus: "PLANNED"},
	}
	id, progress, status := deriveProgress(nodes)
	if id != 3 || progress != "驶往 汉堡" || status != "IN_TRANSIT" {
		t.Fatalf("deriveProgress() = (%d, %q, %q)", id, progress, status)
	}
}

func TestDeriveProgressRollsBackAfterCorrection(t *testing.T) {
	nodes := []store.ShippingRouteNode{
		{ID: 1, PortName: "上海", NodeType: "ORIGIN", NodeStatus: "PLANNED"},
		{ID: 2, PortName: "汉堡", NodeType: "DESTINATION", NodeStatus: "PLANNED"},
	}
	id, progress, status := deriveProgress(nodes)
	if id != 1 || progress != "等待离开 上海" || status != "PLANNED" {
		t.Fatalf("deriveProgress() = (%d, %q, %q)", id, progress, status)
	}
}
