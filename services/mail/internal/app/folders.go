package app

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 自建文件夹（Issue #362）。
//
// 文件夹**真的建在邮件服务器上**，不是 ERP 里的一个标签：员工在 Foxmail 里也
// 看得到同一个文件夹、同一批信。这里的表只是登记。挪信是真的 MOVE，新 UID
// 从 COPYUID 拿、行的身份当场改（见 flagsync.go 的 mergeRepoint），信和附件
// 一个字节不动——它们本来就存在对象存储里，跟文件夹无关。
//
// 挪信**同步做**，不进写回队列：写回是按行的旧身份找信的，而挪进自建文件夹
// 要改行的身份，两者会打架；而且点「移动到」的人本来就在等。失败就如实
// 报错，他重点一次。
//
// v1 的边界：员工在 Foxmail 里直接往自建文件夹收的新信，ERP 不回拉；在 ERP
// 里建、在 ERP 里挪、两边都看得到，先把这条做稳。

// MailFolder 是一个自建文件夹。
type MailFolder struct {
	ID        int64
	AccountID int64
	// Name 给人看；HostName 是服务器上的实际名字，也是 email_inbound.folder
	// 里存的、视图里 'F:' 后面跟的那一串。现在两者相等，分开存是给日后
	// "显示名和服务器名不同"留的路（比如层级）。
	Name     string
	HostName string
}

// ViewKey 是这个文件夹在列表接口里的 view 参数。
func (f MailFolder) ViewKey() string { return "F:" + f.HostName }

// maxFolderNameRunes 是文件夹名的长度上限。服务器各有各的限制，120 个字符
// 在所有已知服务商里都安全。
const maxFolderNameRunes = 120

// validFolderName 检查一个人写的文件夹名能不能安全地送到服务器上。
//
// 斜杠是大多数服务器的层级分隔符（Gmail 用 /，263 用 /，部分用 .），v1 只做
// 平级文件夹，所以不收；星号百分号是 LIST 的通配符；引号和控制字符会破坏
// 命令。名字用 INBOX 或撞上系统文件夹的英文名（Sent/Junk/Trash/Drafts）也
// 不收——Gmail 的 [Gmail]/… 前缀同理。
func validFolderName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", apierr.Invalid("MAIL_FOLDER_NAME_EMPTY", "文件夹名不能为空")
	}
	if n := len([]rune(name)); n > maxFolderNameRunes {
		return "", apierr.Invalid("MAIL_FOLDER_NAME_TOO_LONG", fmt.Sprintf("文件夹名最多 %d 个字", maxFolderNameRunes))
	}
	for _, r := range name {
		if strings.ContainsRune(`/\*%"`, r) || unicode.IsControl(r) {
			return "", apierr.Invalid("MAIL_FOLDER_NAME_INVALID", `文件夹名不能包含 / \ * % " 或控制字符`)
		}
	}
	upper := strings.ToUpper(name)
	for _, reserved := range []string{"INBOX", "SENT", "JUNK", "TRASH", "DRAFTS", "SPAM", "ARCHIVE"} {
		if upper == reserved {
			return "", apierr.Invalid("MAIL_FOLDER_NAME_RESERVED", "这个名字是系统文件夹，换一个")
		}
	}
	if strings.HasPrefix(name, "[Gmail]") {
		return "", apierr.Invalid("MAIL_FOLDER_NAME_RESERVED", "这个名字是系统文件夹，换一个")
	}
	return name, nil
}

// ownedAccount 取一个信箱，并确认它是调用者本人的。不是的一律「不存在」。
func (s *Service) ownedAccount(ctx context.Context, tenantID, employeeID, accountID int64) (MailAccount, error) {
	acct, err := s.ForAccount(ctx, tenantID, accountID)
	if err != nil || acct.EmployeeID != employeeID {
		return MailAccount{}, apierr.NotFound("MAIL_ACCOUNT_NOT_FOUND", "信箱不存在")
	}
	return acct, nil
}

// ListMailFolders 列出一个信箱的自建文件夹。
//
// 顺手把服务器上有、我们还没登记的文件夹登记进来：员工在 Foxmail 里建的
// 文件夹也该在 ERP 里看得到。登记失败或服务器连不上都不影响返回——列表
// 以库里为准，服务器只是补充。
func (s *Service) ListMailFolders(ctx context.Context, tenantID, employeeID, accountID int64) ([]MailFolder, error) {
	acct, err := s.ownedAccount(ctx, tenantID, employeeID, accountID)
	if err != nil {
		return nil, err
	}
	s.importHostFolders(ctx, tenantID, employeeID, acct)
	rows, err := s.q.ListMailFolders(ctx, store.ListMailFoldersParams{TenantID: tenantID, AccountID: accountID})
	if err != nil {
		return nil, err
	}
	out := make([]MailFolder, 0, len(rows))
	for _, r := range rows {
		out = append(out, MailFolder{ID: r.ID, AccountID: r.AccountID, Name: r.Name, HostName: r.HostName})
	}
	return out, nil
}

// importHostFolders 把服务器上有、库里没有的普通文件夹登记进来。
// 系统文件夹（INBOX、已发送、垃圾、回收站、归档、草稿，以及 Gmail 的
// [Gmail]/…）不算。
func (s *Service) importHostFolders(ctx context.Context, tenantID, employeeID int64, acct MailAccount) {
	if s.mailbox == nil {
		return
	}
	names, err := s.mailbox.ListFolders(ctx, acct)
	if err != nil {
		s.log.Info("could not list host folders; showing registered ones only",
			"account", acct.AccountID, "err", err)
		return
	}
	system := map[string]bool{"INBOX": true}
	for _, kind := range []string{"sent", "junk", "trash", "archive"} {
		if n, err := s.specialFolderOf(ctx, acct, kind); err == nil && n != "" {
			system[n] = true
		}
	}
	known := map[string]bool{}
	if rows, err := s.q.ListMailFolders(ctx, store.ListMailFoldersParams{TenantID: tenantID, AccountID: acct.AccountID}); err == nil {
		for _, r := range rows {
			known[r.HostName] = true
		}
	}
	for _, n := range names {
		if system[n] || known[n] || strings.HasPrefix(n, "[Gmail]") || strings.EqualFold(n, "Drafts") {
			continue
		}
		if _, err := validFolderName(n); err != nil {
			continue // 带层级或奇怪字符的，v1 不接
		}
		if _, err := s.q.CreateMailFolder(ctx, store.CreateMailFolderParams{
			TenantID: tenantID, AccountID: acct.AccountID, Name: n, HostName: n, CreatedBy: employeeID,
		}); err != nil {
			s.log.Warn("could not register a host folder", "account", acct.AccountID, "folder", n, "err", err)
		}
	}
}

// CreateMailFolder 建一个文件夹：先在服务器上建，成了再登记。
//
// 顺序有讲究。先登记后建，服务器那一步失败就留下一条"有名无实"的记录，
// 列表里有、点进去空的、挪信过去必失败。反过来，服务器建成了、登记失败，
// 下一次列表会把它导入回来，没有损失。
func (s *Service) CreateMailFolder(ctx context.Context, tenantID, employeeID, accountID int64, name string) (MailFolder, error) {
	name, err := validFolderName(name)
	if err != nil {
		return MailFolder{}, err
	}
	acct, err := s.ownedAccount(ctx, tenantID, employeeID, accountID)
	if err != nil {
		return MailFolder{}, err
	}
	if s.mailbox == nil {
		return MailFolder{}, apierr.Invalid("MAIL_FOLDER_NO_HOST", "邮件服务尚未配置")
	}
	if err := s.mailbox.CreateFolder(ctx, acct, name); err != nil {
		return MailFolder{}, apierr.Invalid("MAIL_FOLDER_CREATE_FAILED", "邮箱服务器拒绝新建这个文件夹："+err.Error())
	}
	row, err := s.q.CreateMailFolder(ctx, store.CreateMailFolderParams{
		TenantID: tenantID, AccountID: accountID, Name: name, HostName: name, CreatedBy: employeeID,
	})
	if err != nil {
		return MailFolder{}, apierr.Invalid("MAIL_FOLDER_EXISTS", "已经有同名的文件夹了")
	}
	return MailFolder{ID: row.ID, AccountID: row.AccountID, Name: row.Name, HostName: row.HostName}, nil
}

// RenameMailFolder 改名：服务器上 RENAME，登记改名，行里存的名字跟着改。
// RENAME 不改信的 UID（RFC 3501），所以行只改 folder 一列，视图由触发器重算。
func (s *Service) RenameMailFolder(ctx context.Context, tenantID, employeeID, folderID int64, name string) (MailFolder, error) {
	name, err := validFolderName(name)
	if err != nil {
		return MailFolder{}, err
	}
	f, err := s.q.GetMailFolder(ctx, store.GetMailFolderParams{TenantID: tenantID, ID: folderID})
	if err != nil {
		return MailFolder{}, apierr.NotFound("MAIL_FOLDER_NOT_FOUND", "文件夹不存在")
	}
	acct, err := s.ownedAccount(ctx, tenantID, employeeID, f.AccountID)
	if err != nil {
		return MailFolder{}, err
	}
	if name == f.HostName {
		return MailFolder{ID: f.ID, AccountID: f.AccountID, Name: f.Name, HostName: f.HostName}, nil
	}
	if err := s.mailbox.RenameFolder(ctx, acct, f.HostName, name); err != nil {
		return MailFolder{}, apierr.Invalid("MAIL_FOLDER_RENAME_FAILED", "邮箱服务器拒绝重命名："+err.Error())
	}
	if err := s.q.RenameMailFolder(ctx, store.RenameMailFolderParams{
		TenantID: tenantID, ID: folderID, Name: name, HostName: name,
	}); err != nil {
		return MailFolder{}, err
	}
	if _, err := s.q.RenameInboundFolder(ctx, store.RenameInboundFolderParams{
		TenantID: tenantID, AccountID: f.AccountID, OldFolder: f.HostName, NewFolder: name,
	}); err != nil {
		// 服务器和登记都改了，只有行没跟上：下次列表按新名字看会是空的。
		// 记下来，人能查；不回滚——服务器那边已经改了，回滚只会更乱。
		s.log.Warn("renamed a folder but could not relabel its mail", "folder", f.HostName, "err", err)
	}
	return MailFolder{ID: f.ID, AccountID: f.AccountID, Name: name, HostName: name}, nil
}

// DeleteMailFolder 删文件夹。**里面还有信就不删**：服务器的 DELETE 会连信一起
// 删，而"我只是想删个空文件夹"和"我要把这些信全删了"是两个完全不同的决定，
// 不能让前者悄悄变成后者。让人先把信挪走。
func (s *Service) DeleteMailFolder(ctx context.Context, tenantID, employeeID, folderID int64) error {
	f, err := s.q.GetMailFolder(ctx, store.GetMailFolderParams{TenantID: tenantID, ID: folderID})
	if err != nil {
		return apierr.NotFound("MAIL_FOLDER_NOT_FOUND", "文件夹不存在")
	}
	acct, err := s.ownedAccount(ctx, tenantID, employeeID, f.AccountID)
	if err != nil {
		return err
	}
	n, err := s.q.CountInboundInFolder(ctx, store.CountInboundInFolderParams{
		TenantID: tenantID, AccountID: f.AccountID, Folder: f.HostName,
	})
	if err != nil {
		return err
	}
	if n > 0 {
		return apierr.Invalid("MAIL_FOLDER_NOT_EMPTY", fmt.Sprintf("文件夹里还有 %d 封信，先把它们移走再删", n))
	}
	if err := s.mailbox.DeleteFolder(ctx, acct, f.HostName); err != nil {
		return apierr.Invalid("MAIL_FOLDER_DELETE_FAILED", "邮箱服务器拒绝删除："+err.Error())
	}
	return s.q.DeleteMailFolder(ctx, store.DeleteMailFolderParams{TenantID: tenantID, ID: folderID})
}

// MoveInbound 把一封信挪进某个自建文件夹（folderID = 0 表示挪回收件箱）。
//
// 同步做，成了才返回。新 UID 从 COPYUID 拿、行的身份当场改；服务器没给
// COPYUID 就按 Message-ID 在目的地找一次（263 不认，但 263 给 COPYUID）。
func (s *Service) MoveInbound(ctx context.Context, tenantID, ownerID, mailID, folderID int64) error {
	row, err := s.q.GetInbound(ctx, store.GetInboundParams{TenantID: tenantID, ID: mailID})
	if err != nil || row.OwnerID != ownerID {
		return errNotFound()
	}
	acct, err := s.ownedAccount(ctx, tenantID, ownerID, row.AccountID)
	if err != nil {
		return err
	}
	target, erpFolder := "INBOX", "INBOX"
	if folderID > 0 {
		f, err := s.q.GetMailFolder(ctx, store.GetMailFolderParams{TenantID: tenantID, ID: folderID})
		if err != nil || f.AccountID != row.AccountID {
			return apierr.NotFound("MAIL_FOLDER_NOT_FOUND", "文件夹不存在")
		}
		target, erpFolder = f.HostName, f.HostName
	}
	if row.Folder == erpFolder {
		return nil // 已经在那里了
	}
	if row.Folder == "SENT" {
		return apierr.Invalid("MAIL_MOVE_SENT", "已发送的信不能挪进文件夹")
	}
	source, err := s.hostFolder(ctx, acct, row.Folder)
	if err != nil {
		return err
	}
	moved, err := s.mailbox.MoveMessages(ctx, acct, source, []uint32{uint32(row.ImapUid)}, target)
	if err != nil {
		return apierr.Invalid("MAIL_MOVE_FAILED", "邮箱服务器拒绝移动："+err.Error())
	}
	newUID, ok := moved[uint32(row.ImapUid)]
	if !ok {
		u, found, err := s.mailbox.FindUIDByMessageID(ctx, acct, target, row.MessageID)
		if err != nil || !found {
			// 挪成功了但找不到新号：行还指着旧位置，下次同步会当成"信没了"。
			// 说清楚，比假装成功强。
			return apierr.Internal("MAIL_MOVE_LOST_TRACK", "信已移动，但没能确认它的新位置，请刷新后再看")
		}
		newUID = u
	}
	if err := s.mergeRepoint(ctx, tenantID, row.AccountID, row.Folder, row.ImapUid, erpFolder, int64(newUID), row.MessageID); err != nil {
		return err
	}
	if erpFolder == "INBOX" && row.ArchivedAt.Valid {
		// 挪回收件箱就是要它回到收件箱，不是回到归档。
		if err := s.q.ClearInboundArchived(ctx, store.ClearInboundArchivedParams{TenantID: tenantID, ID: mailID}); err != nil {
			s.log.Warn("moved to inbox but could not clear archived_at", "id", mailID, "err", err)
		}
	}
	return nil
}
