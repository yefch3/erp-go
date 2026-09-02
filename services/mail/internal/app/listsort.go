package app

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
)

// 列表按哪一列排、往哪个方向排。
//
// 一个人在 263 的网页邮箱里习惯了点「大小」把最占地方的信找出来、点
// 「发件人」把同一个客户的信拢到一起；这里给的就是那五个表头里在我们
// 这份列表上说得通的四个。收件箱那一侧是发件人，已发送那一侧是收件人——
// 同一个位置，问的是不同的问题。
type ListSort struct {
	// By：from | to | subject | date | size。空 = date。
	By string
	// Dir：asc | desc。空 = 该列的自然方向（日期和大小新的/大的在前，文本
	// 从 A 到 Z）。
	Dir string
}

var (
	inboundSortColumns = map[string]bool{"from": true, "subject": true, "date": true, "size": true}
	sentSortColumns    = map[string]bool{"to": true, "subject": true, "date": true, "size": true}
)

// isDefault 说明这就是一直以来的顺序：日期倒序。收件箱在这一档走的是靠
// 索引直接读出前二十五行的那条查询；其余任何一档都要把整个箱排一遍。
func (s ListSort) isDefault() bool {
	return s.By == "date" && s.Dir == "desc"
}

// normalizeListSort 把空值补成默认、把不认识的值挡在门外。
//
// 不认识的列**报错**而不是退回按日期排。退回的样子是「点了没反应」——和
// 「按日期排」在屏幕上一模一样，于是一个拼错的参数名可以在前端里活很久。
func normalizeListSort(s ListSort, allowed map[string]bool) (ListSort, error) {
	if s.By == "" {
		s.By = "date"
	}
	if !allowed[s.By] {
		return ListSort{}, apierr.Invalid("MAIL_SORT_INVALID", "这份列表不支持按「"+s.By+"」排序")
	}
	if s.Dir == "" {
		if s.By == "date" || s.By == "size" {
			s.Dir = "desc"
		} else {
			s.Dir = "asc"
		}
	}
	if s.Dir != "asc" && s.Dir != "desc" {
		return ListSort{}, apierr.Invalid("MAIL_SORT_INVALID", "排序方向只能是 asc 或 desc")
	}
	return s, nil
}

func errSortNotWithKeyword() error {
	return apierr.Invalid("MAIL_SORT_NOT_WITH_KEYWORD", "搜索结果按时间排，不支持再按列排序")
}

// 排序版列表的游标：上一页最后一行的 (列, 方向, [kind,] id, 排序键)。
//
// 列和方向也编进去，解的时候要对得上请求里的：拿「按大小」的游标去翻
// 「按主题」的列表，翻出来的东西没有任何报错但也没有任何意义。前端换排序
// 时会清掉游标，所以对不上只可能是手写的地址。
//
// 排序键放最后，因为它是主题或发件人这种带冒号的自由文本，只有最后一段
// 才能不受分隔符的约束。
const sortCursorTag = "s1"

func encodeSortCursor(sort ListSort, key string, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(
		sortCursorTag + ":" + sort.By + ":" + sort.Dir + ":" + strconv.FormatInt(id, 10) + ":" + key))
}

// decodeSortCursor 解收件箱排序版的游标。空游标是第一页：key 为 nil。
func decodeSortCursor(cursor string, sort ListSort) (*string, int64, error) {
	if cursor == "" {
		return nil, 0, nil
	}
	parts, err := sortCursorParts(cursor, 5)
	if err != nil {
		return nil, 0, err
	}
	if parts[1] != sort.By || parts[2] != sort.Dir {
		return nil, 0, errBadCursor()
	}
	id, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return nil, 0, errBadCursor()
	}
	key := parts[4]
	return &key, id, nil
}

// 已发送多一段 kind：两条腿各自编号，(排序键, id) 不是唯一位置。
func encodeSentSortCursor(sort ListSort, key, kind string, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(
		sortCursorTag + ":" + sort.By + ":" + sort.Dir + ":" + kind + ":" + strconv.FormatInt(id, 10) + ":" + key))
}

// decodeSentSortCursor 解已发送的游标。
//
// 也认改版之前的样子（时间:kind:id）：那种游标只可能出现在按日期倒序的
// 列表上，把时间换算成日期那一档的排序键就还能接着翻。有人在改版那一刻
// 正翻到第三页，不该因为我们换了游标的形状而被打回第一页。
func decodeSentSortCursor(cursor string, sort ListSort) (key *string, kind string, id int64, err error) {
	if cursor == "" {
		return nil, "", 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, "", 0, errBadCursor()
	}
	if !strings.HasPrefix(string(raw), sortCursorTag+":") {
		at, legacyKind, legacyID, err := decodeSentCursor(cursor)
		if err != nil {
			return nil, "", 0, err
		}
		if !sort.isDefault() {
			return nil, "", 0, errBadCursor()
		}
		k := dateSortKey(at.Time)
		return &k, legacyKind, legacyID, nil
	}
	parts, err := sortCursorParts(cursor, 6)
	if err != nil {
		return nil, "", 0, err
	}
	if parts[1] != sort.By || parts[2] != sort.Dir {
		return nil, "", 0, errBadCursor()
	}
	id, err = strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		return nil, "", 0, errBadCursor()
	}
	k := parts[5]
	return &k, parts[3], id, nil
}

func sortCursorParts(cursor string, n int) ([]string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, errBadCursor()
	}
	parts := strings.SplitN(string(raw), ":", n)
	if len(parts) != n || parts[0] != sortCursorTag {
		return nil, errBadCursor()
	}
	return parts, nil
}

// dateSortKey 是 SQL 里 to_char(at AT TIME ZONE 'UTC', 'YYYYMMDDHH24MISSUS')
// 的 Go 版本：UTC 的 20 位数字串。只在把旧游标换算成新游标时用，两边必须
// 逐字一致——差一位，第二页就从错的地方开始。
func dateSortKey(t time.Time) string {
	u := t.UTC()
	return u.Format("20060102150405") + fmt.Sprintf("%06d", u.Nanosecond()/1000)
}
