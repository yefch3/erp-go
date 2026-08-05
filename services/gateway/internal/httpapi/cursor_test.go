package httpapi

import (
	"context"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc"

	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
)

// The gateway is a translator, and the failure mode of a translator is not a
// crash — it is a word silently dropped. This has now happened twice: an
// attachment's download URL went missing because a rebuilt service was talking
// to a gateway compiled against the older proto, and ?cursor= went missing
// because it was added to the proto, the service and the browser but not to
// the handler that turns a query string into a request. Both compiled. Both
// looked like the feature simply did not work.
//
// A cursor dropped here is invisible in the worst way: every page returns the
// first page, so 下一页 changes the address bar and nothing else. These tests
// assert the one thing that cannot be checked by reading — that the value in
// the URL reaches the request.

// recorder stands in for the mail service and keeps the last request it was
// handed. Only the calls under test are implemented; the rest of the interface
// is embedded, so this file does not have to grow every time an RPC is added.
type recorder struct {
	mailv1.EmailServiceClient
	sent      *mailv1.ListMailboxSentRequest
	messages  *mailv1.ListMessagesRequest
	scheduled *mailv1.ListScheduledRequest
	inbound   *mailv1.ListInboundRequest
	search    *mailv1.SearchMailRequest
}

func (r *recorder) SearchMail(_ context.Context, in *mailv1.SearchMailRequest, _ ...grpc.CallOption) (*mailv1.SearchMailResponse, error) {
	r.search = in
	return &mailv1.SearchMailResponse{}, nil
}

func (r *recorder) ListMailboxSent(_ context.Context, in *mailv1.ListMailboxSentRequest, _ ...grpc.CallOption) (*mailv1.ListMailboxSentResponse, error) {
	r.sent = in
	return &mailv1.ListMailboxSentResponse{}, nil
}

func (r *recorder) ListMessages(_ context.Context, in *mailv1.ListMessagesRequest, _ ...grpc.CallOption) (*mailv1.ListMessagesResponse, error) {
	r.messages = in
	return &mailv1.ListMessagesResponse{}, nil
}

func (r *recorder) ListScheduled(_ context.Context, in *mailv1.ListScheduledRequest, _ ...grpc.CallOption) (*mailv1.ListScheduledResponse, error) {
	r.scheduled = in
	return &mailv1.ListScheduledResponse{}, nil
}

func (r *recorder) ListInbound(_ context.Context, in *mailv1.ListInboundRequest, _ ...grpc.CallOption) (*mailv1.ListInboundResponse, error) {
	r.inbound = in
	return &mailv1.ListInboundResponse{}, nil
}

func TestEveryMailboxListForwardsItsCursor(t *testing.T) {
	const want = "Y3Vyc29yLXVuZGVyLXRlc3Q"

	cases := []struct {
		name string
		path string
		call func(*Server, *httptest.ResponseRecorder, string)
		got  func(*recorder) string
	}{
		{
			name: "已发送",
			path: "/api/mailbox-sent?cursor=" + want,
			call: func(s *Server, w *httptest.ResponseRecorder, url string) {
				s.listMailboxSent(w, httptest.NewRequest("GET", url, nil))
			},
			got: func(r *recorder) string { return r.sent.GetCursor() },
		},
		{
			name: "需要处理",
			path: "/api/email-messages?attention_only=true&cursor=" + want,
			call: func(s *Server, w *httptest.ResponseRecorder, url string) {
				s.listEmailMessages(w, httptest.NewRequest("GET", url, nil))
			},
			got: func(r *recorder) string { return r.messages.GetCursor() },
		},
		{
			name: "已定时",
			path: "/api/email-scheduled?cursor=" + want,
			call: func(s *Server, w *httptest.ResponseRecorder, url string) {
				s.listScheduled(w, httptest.NewRequest("GET", url, nil))
			},
			got: func(r *recorder) string { return r.scheduled.GetCursor() },
		},
		{
			name: "搜索",
			path: "/api/mail-search?keyword=x&cursor=" + want,
			call: func(s *Server, w *httptest.ResponseRecorder, url string) {
				s.searchMail(w, httptest.NewRequest("GET", url, nil))
			},
			got: func(r *recorder) string { return r.search.GetCursor() },
		},
		{
			name: "收件箱",
			path: "/api/inbound-mails?cursor=" + want,
			call: func(s *Server, w *httptest.ResponseRecorder, url string) {
				s.listInbound(w, httptest.NewRequest("GET", url, nil))
			},
			got: func(r *recorder) string { return r.inbound.GetCursor() },
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &recorder{}
			s := &Server{Emails: rec}
			c.call(s, httptest.NewRecorder(), c.path)
			if got := c.got(rec); got != want {
				t.Errorf("cursor did not reach the service: got %q, want %q\n"+
					"the handler is dropping ?cursor= from %s, so every page returns the first",
					got, want, c.path)
			}
		})
	}
}
