package app

import (
	"encoding/json"
	"testing"
)

func TestDecodeInquiryTemplateSnapshotKeepsIdentity(t *testing.T) {
	want := inquiryTemplateSnapshot{
		ID: 42, Code: "CUSTOMER_A", Version: 3,
		Columns: []InquiryColumn{{FieldKey: "product", DisplayName: "产品"}},
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeInquiryTemplateSnapshot(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.Code != want.Code || got.Version != want.Version || len(got.Columns) != 1 {
		t.Fatalf("snapshot mismatch: %#v", got)
	}
}

func TestDecodeInquiryTemplateSnapshotAcceptsLegacyColumns(t *testing.T) {
	data, err := json.Marshal([]InquiryColumn{{FieldKey: "product", DisplayName: "产品"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeInquiryTemplateSnapshot(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 0 || got.Code != "" || got.Version != 0 || len(got.Columns) != 1 {
		t.Fatalf("legacy snapshot mismatch: %#v", got)
	}
}
