package httpapi

import (
	"net/http/httptest"
	"testing"
)

// 排序栏点下去，网关得把列和方向都递到服务层。丢了任何一个，屏幕上的样子
// 是「点了没反应」——和按日期排一模一样，所以这里钉死。
func TestBothMailboxListsForwardTheirSort(t *testing.T) {
	rec := &recorder{}
	s := &Server{Emails: rec}

	s.listInbound(httptest.NewRecorder(),
		httptest.NewRequest("GET", "/api/inbound-mails?sort_by=size&sort_dir=asc", nil))
	if rec.inbound.GetSortBy() != "size" || rec.inbound.GetSortDir() != "asc" {
		t.Fatalf("收件箱: sort_by=%q sort_dir=%q", rec.inbound.GetSortBy(), rec.inbound.GetSortDir())
	}

	s.listMailboxSent(httptest.NewRecorder(),
		httptest.NewRequest("GET", "/api/mailbox-sent?sort_by=to&sort_dir=desc", nil))
	if rec.sent.GetSortBy() != "to" || rec.sent.GetSortDir() != "desc" {
		t.Fatalf("已发送: sort_by=%q sort_dir=%q", rec.sent.GetSortBy(), rec.sent.GetSortDir())
	}
}

// 搜索的范围由**令牌**划，不由调用方说了算。
//
// 这条从前是「只搜令牌里那一个箱」，现在是「手上开着的那些箱」——变的是
// 范围，没变的是范围从哪儿来。这里钉住的是没变的那一半：当前这把令牌开的
// 箱一定在范围里，且请求参数里随口写的箱号进不来。
//
// 报上来的额外令牌要真的核过才算数，那一半在 mailunlock_search_test.go：
// 这里没有 Redis，s.Unlock 是空的。
func TestSearchScopeComesFromTheToken(t *testing.T) {
	rec := &recorder{}
	s := &Server{Emails: rec}
	req := httptest.NewRequest("GET", "/api/mail-search?keyword=steel&account_id=99", nil)
	req = req.WithContext(withUnlockedAccount(req.Context(), 7))
	s.searchMail(httptest.NewRecorder(), req)
	got := rec.search.GetAccountIds()
	if len(got) != 1 || got[0] != 7 {
		t.Fatalf("搜索的范围应该是令牌开的那个箱：account_ids=%v", got)
	}
}
