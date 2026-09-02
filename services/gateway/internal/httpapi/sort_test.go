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

// 搜索也只搜令牌里那个箱：网关得把它递到服务层，和收件箱、已发送一样。
func TestSearchForwardsTheUnlockedMailbox(t *testing.T) {
	rec := &recorder{}
	s := &Server{Emails: rec}
	req := httptest.NewRequest("GET", "/api/mail-search?keyword=steel", nil)
	req = req.WithContext(withUnlockedAccount(req.Context(), 7))
	s.searchMail(httptest.NewRecorder(), req)
	if rec.search.GetAccountId() != 7 {
		t.Fatalf("搜索没带上解锁的信箱：account_id=%d，于是站在 A 箱里能搜出 B 箱的信", rec.search.GetAccountId())
	}
}
