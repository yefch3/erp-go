package app

import "testing"

func TestCustomerDisplayName(t *testing.T) {
	for _, test := range []struct {
		name     string
		customer Customer
		want     string
	}{
		{name: "short name", customer: Customer{Name: "Example Trading Company Limited", ShortName: "Example"}, want: "Example"},
		{name: "full name fallback", customer: Customer{Name: "Example Trading Company Limited"}, want: "Example Trading Company Limited"},
		{name: "trim", customer: Customer{Name: " Full Name ", ShortName: "  "}, want: "Full Name"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := customerDisplayName(test.customer); got != test.want {
				t.Fatalf("customerDisplayName()=%q want %q", got, test.want)
			}
		})
	}
}
