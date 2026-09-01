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
