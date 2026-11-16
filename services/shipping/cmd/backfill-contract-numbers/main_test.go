package main

import "testing"

func TestOnlyNumberMetadataMayChange(t *testing.T) {
	before := []byte(`{"number":"","generated":false,"status":"CONTRACT_UPLOADED","file":"signed.pdf","verified":null,"paid":"0","updated_at":"unchanged"}`)
	ok := []byte(`{"number":"LC-1","generated":true,"status":"CONTRACT_UPLOADED","file":"signed.pdf","verified":null,"paid":"0","updated_at":"unchanged"}`)
	if err := onlyMetadataChanged(before, ok, "number", "generated"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`{"status":"PAYMENT_REQUESTED","file":"signed.pdf","verified":null,"paid":"0","updated_at":"unchanged"}`,
		`{"status":"CONTRACT_UPLOADED","file":"other.pdf","verified":null,"paid":"0","updated_at":"unchanged"}`,
		`{"status":"CONTRACT_UPLOADED","file":"signed.pdf","verified":"approved","paid":"0","updated_at":"unchanged"}`,
		`{"status":"CONTRACT_UPLOADED","file":"signed.pdf","verified":null,"paid":"50","updated_at":"unchanged"}`,
		`{"status":"CONTRACT_UPLOADED","file":"signed.pdf","verified":null,"paid":"0","updated_at":"changed"}`,
	} {
		if onlyMetadataChanged(before, []byte(bad), "number", "generated") == nil {
			t.Fatal("non-metadata change accepted")
		}
	}
}
