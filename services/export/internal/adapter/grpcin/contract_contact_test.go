package grpcin

import (
	"encoding/json"
	"testing"

	"github.com/sgao19/erp-go/services/export/internal/store"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestContractContactPreservesLegacyAndExplicitBlankPresence(t *testing.T) {
	legacy := versionToProto(store.GetContractVersionRow{})
	if legacy.CustomerContact != nil {
		t.Fatal("legacy contact must retain absent presence")
	}
	legacyData, err := (protojson.MarshalOptions{EmitUnpopulated: true}).Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var legacyFields map[string]json.RawMessage
	if err := json.Unmarshal(legacyData, &legacyFields); err != nil {
		t.Fatal(err)
	}
	if _, present := legacyFields["customerContact"]; present {
		t.Fatal("legacy contact must remain absent in JSON so the quoted contact can be used")
	}
	empty := ""
	blank := versionToProto(store.GetContractVersionRow{CustomerContact: &empty})
	data, err := (protojson.MarshalOptions{EmitUnpopulated: true}).Marshal(blank)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	value, present := fields["customerContact"]
	if !present || string(value) != `""` {
		t.Fatalf("explicit blank contact omitted: %s", data)
	}
}
