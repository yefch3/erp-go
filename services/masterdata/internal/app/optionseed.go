package app

import (
	"context"
	"fmt"

	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

// defaultOptions 是每家公司开张即有的下拉字典，逐行取自给第一家公司播种的
// 迁移（00001 的付款方式/贸易术语/发运异常，00008 的客户类型与来源，00009
// 的客户负责人职责，00015 的供应商与工厂相关）。
//
// 两处必须一致：迁移是第一家的事实，这张表是其余每家的事实。改任何一边都要
// 同时改另一边——不一致的症状是「两家公司的付款方式选项对不上」，而付款方式
// 印在发给客户的合同上。
//
// 有序切片而不是 map：播种顺序确定，出问题时两次运行看到的是同一个现场。
var defaultOptions = []struct {
	Category string
	Items    []defaultOption
}{
	{"PAYMENT_METHOD", []defaultOption{
		{"TT", "电汇 T/T", 1},
		{"LC", "信用证 L/C", 2},
		{"DP", "付款交单 D/P", 3},
		{"DA", "承兑交单 D/A", 4},
	}},
	{"TRADE_TERM", []defaultOption{
		{"FOB", "FOB 离岸价", 1},
		{"CIF", "CIF 到岸价", 2},
		{"CFR", "CFR 成本加运费", 3},
		{"EXW", "EXW 工厂交货", 4},
		{"DDP", "DDP 完税后交货", 5},
	}},
	{"SHIPPING_EXCEPTION", []defaultOption{
		{"DELAY", "延误", 1},
		{"ROLLOVER", "甩柜", 2},
		{"DOC_MISSING", "单证缺失", 3},
	}},
	{"CUSTOMER_TYPE", []defaultOption{
		{"IMPORTER", "进口商", 1},
		{"DISTRIBUTOR", "经销商", 2},
		{"END_CUSTOMER", "终端客户", 3},
		{"AGENT", "代理商", 4},
		{"OTHER", "其他", 5},
	}},
	{"CUSTOMER_SOURCE", []defaultOption{
		{"REFERRAL", "客户转介绍", 1},
		{"EXHIBITION", "展会", 2},
		{"WEBSITE", "网站", 3},
		{"OUTREACH", "主动开发", 4},
		{"OTHER", "其他", 5},
	}},
	{"CUSTOMER_OWNER_RESPONSIBILITY", []defaultOption{
		{"SALES", "销售", 1},
		{"FOLLOW_UP", "跟单", 2},
		{"DOCUMENT", "单证", 3},
		{"FINANCE", "财务", 4},
		{"CUSTOMER_SERVICE", "客户服务", 5},
	}},
	{"SUPPLIER_BUSINESS_TYPE", []defaultOption{
		{"GENERAL", "产品/材料供应商", 1},
		{"CARRIER", "船公司/承运人", 2},
		{"FORWARDER", "国际货运代理", 3},
		{"CUSTOMS_BROKER", "报关服务商", 4},
		{"WAREHOUSE", "仓储服务商", 5},
		{"SERVICE", "其他专业服务商", 6},
	}},
	{"SUPPLIER_OWNER_RESPONSIBILITY", []defaultOption{
		{"PROCUREMENT", "采购", 1},
		{"FOLLOW_UP", "跟单", 2},
		{"FINANCE", "财务", 3},
		{"LOGISTICS", "物流", 4},
	}},
	{"FACTORY_OWNER_RESPONSIBILITY", []defaultOption{
		{"PROCUREMENT", "采购", 1},
		{"FOLLOW_UP", "跟单", 2},
		{"QUALITY", "质检", 3},
		{"LOGISTICS", "物流", 4},
	}},
}

type defaultOption struct {
	Code      string
	Label     string
	SortOrder int32
}

// seedDefaultOptions 给「还没被播过种」的类别补上默认选项。
//
// 下拉字典的种子全部写死 tenant_id = 1（00001/00008/00009/00015），第二家起
// 的公司一个选项都没有：付款方式、贸易术语、客户类型、供应商类型……每一个下
// 拉框都是空的。和编码规则（#222）、审批流（#223）是同一个病，只是这次不报
// 错——空下拉框不会说自己为什么空，人只会以为是自己没找对地方。
//
// 修在读的这一侧，理由同前：开户在 iam、字典在这里，跨服务播种会给每条开户
// 路径各埋一次「忘了调」。
//
// category 为空表示「把所有类别都看一遍」，那是 ListOptions 不带筛选时的形状。
//
// 只补**一条都没有**的类别。选项可以被停用，停用是有人做过的决定；把它复活
// 回来，等于一个被人特意去掉的付款方式又出现在合同上。
// 返回是否真的写进去了，好让调用方决定要不要再读一遍——什么都没补的时候
// 重读一次，是为一个已知答案多跑一趟。
func (s *Service) seedDefaultOptions(ctx context.Context, tenantID int64, category string) (bool, error) {
	present, err := s.q.OptionCategoriesOf(ctx, tenantID)
	if err != nil {
		return false, fmt.Errorf("masterdata: read option categories: %w", err)
	}
	seeded := make(map[string]bool, len(present))
	for _, c := range present {
		seeded[c] = true
	}
	inserted := false
	for _, def := range defaultOptions {
		if category != "" && def.Category != category {
			continue
		}
		if seeded[def.Category] {
			continue
		}
		for _, it := range def.Items {
			n, err := s.q.InsertOptionIfAbsent(ctx, store.InsertOptionIfAbsentParams{
				TenantID: tenantID, Category: def.Category,
				Code: it.Code, Label: it.Label, SortOrder: it.SortOrder,
			})
			if err != nil {
				return inserted, fmt.Errorf("masterdata: seed option %s/%s: %w", def.Category, it.Code, err)
			}
			if n > 0 {
				inserted = true
			}
		}
	}
	return inserted, nil
}
