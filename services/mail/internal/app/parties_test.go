package app

import (
	"reflect"
	"testing"
)

func TestParsePartiesHandlesTheHeadersRealMailActuallyHas(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []MailParty
	}{
		{"标准的一串", `Ana Maria Gomez <agomez@acerosinka.com>, jgomez@acerosinka.com, allosa@acerosinka.com`,
			[]MailParty{{"Ana Maria Gomez", "agomez@acerosinka.com"}, {"", "jgomez@acerosinka.com"}, {"", "allosa@acerosinka.com"}}},
		{"显示名里带逗号（引号包着）", `"Gomez, Ana" <agomez@x.com>, b@x.com`,
			[]MailParty{{"Gomez, Ana", "agomez@x.com"}, {"", "b@x.com"}}},
		{"带重音的名字", `MARILIN LUDEÑA <importaciones@acerosinka.com>`,
			[]MailParty{{"MARILIN LUDEÑA", "importaciones@acerosinka.com"}}},
		{"Outlook 式的分号", `a@x.com; b@x.com;c@x.com`,
			[]MailParty{{"", "a@x.com"}, {"", "b@x.com"}, {"", "c@x.com"}}},
		{"大写地址小写化、重复去掉", `A@X.com, a@x.com, B@x.com`,
			[]MailParty{{"", "a@x.com"}, {"", "b@x.com"}}},
		{"没引号的中文名（RFC 解不动，松散路）", `张三 <zhang@x.com>, 李四 <li@x.com>`,
			[]MailParty{{"张三", "zhang@x.com"}, {"李四", "li@x.com"}}},
		// 审查找出来的：一个人写得不规范就让整段头走松散路，从前松散路会把
		// "Jane Doe jane@x.com" 整段当成地址。现在从每一段里抠地址。
		{"显示名后面直接跟地址、没有尖括号", `Ana Gomez <agomez@x.com>, Jane Doe jane@x.com, ok@x.com`,
			[]MailParty{{"Ana Gomez", "agomez@x.com"}, {"Jane Doe", "jane@x.com"}, {"", "ok@x.com"}}},
		{"抠不出地址的段直接丢，不编一个", `Ana Gomez <agomez@x.com>, just a name, a@b`,
			[]MailParty{{"Ana Gomez", "agomez@x.com"}}},
		{"空", "   ", nil},
		{"不是地址的东西", `undisclosed-recipients:;`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseParties(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestLooksLikeAddressRefusesWhatWouldBounce(t *testing.T) {
	for _, bad := range []string{"", "jane doe jane@x.com", "a@b", "@x.com", "a@@x.com", "<a@x.com>", "a@x.com b@x.com"} {
		if looksLikeAddress(bad) {
			t.Errorf("%q 不该当成地址", bad)
		}
	}
	for _, ok := range []string{"a@x.com", "first.last+tag@sub.example.co.uk", "importaciones@acerosinka.com"} {
		if !looksLikeAddress(ok) {
			t.Errorf("%q 该当成地址", ok)
		}
	}
}
