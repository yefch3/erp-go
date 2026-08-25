package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

// 发出去的信要在邮箱自己的「已发送」里留底。
//
// SMTP 只负责把信交出去，不会在发件人的邮箱里放任何东西。Gmail 自己会补这一
// 步，263 不会——所以 263 账号发完信，ERP 和 263 网页版的已发送都是空的，
// 空得没有任何迹象说明为什么。这组测试守的就是这件事，外加一条反向的：
// 已经自己留底的主机不能再存一份，否则人家的 Gmail 里每封信都变成两封。
type fakeAppendMailbox struct {
	// 只覆盖用得到的两个方法；其余方法真被调用就是这段代码越界了，
	// 让它以 nil 接口的方式当场炸掉，好过悄悄返回零值。
	Mailbox

	sentFolder    string
	sentFolderErr error
	folderAsks    int

	appends   []appendCall
	appendErr error
}

type appendCall struct {
	folder string
	raw    []byte
	at     time.Time
}

func (f *fakeAppendMailbox) SentFolder(context.Context, MailAccount) (string, error) {
	f.folderAsks++
	return f.sentFolder, f.sentFolderErr
}

func (f *fakeAppendMailbox) AppendMessage(_ context.Context, _ MailAccount, folder string, raw []byte, at time.Time) error {
	f.appends = append(f.appends, appendCall{folder: folder, raw: raw, at: at})
	return f.appendErr
}

func newSentCopyService(mb Mailbox) *Service {
	s := New(nil, Deps{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.UseMailbox(mb)
	return s
}

func TestSentCopyIsFiledOnAPlainHost(t *testing.T) {
	mb := &fakeAppendMailbox{sentFolder: "Sent"}
	s := newSentCopyService(mb)
	acct := MailAccount{AccountID: 13, Email: "erptest@263.net", Host: "smtp.263.net"}
	raw := []byte("Message-ID: <a@263.net>\r\nSubject: hi\r\n\r\nbody")

	before := time.Now()
	s.fileSentCopy(context.Background(), acct, raw)

	if len(mb.appends) != 1 {
		t.Fatalf("发出去的信没有在已发送里留底，append 次数 %d", len(mb.appends))
	}
	got := mb.appends[0]
	if got.folder != "Sent" {
		t.Fatalf("存错了文件夹：%q", got.folder)
	}
	if string(got.raw) != string(raw) {
		t.Fatalf("留底的不是发出去的那封信：%q", got.raw)
	}
	if got.at.Before(before) {
		t.Fatalf("留底时间早于发送时间：%v", got.at)
	}
}

func TestSentCopyIsNotFiledWhereTheHostAlreadyKeepsOne(t *testing.T) {
	for _, host := range []string{"smtp.gmail.com", "gmail.com", "SMTP.GOOGLEMAIL.COM"} {
		mb := &fakeAppendMailbox{sentFolder: "[Gmail]/Sent Mail"}
		s := newSentCopyService(mb)
		s.fileSentCopy(context.Background(), MailAccount{AccountID: 1, Host: host}, []byte("x"))

		if len(mb.appends) != 0 {
			t.Fatalf("%s 自己就会留底，再存一份等于每封信在 Gmail 里都变成两封", host)
		}
		// 连问都不该问：跳过的判断不值得为它拨一次号。
		if mb.folderAsks != 0 {
			t.Fatalf("%s：明明要跳过，却还是去找了一次已发送文件夹", host)
		}
	}
}

// notgmail.com 以 gmail.com 结尾。用纯后缀判断，这家公司的每封信都会被
// 当成「Gmail 自己会留底」而不留底——而且没有任何报错。
func TestLookalikeDomainIsNotMistakenForGmail(t *testing.T) {
	mb := &fakeAppendMailbox{sentFolder: "Sent"}
	s := newSentCopyService(mb)
	s.fileSentCopy(context.Background(), MailAccount{Host: "smtp.notgmail.com"}, []byte("x"))

	if len(mb.appends) != 1 {
		t.Fatal("smtp.notgmail.com 被当成了 Gmail，这家公司的已发送会一直是空的")
	}
}

// 留底是尽力而为：信已经发出去了，存不进去不能反过来影响任何东西。
func TestFilingFailuresStayQuiet(t *testing.T) {
	t.Run("找不到已发送文件夹", func(t *testing.T) {
		mb := &fakeAppendMailbox{sentFolderErr: errors.New("no such folder")}
		s := newSentCopyService(mb)
		s.fileSentCopy(context.Background(), MailAccount{AccountID: 2, Host: "mail.example.com"}, []byte("x"))
		if len(mb.appends) != 0 {
			t.Fatal("文件夹都没找到，却往某处存了一封信")
		}
	})

	t.Run("主机拒绝存入", func(t *testing.T) {
		mb := &fakeAppendMailbox{sentFolder: "Sent", appendErr: errors.New("over quota")}
		s := newSentCopyService(mb)
		s.fileSentCopy(context.Background(), MailAccount{AccountID: 3, Host: "mail.example.com"}, []byte("x"))
	})

	t.Run("没有信可存", func(t *testing.T) {
		mb := &fakeAppendMailbox{sentFolder: "Sent"}
		s := newSentCopyService(mb)
		// dev 发送适配器什么都不发，自然也没有原文可留底。
		s.fileSentCopy(context.Background(), MailAccount{AccountID: 4, Host: "mail.example.com"}, nil)
		if len(mb.appends) != 0 || mb.folderAsks != 0 {
			t.Fatal("没有原文却去存了一封空信")
		}
	})
}

// 已发送文件夹的名字每个账号只找一次：找一次要拨一次号。
func TestSentFolderIsResolvedOncePerAccount(t *testing.T) {
	mb := &fakeAppendMailbox{sentFolder: "已发送"}
	s := newSentCopyService(mb)
	acct := MailAccount{AccountID: 77, Host: "smtp.263.net"}

	for range 3 {
		s.fileSentCopy(context.Background(), acct, []byte("x"))
	}
	if mb.folderAsks != 1 {
		t.Fatalf("每封信都重新找一次已发送文件夹（%d 次），发一批信就是一批多余的往返", mb.folderAsks)
	}
	if len(mb.appends) != 3 {
		t.Fatalf("三封信只留底了 %d 封", len(mb.appends))
	}
}
