package app

import (
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
)

func TestListSortFillsDefaultsAndRejectsWhatItDoesNotKnow(t *testing.T) {
	cases := []struct {
		name    string
		in      ListSort
		allowed map[string]bool
		want    ListSort
		wantErr string
	}{
		{"空 = 日期倒序", ListSort{}, inboundSortColumns, ListSort{By: "date", Dir: "desc"}, ""},
		{"大小默认大的在前", ListSort{By: "size"}, inboundSortColumns, ListSort{By: "size", Dir: "desc"}, ""},
		{"主题默认 A 到 Z", ListSort{By: "subject"}, inboundSortColumns, ListSort{By: "subject", Dir: "asc"}, ""},
		{"收件人在收件箱那侧不认", ListSort{By: "to"}, inboundSortColumns, ListSort{}, "MAIL_SORT_INVALID"},
		{"发件人在已发送那侧不认", ListSort{By: "from"}, sentSortColumns, ListSort{}, "MAIL_SORT_INVALID"},
		{"方向拼错", ListSort{By: "date", Dir: "down"}, inboundSortColumns, ListSort{}, "MAIL_SORT_INVALID"},
		{"给全了照收", ListSort{By: "to", Dir: "desc"}, sentSortColumns, ListSort{By: "to", Dir: "desc"}, ""},
		// 分档跟着过，不被补默认值那一段吃掉。
		{"分档原样带过去", ListSort{StarFirst: true, UnreadFirst: true}, inboundSortColumns,
			ListSort{By: "date", Dir: "desc", StarFirst: true, UnreadFirst: true}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := normalizeListSort(c.in, c.allowed)
			if c.wantErr != "" {
				if apierr.CodeFromError(err) != c.wantErr {
					t.Fatalf("want %s, got err=%v", c.wantErr, err)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("got %+v err=%v, want %+v", got, err, c.want)
			}
		})
	}
}

func TestSortCursorRoundTripsAndRefusesAnotherSortsCursor(t *testing.T) {
	sort := ListSort{By: "subject", Dir: "asc"}
	// 排序键是自由文本：带冒号、带中文、带空格都得原样回来。
	key := "re: [update rfq] hrfp galva: 80 mt 询价"
	cur := encodeSortCursor(sort, key, 42)

	gotKey, gotID, err := decodeSortCursor(cur, sort)
	if err != nil || gotKey == nil || *gotKey != key || gotID != 42 {
		t.Fatalf("round trip broke: key=%v id=%d err=%v", gotKey, gotID, err)
	}
	// 拿「按主题」的游标去翻「按大小」的列表：翻出来的东西没有任何报错但
	// 也没有任何意义，所以要当场拒绝。
	if _, _, err := decodeSortCursor(cur, ListSort{By: "size", Dir: "asc"}); apierr.CodeFromError(err) == "" {
		t.Fatal("a cursor from another sort was accepted")
	}
	if _, _, err := decodeSortCursor(cur, ListSort{By: "subject", Dir: "desc"}); apierr.CodeFromError(err) == "" {
		t.Fatal("a cursor from the other direction was accepted")
	}
	if k, id, err := decodeSortCursor("", sort); err != nil || k != nil || id != 0 {
		t.Fatalf("empty cursor must be the first page, got %v %d %v", k, id, err)
	}
	if _, _, err := decodeSortCursor("not-base64!", sort); err == nil {
		t.Fatal("garbage decoded")
	}
}

func TestSentCursorStillAcceptsTheShapeItHadBeforeSorting(t *testing.T) {
	// 改版前发出去的游标：时间:kind:id。只可能出现在日期倒序的列表上。
	at := time.Date(2026, 9, 1, 12, 34, 56, 789000, time.UTC)
	legacy := encodeSentCursor(at, "HOST", 7)

	key, kind, id, err := decodeSentSortCursor(legacy, ListSort{By: "date", Dir: "desc"})
	if err != nil || key == nil {
		t.Fatalf("legacy cursor rejected: %v", err)
	}
	// 和 SQL 里 to_char(..., 'YYYYMMDDHH24MISSUS') 逐字一致——差一位，
	// 第二页就从错的地方开始。
	if *key != "20260901123456000789" || kind != "HOST" || id != 7 {
		t.Fatalf("legacy cursor converted wrong: key=%q kind=%q id=%d", *key, kind, id)
	}
	// 旧游标配一个不是日期倒序的排序：不可能是我们发出去的组合。
	if _, _, _, err := decodeSentSortCursor(legacy, ListSort{By: "to", Dir: "asc"}); err == nil {
		t.Fatal("legacy cursor accepted for a sort it cannot have come from")
	}

	// 新形状来回走一遍，kind 和带冒号的键都要在。
	sort := ListSort{By: "to", Dir: "asc"}
	cur := encodeSentSortCursor(sort, "buyer:x@overseas.com", "ERP", 99)
	key, kind, id, err = decodeSentSortCursor(cur, sort)
	if err != nil || key == nil || *key != "buyer:x@overseas.com" || kind != "ERP" || id != 99 {
		t.Fatalf("new cursor round trip: key=%v kind=%q id=%d err=%v", key, kind, id, err)
	}
}

func TestDateSortKeyIsTwentyDigitsOfUTC(t *testing.T) {
	// 东八区的下午三点，键里写的是 UTC 的七点。
	cst := time.FixedZone("CST", 8*3600)
	got := dateSortKey(time.Date(2026, 9, 1, 15, 0, 0, 5000, cst))
	if got != "20260901070000000005" {
		t.Fatalf("got %q", got)
	}
	if len(got) != 20 {
		t.Fatalf("key must be fixed width for lexical order to be time order, got %d", len(got))
	}
}

// 开着分档就不是「一直以来的顺序」了：那条靠索引直接读前二十五行的查询
// 按 last_at 读，读不出分档。这一句要是漏了，开关点了没反应。
func TestTopFlagsAreNotTheDefaultSort(t *testing.T) {
	if !(ListSort{By: "date", Dir: "desc"}).isDefault() {
		t.Fatal("日期倒序、不分档，就是默认")
	}
	for _, s := range []ListSort{
		{By: "date", Dir: "desc", StarFirst: true},
		{By: "date", Dir: "desc", UnreadFirst: true},
		{By: "date", Dir: "desc", StarFirst: true, UnreadFirst: true},
	} {
		if s.isDefault() {
			t.Fatalf("%+v 开着分档，不能走靠索引那条查询", s)
		}
	}
	// 已发送没有星标也没有未读，开关得脱干净。
	if got := (ListSort{By: "date", Dir: "desc", StarFirst: true, UnreadFirst: true}).withoutTop(); got.topKey() != "" {
		t.Fatalf("已发送那一侧不该带分档：%+v", got)
	}
}
