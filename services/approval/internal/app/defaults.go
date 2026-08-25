package app

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/approval/internal/store"
)

// defaultFlows 是每家公司开张即有的审批流，形状取自给第一家公司播种的迁移
// （00001+00003 的合同、00005 的采购单、00006 的采购单差异确认）。
//
// 有一处**故意**和迁移不同：迁移里合同那条最初写的是「角色 1」，因为当时
// iam 只 bootstrap 出一个角色。角色编号是各家公司自己的，1 号角色属于第一家
// ——照抄过来，第二家的合同审批会指向一个不属于它的角色。00003 已经把第一家
// 的合同节点改成了「按汇报线往上找」，那才是可以搬家的形状：它问的是「提交
// 人的上级」，不需要任何一家公司预先存在某个编号的角色。
//
// 两处仍须一致：迁移是第一家的事实，这张表是其余每家的事实。改任何一边都要
// 同时改另一边——不一致的症状是「新公司的单子走了一条和老公司不同的审批路
// 线」，而审批路线决定谁为一笔钱负责。
var defaultFlows = map[string][]defaultFlow{
	"CONTRACT": {{
		Name:      "出口合同审批",
		MinAmount: "0",
		Nodes: []defaultNode{
			{Seq: 1, Name: "直属上级审批", Type: "MANAGER", Ref: 1},
			{Seq: 2, Name: "上级的上级审批", Type: "MANAGER", Ref: 2},
		},
	}},
	// 采购单按金额分档：五百块的补货和五十万的承诺不该要同一套签字。
	"PURCHASE_ORDER": {{
		Name:      "采购单审批",
		MinAmount: "0",
		Nodes: []defaultNode{
			{Seq: 1, Name: "直属上级审批", Type: "MANAGER", Ref: 1},
		},
	}, {
		Name:      "采购单审批（大额）",
		MinAmount: "50000",
		Nodes: []defaultNode{
			{Seq: 1, Name: "直属上级审批", Type: "MANAGER", Ref: 1},
			{Seq: 2, Name: "上级的上级审批", Type: "MANAGER", Ref: 2},
		},
	}},
	"PURCHASE_ORDER_CHANGE": {{
		Name:      "采购单供应商差异确认审批",
		MinAmount: "0",
		Nodes: []defaultNode{
			{Seq: 1, Name: "直属上级审批", Type: "MANAGER", Ref: 1},
		},
	}},
}

type defaultFlow struct {
	Name string
	// 档位下限，字符串因为存的是 numeric：金额不走 float。
	MinAmount string
	Nodes     []defaultNode
}

type defaultNode struct {
	Seq int32
	// MANAGER 时 Ref 是「往上第几级」，不是角色编号——这正是它能搬家的原因。
	Name string
	Type string
	Ref  int64
}

// seedDefaultFlows 给「还没被播过种」的 (公司, 单据类型) 补上默认审批流。
//
// 审批流的种子全部写死 tenant_id = 1（00001/00005/00006），第二家起的公司一
// 条都没有：第一份合同、第一张采购单，提交时全部撞在「该单据类型未配置审批
// 流」上。这和编码规则（#222）、邮件后台只服务第一家（#216）是同一个病——
// 按第一家公司的形状写的种子，被当成了所有公司的地基。
//
// 修在读的这一侧而不是开户那一侧，理由和编码规则一样：开户在 iam、审批流在
// 这里，跨服务播种会给每条开户路径各埋一次「忘了调」。
//
// 只在**一条都没有**时播种。有人把流程停用或删掉，是他特意做的决定，不是没
// 播过种；把它复活回来比一开始就没有更坏。
func (s *Service) seedDefaultFlows(ctx context.Context, tenantID int64, bizType string) error {
	flows, known := defaultFlows[bizType]
	if !known {
		return nil // 不认识的单据类型：调用方拿回原来的「未配置审批流」。
	}
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		n, err := q.CountDefinitionsFor(ctx, store.CountDefinitionsForParams{
			TenantID: tenantID, BizType: bizType,
		})
		if err != nil {
			return fmt.Errorf("approval: count definitions: %w", err)
		}
		if n > 0 {
			return nil
		}
		for _, f := range flows {
			if _, err := q.SeedDefinition(ctx, store.SeedDefinitionParams{
				TenantID: tenantID, BizType: bizType, Name: f.Name, MinAmount: f.MinAmount,
			}); err != nil {
				return fmt.Errorf("approval: seed definition %s: %w", f.Name, err)
			}
			// 写完必须再读：并发时插入被 DO NOTHING 掉的那一方，读到的才是
			// 真正生效的那条定义，节点必须挂在它身上。
			defID, err := q.GetDefinitionBand(ctx, store.GetDefinitionBandParams{
				TenantID: tenantID, BizType: bizType, MinAmount: f.MinAmount,
			})
			if err != nil {
				return fmt.Errorf("approval: read seeded definition %s: %w", f.Name, err)
			}
			for _, node := range f.Nodes {
				if _, err := q.SeedNode(ctx, store.SeedNodeParams{
					TenantID: tenantID, DefinitionID: defID, Seq: node.Seq, Name: node.Name,
					ApproverType: node.Type, ApproverRef: node.Ref, ApproveMode: "ANY",
				}); err != nil {
					return fmt.Errorf("approval: seed node %s: %w", node.Name, err)
				}
			}
		}
		return nil
	})
}
