package app

import "testing"

func steelDefs() []AttributeDef {
	return []AttributeDef{
		{Key: "material", Label: "材质", DataType: "ENUM", Level: LevelProduct,
			EnumValues: []string{"Q235B", "SPCC"}, IsMatchable: true},
		{Key: "thickness_mm", Label: "厚度", DataType: "DIMENSION", Level: LevelSKU,
			MatchStrategy: "TOLERANCE", TolerancePct: "3", IsMatchable: true},
		{Key: "width_mm", Label: "宽度", DataType: "DIMENSION", Level: LevelSKU, IsMatchable: true},
		{Key: "surface", Label: "表面", DataType: "ENUM", Level: LevelSKU,
			EnumValues: []string{"酸洗", "光亮"}, IsMatchable: true},
		{Key: "note", Label: "备注", DataType: "TEXT", Level: LevelSKU, IsMatchable: false},
	}
}

func TestLineageParsesMaterialisedPath(t *testing.T) {
	got := lineageOf("/1/4/9/", 9)
	want := []int64{1, 4, 9}
	if len(got) != len(want) {
		t.Fatalf("lineage = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("lineage = %v, want %v", got, want)
		}
	}
}

func TestLineageFallsBackWhenPathIsEmpty(t *testing.T) {
	// A flat tree, or a category whose path was never materialised, must
	// still resolve its own template rather than returning nothing.
	got := lineageOf("", 7)
	if len(got) != 1 || got[0] != 7 {
		t.Fatalf("lineage = %v, want [7]", got)
	}
}

func TestSignatureIgnoresKeyOrderAndNumberFormat(t *testing.T) {
	defs := steelDefs()
	a := signatureOf(defs, map[string]any{
		"thickness_mm": 3.0, "width_mm": 1250, "surface": "酸洗",
	})
	b := signatureOf(defs, map[string]any{
		"surface": "酸洗", "width_mm": 1250.0, "thickness_mm": "3.00",
	})
	if a != b {
		t.Fatalf("same spec produced different signatures:\n  %q\n  %q", a, b)
	}
	if a == "" {
		t.Fatal("signature is empty for a fully specified SKU")
	}
}

func TestSignatureExcludesProductLevelAndUnmatchable(t *testing.T) {
	defs := steelDefs()
	// material is PRODUCT level and note is not matchable; neither may reach
	// the signature, or two SKUs that differ only in a remark would be
	// treated as different variants.
	withNote := signatureOf(defs, map[string]any{
		"material": "Q235B", "thickness_mm": 3.0, "note": "客户特别要求",
	})
	withoutNote := signatureOf(defs, map[string]any{"thickness_mm": 3.0})
	if withNote != withoutNote {
		t.Fatalf("signature leaked a product-level or unmatchable field:\n  %q\n  %q",
			withNote, withoutNote)
	}
}

func TestSignatureEmptyWhenNothingStructured(t *testing.T) {
	// Level 0: no structured values at all. An empty signature keeps the
	// partial unique index from applying, which is what lets unstructured
	// tenants keep writing SKUs.
	if got := signatureOf(steelDefs(), map[string]any{}); got != "" {
		t.Fatalf("signature = %q, want empty", got)
	}
}

func TestValidateRejectsBadEnumAndNonNumber(t *testing.T) {
	defs := steelDefs()
	if err := validateValues(defs, map[string]any{"surface": "镀锌"}, LevelSKU); err == nil {
		t.Fatal("expected a refusal for a value outside the enum")
	}
	if err := validateValues(defs, map[string]any{"thickness_mm": "厚一点"}, LevelSKU); err == nil {
		t.Fatal("expected a refusal for a non-numeric dimension")
	}
}

func TestValidateChecksOnlyItsOwnLevel(t *testing.T) {
	defs := steelDefs()
	// A bad SKU-level value must not trip validation of the product level,
	// and vice versa — the two are written by separate calls.
	if err := validateValues(defs, map[string]any{"surface": "镀锌"}, LevelProduct); err != nil {
		t.Fatalf("product-level validation looked at a SKU field: %v", err)
	}
}

func TestValidateLetsUnknownKeysThrough(t *testing.T) {
	// Anything the template does not model is stored as-is. A tenant with no
	// template has nothing but unknown keys, and refusing them would make the
	// catalogue unwritable.
	if err := validateValues(steelDefs(), map[string]any{"coating": "PE"}, LevelSKU); err != nil {
		t.Fatalf("unknown key was refused: %v", err)
	}
	if err := validateValues(nil, map[string]any{"anything": 1}, LevelSKU); err != nil {
		t.Fatalf("value refused with no template at all: %v", err)
	}
}

func TestValidateRequiresWhatIsMarkedRequired(t *testing.T) {
	defs := []AttributeDef{
		{Key: "width_mm", Label: "宽度", DataType: "DIMENSION", Level: LevelSKU, IsRequired: true},
	}
	if err := validateValues(defs, map[string]any{}, LevelSKU); err == nil {
		t.Fatal("expected a refusal for a missing required attribute")
	}
	if err := validateValues(defs, map[string]any{"width_mm": 1250}, LevelSKU); err != nil {
		t.Fatalf("valid value refused: %v", err)
	}
}

func TestValidateAcceptsEnumWithNoValuesConfigured(t *testing.T) {
	// A half-filled template must stay usable: an ENUM whose allowed list is
	// still empty accepts anything rather than blocking every write.
	defs := []AttributeDef{{Key: "surface", Label: "表面", DataType: "ENUM", Level: LevelSKU}}
	if err := validateValues(defs, map[string]any{"surface": "任意值"}, LevelSKU); err != nil {
		t.Fatalf("empty enum list refused a value: %v", err)
	}
}

func TestAttrKeyRules(t *testing.T) {
	ok := []string{"material", "thickness_mm", "a1"}
	bad := []string{"", "1abc", "Material", "thickness-mm", "厚度"}
	for _, k := range ok {
		if !validAttrKey(k) {
			t.Errorf("key %q should be allowed", k)
		}
	}
	for _, k := range bad {
		if validAttrKey(k) {
			t.Errorf("key %q should be refused", k)
		}
	}
}
