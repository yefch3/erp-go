package grpcin

import (
	"encoding/json"
	"testing"

	"github.com/sgao19/erp-go/services/procurement/internal/app"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestQualityTaskPurchaseRemarkIsSeparateAndJSONVisible(t *testing.T) {
	q := app.QualityTask{Remark: "质检申请备注", PurchaseOrderRemark: "采购备注\n按合同区分工厂 <b>原文</b>"}
	proto := qualityTaskProto(q)
	if proto.GetRemark() != q.Remark || proto.GetPurchaseOrderRemark() != q.PurchaseOrderRemark {
		t.Fatalf("remarks lost or merged: %+v", proto)
	}
	raw, err := protojson.Marshal(proto)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err = json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["purchaseOrderRemark"] != q.PurchaseOrderRemark || wire["remark"] != q.Remark {
		t.Fatalf("wrong public JSON fields: %s", raw)
	}
}
