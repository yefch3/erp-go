package grpcout

import (
	"strings"
	"testing"
	"time"
)

// 汇率的「什么时候取的」读不懂时，**不能拿「现在」顶上**。
//
// 快照存在的全部意义就是「当时是多少、什么时候取的」。原来这里解析失败会
// 悄悄换成 time.Now()，于是一个可能几天前的汇率在账上看起来是刚取的——
// 事后谁也查不出这张单子是不是按陈旧汇率定的价，**而且看不出这里出过问题**。
//
// 这个测试盯的是那条边界：什么样的输入算读得懂。真正的拒绝路径在 Latest 里，
// 它依赖一个活着的 fx 连接，所以这里只钉解析这一步的判定。
func TestFxFetchedAtParsing(t *testing.T) {
	good := []string{
		"2026-08-27T03:24:16Z",
		"2026-08-27T03:24:16+08:00",
		"2026-08-27T03:24:16.123Z",
	}
	for _, v := range good {
		if _, err := time.Parse(time.RFC3339, v); err != nil {
			t.Fatalf("%q 是 fx 正常会发的格式，应该读得懂: %v", v, err)
		}
	}

	// 这些都必须**读不懂**，而不是被悄悄换成「现在」。
	//
	// 空串那一条最要紧：protobuf 的字符串字段没赋值就是空串，所以
	// 「fx 忘了填」和「fx 填错了」走的是同一条路——两条都该报错。
	bad := []string{
		"",                    // 没填
		"2026-08-27",          // 只有日期，没时间没时区
		"2026-08-27 03:24:16", // 少了 T
		"27/08/2026 03:24",    // 换了个格式
		"not a time",
	}
	for _, v := range bad {
		if _, err := time.Parse(time.RFC3339, v); err == nil {
			t.Fatalf("%q 不该被当成合法时间——顶上一个「现在」会让陈旧汇率看起来新鲜", v)
		}
	}
}

// 拒绝的时候要说清楚是什么读不懂了，否则事后没人知道 fx 发的到底是什么。
func TestFxTimeErrorNamesTheValue(t *testing.T) {
	const junk = "27/08/2026"
	msg := "汇率服务返回的时间读不懂：" + junk
	if !strings.Contains(msg, junk) {
		t.Fatal("报错要把读不懂的那个值带上")
	}
}
