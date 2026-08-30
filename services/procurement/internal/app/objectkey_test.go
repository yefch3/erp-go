package app

import "testing"

// 这道检查是共用一个存储桶时唯一的跨租户隔离，值得有一组不依赖数据库的
// 快测钉着——三处直传（银行对账单、发票扫描件、对账页凭证）共用它。
func TestObjectKeyBelongsTo(t *testing.T) {
	const prefix = "po-recon/7/42/"
	for _, c := range []struct {
		name string
		key  string
		want bool
	}{
		{"自己签出来的", prefix + "deadbeef-a.pdf", true},
		{"文件名里带点也无辜", prefix + "deadbeef-..pdf", true},
		{"文件名里带两个点的目录名", prefix + "a..b/c.pdf", true},
		{"空的", "", false},
		{"别的租户", "po-recon/9/42/x.pdf", false},
		{"同租户别的单", "po-recon/7/43/x.pdf", false},
		{"别的模块", "supplier-invoices/7/42/x.pdf", false},
		// 前缀对得上、却爬出了自己的目录。在 MinIO 里 ".." 只是普通字符，
		// 单看存储不构成越权；但预签名地址把 key 放在 URL 的 path 里，
		// 中间任何一个会规范化路径的环节都会让这个结论失效。
		{"爬出去", prefix + "../../9/x.pdf", false},
		{"藏在中间的爬出去", prefix + "a/../../../9/x.pdf", false},
		{"单点也不收", prefix + "./x.pdf", false},
		// 前缀只差一个斜杠：不能因为 HasPrefix 就把 42999 当成 42。
		{"相邻的单号", "po-recon/7/42999/x.pdf", false},
	} {
		if got := objectKeyBelongsTo(c.key, prefix); got != c.want {
			t.Errorf("%s：objectKeyBelongsTo(%q) = %v，要 %v", c.name, c.key, got, c.want)
		}
	}
}
