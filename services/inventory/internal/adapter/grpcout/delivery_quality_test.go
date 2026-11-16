package grpcout

import (
	"context"
	"errors"
	"reflect"
	"testing"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"google.golang.org/grpc"
)

type deliveryQualityClientStub struct {
	prv1.PurchaseOrderServiceClient
	result  *prv1.CheckDeliveryQualityResponse
	err     error
	request *prv1.CheckDeliveryQualityRequest
}

func (c *deliveryQualityClientStub) CheckDeliveryQuality(_ context.Context, req *prv1.CheckDeliveryQualityRequest, _ ...grpc.CallOption) (*prv1.CheckDeliveryQualityResponse, error) {
	c.request = req
	return c.result, c.err
}

func TestDeliveryQualityClientFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name   string
		result *prv1.CheckDeliveryQualityResponse
		err    error
		code   string
	}{
		{"passed", &prv1.CheckDeliveryQualityResponse{Allowed: true}, nil, ""},
		{"not inspected", &prv1.CheckDeliveryQualityResponse{Code: "QUALITY_NOT_INSPECTED", Reason: "货物未质检"}, nil, "QUALITY_NOT_INSPECTED"},
		{"missing response", nil, nil, "IV_QUALITY_NOT_PASSED"},
		{"empty refusal", &prv1.CheckDeliveryQualityResponse{}, nil, "IV_QUALITY_NOT_PASSED"},
		{"rpc failure", nil, errors.New("unavailable"), "IV_QUALITY_UNAVAILABLE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &deliveryQualityClientStub{result: test.result, err: test.err}
			guard := &DeliveryQuality{client: client}
			err := guard.Check(context.Background(), []int64{7, 8})
			if apierr.CodeFromError(err) != test.code || (test.code == "" && err != nil) {
				t.Fatalf("unexpected result: %v", err)
			}
			if !reflect.DeepEqual(client.request.GetPoIds(), []int64{7, 8}) || client.request.GetContractId() != 0 {
				t.Fatalf("wrong evidence request: %v", client.request)
			}
		})
	}
	client := &deliveryQualityClientStub{}
	if err := (&DeliveryQuality{client: client}).Check(context.Background(), nil); apierr.CodeFromError(err) != "IV_QUALITY_SOURCE_REQUIRED" || client.request != nil {
		t.Fatalf("empty source was not blocked locally: %v", err)
	}
}
