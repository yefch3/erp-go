package main

import (
	"context"
	"fmt"
	"os"
	"time"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/masterref"
	"github.com/sgao19/erp-go/services/masterdata/internal/app"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// All business stores participate, including archived records and JSON bodies.
// A missing peer never silently turns into permission to remove a record.
func referenceChecker() (app.ReferenceChecker, func(), error) {
	type peer struct {
		name   string
		client commonv1.MasterDataReferenceServiceClient
	}
	peers := []peer{}
	conns := []*grpc.ClientConn{}
	closeAll := func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}
	for _, spec := range [][3]string{{"EXPORT_ADDR", "localhost:9006", "销售合同"}, {"PROCUREMENT_ADDR", "localhost:9007", "询盘与采购"}, {"SHIPPING_ADDR", "localhost:9009", "船期物流"}, {"INVENTORY_ADDR", "localhost:9008", "仓储"}, {"PRODUCT_ADDR", "localhost:9004", "产品"}, {"MAIL_ADDR", "localhost:9010", "邮件"}, {"APPROVAL_ADDR", "localhost:9005", "审批"}} {
		addr := os.Getenv(spec[0])
		if addr == "" {
			addr = spec[1]
		}
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(grpcx.UnaryClientPropagator()))
		if err != nil {
			closeAll()
			return nil, nil, err
		}
		conns = append(conns, conn)
		peers = append(peers, peer{spec[2], commonv1.NewMasterDataReferenceServiceClient(conn)})
	}
	check := func(ctx context.Context, tenant int64, entity string, candidates []masterref.Candidate) (map[int64]string, error) {
		used := map[int64]string{}
		if len(candidates) == 0 {
			return used, nil
		}
		request := &commonv1.CheckMasterDataReferencesRequest{Entity: entity}
		for _, c := range candidates {
			request.Candidates = append(request.Candidates, &commonv1.MasterDataReferenceCandidate{Id: c.ID, Labels: c.Labels})
		}
		for _, p := range peers {
			child, cancel := context.WithTimeout(ctx, 6*time.Second)
			resp, err := p.client.CheckMasterDataReferences(child, request)
			cancel()
			if err != nil {
				return nil, fmt.Errorf("%s references unavailable: %w", p.name, err)
			}
			for _, id := range resp.ReferencedIds {
				used[id] = "已被业务引用：" + p.name
			}
		}
		return used, nil
	}
	return check, closeAll, nil
}
