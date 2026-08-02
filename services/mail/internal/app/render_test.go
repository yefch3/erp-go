package app

import "testing"

var alice = Recipient{
	Name: "John Smith", Email: "john@abccorp.com", CustomerName: "ABC Corp",
}
var me = Sender{Name: "李娜", Title: "Sales Manager", Email: "lina@ours.com", Phone: "+86 138"}

func TestRenderSubstitutes(t *testing.T) {
	got, missing := Render("Dear {{contact_name}}, from {{my_name}}", alice, me)
	if len(missing) != 0 {
		t.Fatalf("unexpected missing: %v", missing)
	}
	if got != "Dear John Smith, from 李娜" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderReportsMissingRatherThanBlanking(t *testing.T) {
	// The whole point: an unresolved variable must be reported so the caller
	// can route the message to a person. Substituting an empty string would
	// send "Dear ," to a customer, which cannot be taken back.
	blank := Recipient{Email: "x@y.com"}
	got, missing := Render("Dear {{contact_name}},", blank, me)
	if len(missing) != 1 || missing[0] != "contact_name" {
		t.Fatalf("missing = %v, want [contact_name]", missing)
	}
	if got == "Dear ," {
		t.Fatal("blanked the variable instead of reporting it")
	}
}

func TestRenderTreatsWhitespaceValueAsMissing(t *testing.T) {
	// A contact whose name is a stray space is the same problem as no name.
	spaces := Recipient{Name: "   ", Email: "x@y.com"}
	_, missing := Render("Dear {{contact_name}},", spaces, me)
	if len(missing) != 1 {
		t.Fatalf("missing = %v, want contact_name", missing)
	}
}

func TestRenderIgnoresBareWords(t *testing.T) {
	// Delimiters exist so prose that happens to mention a variable name is
	// left alone. A bare-word scheme would rewrite this sentence.
	tpl := "Please put contact_name in the reference field."
	got, missing := Render(tpl, alice, me)
	if got != tpl {
		t.Fatalf("rewrote prose: %q", got)
	}
	if len(missing) != 0 {
		t.Fatalf("unexpected missing: %v", missing)
	}
}

func TestRenderToleratesInnerSpacing(t *testing.T) {
	got, missing := Render("Hi {{ contact_first_name }}", alice, me)
	if len(missing) != 0 || got != "Hi John" {
		t.Fatalf("got %q missing %v", got, missing)
	}
}

func TestFirstNameSplitsLatinButNotCJK(t *testing.T) {
	if got := firstName("John Smith"); got != "John" {
		t.Errorf("firstName(John Smith) = %q", got)
	}
	// A Chinese name is short and idiomatically used whole; slicing it would
	// address the customer by their surname alone, which reads as brusque.
	if got := firstName("李娜"); got != "李娜" {
		t.Errorf("firstName(李娜) = %q", got)
	}
	if got := firstName(""); got != "" {
		t.Errorf("firstName(empty) = %q", got)
	}
}

func TestRenderUsesExtraValues(t *testing.T) {
	r := alice
	r.Extra = map[string]string{"contract_no": "CT-202607-0027"}
	got, missing := Render("Re: {{contract_no}}", r, me)
	if len(missing) != 0 || got != "Re: CT-202607-0027" {
		t.Fatalf("got %q missing %v", got, missing)
	}
}

func TestVariablesInListsEachOnce(t *testing.T) {
	got := VariablesIn("{{contact_name}} {{my_name}} {{contact_name}}")
	if len(got) != 2 || got[0] != "contact_name" || got[1] != "my_name" {
		t.Fatalf("VariablesIn = %v", got)
	}
}

func TestNewMessageKeyIsUniqueAndWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		k, err := newMessageKey()
		if err != nil {
			t.Fatalf("newMessageKey: %v", err)
		}
		if len(k) != 36 {
			t.Fatalf("key %q is not a uuid", k)
		}
		if seen[k] {
			t.Fatalf("duplicate key %q — retries would collide", k)
		}
		seen[k] = true
	}
}
