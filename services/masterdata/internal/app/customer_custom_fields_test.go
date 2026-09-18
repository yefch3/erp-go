package app

import "testing"

func TestCustomerFieldKeyIsStableAcrossWhitespaceAndCase(t *testing.T) {
	first := customerFieldKey(" Customer Level ")
	second := customerFieldKey("customer   level")
	if first != second || first == "" {
		t.Fatalf("expected stable non-empty key, got %q and %q", first, second)
	}
}

func TestNormalizeFieldAliasesAndImportTags(t *testing.T) {
	aliases := normalizeFieldAliases([]string{"区域", " 区域 ", "REGION", "region", ""})
	if len(aliases) != 2 || aliases[0] != "区域" || aliases[1] != "REGION" {
		t.Fatalf("unexpected aliases: %#v", aliases)
	}
	tags := splitImportTags("重点客户，钢材; Europe；重点客户")
	if len(tags) != 3 || tags[0] != "重点客户" || tags[2] != "Europe" {
		t.Fatalf("unexpected tags: %#v", tags)
	}
}
