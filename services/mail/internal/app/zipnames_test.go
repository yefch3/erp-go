package app

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestZipEntryNamesKeepEveryAttachmentDistinct(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			// 生产上 186 封信是这样的。zip 允许重名，解压时后一个盖掉前一个，
			// 十个附件解出来只有八个，一声不响。
			"重名的加序号，不是覆盖",
			[]string{"报价单.xlsx", "报价单.xlsx", "报价单.xlsx"},
			[]string{"报价单.xlsx", "报价单 (2).xlsx", "报价单 (3).xlsx"},
		},
		{
			// 序号要加在扩展名前面，不然双击打不开。
			"序号加在扩展名前",
			[]string{"a.tar.gz", "a.tar.gz"},
			[]string{"a.tar.gz", "a.tar (2).gz"},
		},
		{
			"大小写不同也算重名（Windows 和 macOS 解压时会撞）",
			[]string{"Quote.PDF", "quote.pdf"},
			[]string{"Quote.PDF", "quote (2).pdf"},
		},
		{
			"路径一律剥掉，只留最后一段",
			[]string{"../../etc/passwd", `C:\Users\x\报价.doc`, "/tmp/a.txt"},
			[]string{"passwd", "报价.doc", "a.txt"},
		},
		{
			"没名字的给一个位置名，而不是空条目",
			[]string{"", "   ", "..", "."},
			[]string{"附件-1", "附件-2", "附件-3", "附件-4"},
		},
		{
			"没有扩展名的也能加序号",
			[]string{"README", "README"},
			[]string{"README", "README (2)"},
		},
		{
			"控制字符去掉",
			[]string{"a\x00b\x1fc.txt"},
			[]string{"abc.txt"},
		},
		{"空列表", nil, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := zipEntryNames(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}

// 加了序号之后不能又和别的名字撞上。
func TestZipEntryNamesDoNotCollideAfterNumbering(t *testing.T) {
	got := zipEntryNames([]string{"a.pdf", "a (2).pdf", "a.pdf", "a.pdf"})
	seen := map[string]bool{}
	for _, n := range got {
		if seen[n] {
			t.Fatalf("加完序号还是撞了：%q", got)
		}
		seen[n] = true
	}
	if len(got) != 4 {
		t.Fatalf("条目数变了：%q", got)
	}
}

func TestBundleFileNameIsRecognisableOnADesktop(t *testing.T) {
	cases := []struct{ subject, want string }{
		{"Steel Request July 2026", "Steel Request July 2026-附件.zip"},
		// 主题也来自发件人，同样要剥路径。
		{"../../etc/passwd", "passwd-附件.zip"},
		// 没主题的信是有的：用邮件号，至少唯一。
		{"", "邮件-17312-附件.zip"},
		{"   ", "邮件-17312-附件.zip"},
	}
	for _, c := range cases {
		if got := bundleFileName(c.subject, 17312); got != c.want {
			t.Errorf("bundleFileName(%q) = %q, want %q", c.subject, got, c.want)
		}
	}
}

// 长主题按字符截，不按字节——按字节截会把汉字切成半个，名字尾巴是乱码。
func TestALongChineseSubjectIsNotCutMidCharacter(t *testing.T) {
	long := ""
	for i := 0; i < 80; i++ {
		long += "报"
	}
	got := bundleFileName(long, 1)
	if !utf8.ValidString(got) {
		t.Fatalf("截出了非法 UTF-8：%q", got)
	}
	name := strings.TrimSuffix(got, "-附件.zip")
	if n := utf8.RuneCountInString(name); n != 60 {
		t.Errorf("应该正好留 60 个字，实际 %d", n)
	}
}
