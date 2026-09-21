package grpcout

import (
	"testing"

	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
)

func TestCustomerSnapshotNamePrefersShortName(t *testing.T) {
	for _, test := range []struct {
		name     string
		customer *mdv1.Customer
		want     string
	}{
		{name: "简称优先", customer: &mdv1.Customer{Name: "完整客户名称", ShortName: "客户简称"}, want: "客户简称"},
		{name: "简称为空回退全称", customer: &mdv1.Customer{Name: "完整客户名称"}, want: "完整客户名称"},
		{name: "清理空白", customer: &mdv1.Customer{Name: " 完整客户名称 ", ShortName: "  "}, want: "完整客户名称"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := customerSnapshotName(test.customer); got != test.want {
				t.Fatalf("customerSnapshotName()=%q want %q", got, test.want)
			}
		})
	}
}
