package grpcin

import (
	"strings"
	"testing"

	"github.com/sgao19/erp-go/services/export/internal/store"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestContractContactPreservesLegacyAndExplicitBlankPresence(t *testing.T) {
	legacy := versionToProto(store.GetContractVersionRow{})
	if legacy.CustomerContact != nil {
		t.Fatal("legacy contact must retain absent presence")
	}
	empty := ""
	blank := versionToProto(store.GetContractVersionRow{CustomerContact: &empty})
	data, err := (protojson.MarshalOptions{EmitUnpopulated: true}).Marshal(blank)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"customerContact":""`) {
		t.Fatalf("explicit blank contact omitted: %s", data)
	}
}
