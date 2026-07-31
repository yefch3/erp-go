// Package grpcout holds inventory's outbound dependencies. Inventory owns
// stock and nothing else; anything it needs from another service it asks for
// at write time rather than reading somebody else's tables.
package grpcout

import (
	"context"

	"google.golang.org/grpc"

	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
)

type Numbering struct{ client mdv1.NumberingServiceClient }

func NewNumbering(conn *grpc.ClientConn) *Numbering {
	return &Numbering{client: mdv1.NewNumberingServiceClient(conn)}
}

func (n *Numbering) Next(ctx context.Context, bizType string) (string, error) {
	resp, err := n.client.NextNumber(ctx, &mdv1.NextNumberRequest{BizType: bizType})
	if err != nil {
		return "", err
	}
	return resp.GetNumber(), nil
}
