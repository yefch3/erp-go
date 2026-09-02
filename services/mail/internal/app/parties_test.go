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
