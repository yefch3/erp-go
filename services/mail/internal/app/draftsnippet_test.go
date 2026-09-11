package app

import "testing"

// 草稿箱列表第三行那句话。
//
// 它读的是 left(body, 8000)——正文的开头，不是全文——所以这里连着「只给了
// 开头」这件事一起测：截断处正好落在标签中间是常态，摘要不能因此吐出半个标签。
func TestDraftSnippet(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		format string
		want   string
	}{
		{
			name:   "纯文本原样，连续空白压成一个空格",
			body:   "报价\n\n单价 3.5 美元\t含运费",
			format: "TEXT",
			want:   "报价 单价 3.5 美元 含运费",
		},
		{
			name:   "HTML 剥成文字",
			body:   "<p>您好，</p><p>附上<b>最新</b>报价。</p>",
			format: "HTML",
			want:   "您好， 附上最新报价。",
		},
		{
			// 只剥标签的话，摘要开头会是一整段 CSS——而人想看的是第一句话。
			name:   "样式表的内容一起去掉，不是只去标签",
			body:   "<style>.x{color:red;font-size:14px}</style><p>报价已更新</p>",
			format: "HTML",
			want:   "报价已更新",
		},
		{
			name:   "空正文给空串，不给一段空白",
			body:   "   \n\t ",
			format: "HTML",
			want:   "",
		},
		{
			// left(body, 8000) 会在任意位置切断。半个标签不能变成半行文字。
			name:   "正文被截断在标签中间，不吐出残标签",
			body:   "<p>先说结论：这批可以做</p><div style=\"colo",
			format: "HTML",
			want:   "先说结论：这批可以做",
		},
		{
			// 格式列写着 HTML 之外的任何东西都当纯文本，和 normalizeFormat 一致。
			name:   "认不得的格式当纯文本",
			body:   "<p>标签本身就是内容</p>",
			format: "",
			want:   "<p>标签本身就是内容</p>",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := draftSnippet(c.body, c.format); got != c.want {
				t.Errorf("draftSnippet(%q, %q)\n got %q\nwant %q", c.body, c.format, got, c.want)
			}
		})
	}
}

// 摘要有上限。一封长草稿的列表行不该把整篇搬进那一行——列表按行等高排的，
// 一行摘要撑不开也读不完。
func TestDraftSnippetTruncates(t *testing.T) {
	long := ""
	for len(long) < 3000 {
		long += "报价明细一条又一条，"
	}
	got := draftSnippet(long, "TEXT")
	if n := len([]rune(got)); n > 201 {
		t.Errorf("摘要 %d 个字，太长了", n)
	}
	if len(got) == 0 {
		t.Fatal("摘要不该是空的")
	}
}
