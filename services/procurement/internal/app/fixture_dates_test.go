package app

import "time"

// validFor 给测试夹具一个「从今天起 days 天后」的到期日（YYYY-MM-DD）。
//
// 写死的日期是定时炸弹：pkg/pgdb 给每个会话设的是 Asia/Shanghai，库里的
// current_date 跟着上海走，到期那天 UTC 16:00 一过 valid_until>=current_date
// 就不成立，测试在没有任何代码改动的情况下变红。days 留 30 天余量，
// 所以按 UTC 还是按上海算「今天」差那一天无关紧要。
func validFor(days int) string {
	return time.Now().UTC().AddDate(0, 0, days).Format("2006-01-02")
}
