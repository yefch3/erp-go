package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
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

// 文件夹的角色。左栏按同一级别显示服务器上的全部文件夹，角色决定图标、
// 位置、能不能改名删除：只有 CUSTOM 能。角色按可信度定，见 roleOf。
const (
	roleInbox   = "INBOX"
	roleSent    = "SENT"
	roleJunk    = "JUNK"
	roleTrash   = "TRASH"
	roleArchive = "ARCHIVE"
	roleDrafts  = "DRAFTS"
	// roleSystem 是服务器自带、ERP 不认得的真文件夹：病毒文件夹、广告邮件、
	// 其他文件夹。里面是真的信，同步、显示，只是不能改名删除。
	roleSystem = "SYSTEM"
	// roleVirtual 是「内容是别处的信的映射」：Gmail 的「重要」「已加星标」
	// 「全部邮件」，以及只作层级容器、选都选不进去的 [Gmail]。
	//
	// 和 SYSTEM 分开是因为**同步**：一封收件箱的信同时挂着这些标签，把它们
	// 当文件夹同步下来，同一封信会按 (租户, 信箱, 文件夹, UID) 存好几行。
	// 不同步，也不显示。
	roleVirtual = "VIRTUAL"
	roleCustom  = "CUSTOM"
)

// roleRank 是左栏的顺序：认得的系统文件夹在前，自建的在后，同角色按名字。
var roleRank = map[string]int{
	roleInbox: 0, roleSent: 1, roleArchive: 2, roleJunk: 3, roleTrash: 4,
	roleDrafts: 5, roleSystem: 6, roleCustom: 7, roleVirtual: 8,
}

// syncableRole 说这个角色的文件夹要不要把里面的信收进 ERP。
//
// 收件箱、已发送、垃圾邮件走各自专门的那条路（syncOne 里），不在这里。
// 归档和回收站**不能**收：ERP 归档/删除是「行留在收件箱、加个标记，服务器
// 那份挪去归档/回收站」，把那两个文件夹收进来，同一封信会多出一行。
// 草稿箱留给下一期（草稿分两层）。虚拟的见 roleVirtual。
func syncableRole(role string) bool {
	return role == roleCustom || role == roleSystem
}

// MailFolder 是服务器上的一个文件夹在 ERP 里的登记。
type MailFolder struct {
	ID        int64
	AccountID int64
	// Name 给人看；HostName 是服务器上的实际名字，也是 email_inbound.folder
	// 里存的、视图里 'F:' 后面跟的那一串。现在两者相等，分开存是给日后
	// "显示名和服务器名不同"留的路（比如层级）。
	Name     string
	HostName string
	Role     string
}

// ViewKey 是这个文件夹在列表接口里的 view 参数。只有自建的走 'F:' 视图；
// 系统文件夹各有各的视图（INBOX、TRASH……），由前端按角色映射。
func (f MailFolder) ViewKey() string {
	if syncableRole(f.Role) {
		return "F:" + f.HostName
	}
	return ""
}

// maxFolderNameRunes 是文件夹名的长度上限。服务器各有各的限制，120 个字符
// 在所有已知服务商里都安全。
const maxFolderNameRunes = 120

// HostFolder 是服务器 LIST 回来的一个文件夹。Special 是「这不是用户建的」：
// 带 RFC 6154 的 special-use 属性（\Drafts \Sent \Junk \Trash \Archive
// \All \Flagged \Important）或者 \Noselect（只是个层级容器，选不进去）。
type HostFolder struct {
	Name    string
	Special bool
	// Role 是属性翻出来的角色提示（DRAFTS/SENT/JUNK/TRASH/ARCHIVE/SYSTEM），
	// 没有属性就是空串。属性是权威的，所以它在 roleOf 里排在猜名字前面。
	Role string
}

// providerSystemFolders 是各家邮箱服务器自带的系统文件夹名。
//
// 属性是权威的，可 263、网易这些老服务器不声明属性，只有名字——263 的
// 草稿箱、已归档，网易的病毒文件夹，都是这样漏进「自建文件夹」列表的：
// 列表里多出一个「草稿箱」，改名时服务器答 "can't rename default folder"。
//
// 只收各家**真实的默认名**，不收「像系统文件夹的词」：Gmail 这类靠属性认的
// 服务器上，用户建一个 Templates 或 Notes 是正当需求，挡了还说它是系统文件夹
// 就是误导。名单永远不可能完整，所以它是最后一道，不是唯一一道。大小写不分。
var providerSystemFolders = []string{
	// 263 / 网易 163、126 / 新浪
	"收件箱", "草稿箱", "草稿夹", "已发送", "已发送邮件", "发件箱",
	"已删除", "已删除邮件", "垃圾邮件", "垃圾箱", "已归档",
	"病毒文件夹", "病毒邮件", "广告邮件", "订阅邮件", "其他文件夹",
	// QQ / 腾讯企业 / 阿里 / 通用英文
	"INBOX", "Drafts", "Sent", "Sent Messages", "Sent Items",
	"Deleted Messages", "Deleted Items", "Trash", "Junk", "Spam", "Archive",
}

// isProviderSystemFolder 判断一个名字是不是某家服务器的系统文件夹。
func isProviderSystemFolder(name string) bool {
	name = strings.TrimSpace(name)
	for _, sys := range providerSystemFolders {
		if strings.EqualFold(name, sys) {
			return true
		}
	}
	return false
}

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
	if isProviderSystemFolder(name) {
		return "", apierr.Invalid("MAIL_FOLDER_NAME_RESERVED", "这个名字是系统文件夹，换一个")
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

// ListMailFolders 列出一个信箱的文件夹。
//
// **答案来自库，不等服务器。** 顺手对一遍服务器上有哪些（员工在 Foxmail 里
// 建的文件夹也该看得到），但那一步放到后台去：它要发一条 IMAP LIST，连接是
// 冷的时候还要重新握手加登录，人在页面上要等好几秒才看见左栏——而绝大多数
// 时候答案和库里的一模一样。
//
// 只有一种情况非等不可：库里一条都没有。那时候直接返回等于给一个空左栏，
// 而空左栏和"这个箱没有文件夹"分不出来。
func (s *Service) ListMailFolders(ctx context.Context, tenantID, employeeID, accountID int64) ([]MailFolder, error) {
	acct, err := s.ownedAccount(ctx, tenantID, employeeID, accountID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListMailFolders(ctx, store.ListMailFoldersParams{TenantID: tenantID, AccountID: accountID})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		s.syncHostFolders(ctx, tenantID, employeeID, acct)
		rows, err = s.q.ListMailFolders(ctx, store.ListMailFoldersParams{TenantID: tenantID, AccountID: accountID})
		if err != nil {
			return nil, err
		}
	} else {
		s.refreshHostFoldersInBackground(tenantID, employeeID, acct)
	}
	out := make([]MailFolder, 0, len(rows))
	for _, r := range rows {
		out = append(out, MailFolder{ID: r.ID, AccountID: r.AccountID, Name: r.Name, HostName: r.HostName, Role: r.Role})
	}
	sort.SliceStable(out, func(a, b int) bool {
		ra, rb := roleRank[out[a].Role], roleRank[out[b].Role]
		if ra != rb {
			return ra < rb
		}
		return out[a].Name < out[b].Name
	})
	return out, nil
}

// isDraftsName 认各家草稿箱的名字：属性缺席时的兜底。
func isDraftsName(name string) bool {
	for _, d := range []string{"Drafts", "Draft", "草稿箱", "草稿夹", "草稿"} {
		if strings.EqualFold(strings.TrimSpace(name), d) {
			return true
		}
	}
	return false
}

// roleOf 给服务器上的一个文件夹定角色，按可信度从高到低：
//
//  1. INBOX 就是 INBOX。
//  2. 我们定位到的已发送 / 垃圾邮件 / 已删除 / 归档（定位本身先看属性再猜名字）。
//  3. 服务器声明的属性（Gmail、新服务器）。
//  4. Gmail 的 [Gmail]/… 一律系统。
//  5. 各家默认名：草稿箱一类是 DRAFTS，其余是 SYSTEM。
//  6. 都不是，才算用户建的。
//
// 名单永远不可能完整，所以漏网的会成为 CUSTOM；服务器对它的改名/删除答
// 「默认文件夹」时再改成 SYSTEM（见 hostSaysDefaultFolder）。
func roleOf(hf HostFolder, specials map[string]string) string {
	n := hf.Name
	switch {
	case n == "INBOX":
		return roleInbox
	case n != "" && n == specials["sent"]:
		return roleSent
	case n != "" && n == specials["junk"]:
		return roleJunk
	case n != "" && n == specials["trash"]:
		return roleTrash
	case n != "" && n == specials["archive"]:
		return roleArchive
	case hf.Role != "":
		return hf.Role
	case strings.HasPrefix(n, "[Gmail]"):
		// Gmail 的系统标签。属性没说是哪一种就当虚拟的：宁可少同步一个，
		// 不可把整个邮箱按标签存好几遍。
		return roleVirtual
	case isDraftsName(n):
		return roleDrafts
	case isProviderSystemFolder(n):
		return roleSystem
	default:
		return roleCustom
	}
}

// refreshHostFoldersInBackground 在请求答完之后再去和服务器对一遍。
//
// 脱开请求的上下文：浏览器拿到列表就走了，请求的 ctx 随即取消，而这一步还
// 没发出去。同一个信箱同时只跑一个——三个页签一起刷新不该变成三条 LIST。
//
// 代价说清楚：在 Foxmail 里新建的文件夹，要到**下一次**打开这个信箱才出现。
// 拿这个换掉每次进邮箱都等几秒，值。
func (s *Service) refreshHostFoldersInBackground(tenantID, employeeID int64, acct MailAccount) {
	if _, busy := s.folderRefresh.LoadOrStore(acct.AccountID, true); busy {
		return
	}
	go func() {
		defer s.folderRefresh.Delete(acct.AccountID)
		ctx, cancel := context.WithTimeout(context.Background(), folderRefreshTimeout)
		defer cancel()
		s.syncHostFolders(ctx, tenantID, employeeID, acct)
	}()
}

// folderRefreshTimeout 给后台那一趟对账封顶：一台连上就挂的服务器不该留下
// 一个永远跑不完的 goroutine，也不该把「同时只跑一个」这个锁永远占着。
const folderRefreshTimeout = 30 * time.Second

// syncHostFolders 把服务器 LIST 回来的全部文件夹登记进来（或刷新角色），
// 服务器上已经没有、库里又没有信的登记清掉。LIST 失败就什么都不动，列表
// 以库里为准——服务器暂时连不上不该让左栏少一截。
func (s *Service) syncHostFolders(ctx context.Context, tenantID, employeeID int64, acct MailAccount) {
	if s.mailbox == nil {
		return
	}
	// 先记下 LIST 之前库里有哪些，再去问服务器。清理只针对这一批：LIST 进行
	// 中另一个页面刚建的文件夹（服务器上有、库里刚登记）不在这批里，不会被
	// 当成"服务器上没了"清掉——否则刚建的文件夹一刷新就消失一次。
	before, err := s.q.ListMailFolders(ctx, store.ListMailFoldersParams{TenantID: tenantID, AccountID: acct.AccountID})
	if err != nil {
		return
	}
	names, err := s.mailbox.ListFolders(ctx, acct)
	if err != nil {
		s.log.Info("could not list host folders; showing registered ones only",
			"account", acct.AccountID, "err", err)
		return
	}
	specials := map[string]string{}
	for _, kind := range []string{"sent", "junk", "trash", "archive"} {
		if n, err := s.specialFolderOf(ctx, acct, kind); err == nil && n != "" {
			specials[kind] = n
		}
	}
	onHost := map[string]bool{}
	for _, hf := range names {
		if _, err := validFolderName(hf.Name); err != nil && roleOf(hf, specials) == roleCustom {
			continue // 带层级或奇怪字符的自建文件夹，v1 不接；系统的名字不受这条限制
		}
		onHost[hf.Name] = true
		if _, err := s.q.UpsertHostFolder(ctx, store.UpsertHostFolderParams{
			TenantID: tenantID, AccountID: acct.AccountID, HostName: hf.Name,
			Role: roleOf(hf, specials), CreatedBy: employeeID,
		}); err != nil {
			s.log.Warn("could not register a host folder", "account", acct.AccountID, "folder", hf.Name, "err", err)
		}
	}
	for _, r := range before {
		if onHost[r.HostName] {
			continue
		}
		// 服务器上没了（在 Foxmail 里删的）。里面还有 ERP 的信就留着：
		// 让信失去文件夹比左栏多一行更糟。
		n, err := s.q.CountInboundInFolder(ctx, store.CountInboundInFolderParams{TenantID: tenantID, AccountID: acct.AccountID, Folder: r.HostName})
		if err != nil || n > 0 {
			continue
		}
		if err := s.q.DeleteMailFolder(ctx, store.DeleteMailFolderParams{TenantID: tenantID, ID: r.ID}); err != nil {
			s.log.Warn("could not drop a folder gone from the host", "account", acct.AccountID, "folder", r.HostName, "err", err)
		}
	}
}

// hostSaysDefaultFolder 认出服务器「这是默认文件夹，不能改/删」的答复：
// 263 答 "can't rename default folder or Invalid folder name"，Gmail 答
// "System folder cannot be renamed"。认出来就把角色改成 SYSTEM——服务器的
// 拒绝是最后的裁判，而且之后列文件夹不会再把它盖回 CUSTOM。
//
// 正因为改了就不回头，这里**只认明确说「默认/系统文件夹」的话**，不认泛泛的
// "can't rename"：Dovecot 类服务器改名撞到已存在的名字也答 "Can't rename
// mailbox to X: already exists"，那是名字的问题，不是文件夹的身份，误判会把
// 一个正当的自建文件夹永久锁死。宁可漏判（用户看到服务器原话、再点一次），
// 不可误判。真误判了的恢复路：在 Foxmail 里改个名，旧登记因服务器上没了
// 而被清掉，下次以新名字重新登记成 CUSTOM。
func hostSaysDefaultFolder(err error) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Error())
	for _, hint := range []string{"default folder", "system folder", "cannot be renamed", "cannot be deleted", "系统文件夹"} {
		if strings.Contains(m, hint) {
			return true
		}
	}
	return false
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
		TenantID: tenantID, AccountID: accountID, Name: name, HostName: name, Role: roleCustom, CreatedBy: employeeID,
	})
	if err != nil {
		return MailFolder{}, apierr.Invalid("MAIL_FOLDER_EXISTS", "已经有同名的文件夹了")
	}
	return MailFolder{ID: row.ID, AccountID: row.AccountID, Name: row.Name, HostName: row.HostName, Role: row.Role}, nil
}

// RenameMailFolder 改名：服务器上 RENAME，登记改名，行里存的名字跟着改。
// RENAME 不改信的 UID（RFC 3501），所以行只改 folder 一列，视图由触发器重算。
func (s *Service) RenameMailFolder(ctx context.Context, tenantID, employeeID, folderID int64, name string) (MailFolder, error) {
	f, err := s.q.GetMailFolder(ctx, store.GetMailFolderParams{TenantID: tenantID, ID: folderID})
	if err != nil {
		return MailFolder{}, apierr.NotFound("MAIL_FOLDER_NOT_FOUND", "文件夹不存在")
	}
	acct, err := s.ownedAccount(ctx, tenantID, employeeID, f.AccountID)
	if err != nil {
		return MailFolder{}, err
	}
	if f.Role != roleCustom {
		return MailFolder{}, apierr.Invalid("MAIL_FOLDER_SYSTEM", "这是邮箱服务器自带的文件夹，不能改名")
	}
	// 改成同名 = 不动。放在校验前面：名单扩了之后，一个改版前就叫「已归档」
	// 的正当文件夹，改成同名本该是无操作，先校验会把它拒掉。
	if strings.TrimSpace(name) == f.HostName {
		return MailFolder{ID: f.ID, AccountID: f.AccountID, Name: f.Name, HostName: f.HostName, Role: f.Role}, nil
	}
	name, err = validFolderName(name)
	if err != nil {
		return MailFolder{}, err
	}
	if err := s.mailbox.RenameFolder(ctx, acct, f.HostName, name); err != nil {
		if hostSaysDefaultFolder(err) {
			_ = s.q.SetMailFolderRole(ctx, store.SetMailFolderRoleParams{TenantID: tenantID, ID: f.ID, Role: roleSystem})
			return MailFolder{}, apierr.Invalid("MAIL_FOLDER_SYSTEM", "邮箱服务器说这是它自带的文件夹，不能改名；已按系统文件夹处理")
		}
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
	return MailFolder{ID: f.ID, AccountID: f.AccountID, Name: name, HostName: name, Role: f.Role}, nil
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
	if f.Role != roleCustom {
		return apierr.Invalid("MAIL_FOLDER_SYSTEM", "这是邮箱服务器自带的文件夹，不能删除")
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
		if hostSaysDefaultFolder(err) {
			_ = s.q.SetMailFolderRole(ctx, store.SetMailFolderRoleParams{TenantID: tenantID, ID: f.ID, Role: roleSystem})
			return apierr.Invalid("MAIL_FOLDER_SYSTEM", "邮箱服务器说这是它自带的文件夹，不能删除；已按系统文件夹处理")
		}
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
		if f.Role != roleCustom {
			return apierr.Invalid("MAIL_FOLDER_SYSTEM", "只能挪进自建文件夹或收件箱")
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

// maxBatchMove 一次最多挪多少封——**展开整条会话之后**的封数，不是勾选的
// 行数。列表一页几十行，勾满一页也远不到这个数；上限挡的是勾了一页长会话
// （客户拉锯几十个来回的那种）一次要挪几千封，以及脚本一次塞几千个 id。
// 是变量不是常量：测试把它调小来钉住「卡的是展开后的数」。
var maxBatchMove = 200

// moveTarget 是「挪到哪」：folderID = 0 是收件箱，任何信箱都有；自建文件夹
// 属于某一个信箱，别的信箱的信挪不进去。
type moveTarget struct {
	accountID int64
	host, erp string
}

func (s *Service) resolveMoveTarget(ctx context.Context, tenantID, folderID int64) (moveTarget, error) {
	if folderID <= 0 {
		return moveTarget{host: "INBOX", erp: "INBOX"}, nil
	}
	f, err := s.q.GetMailFolder(ctx, store.GetMailFolderParams{TenantID: tenantID, ID: folderID})
	if err != nil {
		return moveTarget{}, apierr.NotFound("MAIL_FOLDER_NOT_FOUND", "文件夹不存在")
	}
	switch f.Role {
	case roleCustom:
		return moveTarget{accountID: f.AccountID, host: f.HostName, erp: f.HostName}, nil
	case roleJunk:
		// 「标为垃圾邮件」。这是把信真的挪进服务器的垃圾箱，不是打个标记——
		// 服务商的过滤器靠这个学，只在 ERP 里记一笔它学不到。
		//
		// erp 写 'JUNK' 而不是主机上那个文件夹名：视图是按 folder 的值算的
		// （mail_view_of），写成「垃圾邮件」三个字的话它会被当成一个**自建
		// 文件夹**（F:垃圾邮件），左栏凭空多出一个同名的格子。
		return moveTarget{accountID: f.AccountID, host: f.HostName, erp: roleJunk}, nil
	}
	// 其余系统文件夹一律不收，各有各的理由：
	//
	//   已发送  —— 挪进去之后这封信在界面上就成了「我发出的」，而它是收到的。
	//   草稿箱  —— ERP 的草稿是另一张表，一封收到的信变不成草稿。
	//   回收站  —— 视图看的是 deleted_at 不是 folder，改 folder 会让它从收件箱
	//             消失却不出现在回收站里。走标记那条路（MarkInbound deleted）。
	//   归档    —— 同上，看的是 archived_at。
	//   虚拟    —— Gmail 的「重要」「已加星标」是标签不是文件夹，挪进去会让
	//             同一封信被存两遍。
	return moveTarget{}, apierr.Invalid("MAIL_FOLDER_SYSTEM", "只能挪进自建文件夹、收件箱或垃圾邮件")
}

// batchMoveItem 是待挪的一封信在库里的身份。
type batchMoveItem struct {
	id, accountID, uid int64
	folder, messageID  string
	archived           bool
}

// moveGroup 是同一个信箱、同一个来源文件夹里的一批：服务器上一次 MOVE 挪完。
type moveGroup struct {
	accountID int64
	folder    string
}

// MoveInboundBatch 把一批信挪进同一个文件夹（folderID = 0 是收件箱）。
//
// 从列表来的批量操作。列表一行是一条会话，wholeThread 时整条会话一起挪——
// 只挪最新那封会把行留在原地、少一封（MarkInbound 的归档同一个道理）。
//
// **按来源文件夹分组，每组一次 MOVE。** 一次登录挪一批，而不是每封信各登录
// 一次：网易对频繁登录会限流，二十封信挪二十次登录就是二十次被掐的机会，
// 而且慢二十倍。一组里服务器拒了就整组失败；服务器挪成了但个别信拿不到
// 新号的，那几封单独算失败（行还指着旧位置，下次同步会当成"信没了"，所以
// 宁可报出来）。
//
// 返回挪成功的封数和失败的 id。一封失败不影响其他封。
func (s *Service) MoveInboundBatch(ctx context.Context, tenantID, ownerID int64, ids []int64, folderID int64, wholeThread bool) (int, []int64, error) {
	if len(ids) == 0 {
		return 0, nil, apierr.Invalid("MAIL_MOVE_EMPTY", "没有选中任何邮件")
	}
	if len(ids) > maxBatchMove {
		return 0, nil, apierr.Invalid("MAIL_MOVE_TOO_MANY", fmt.Sprintf("一次最多移动 %d 封", maxBatchMove))
	}
	target, err := s.resolveMoveTarget(ctx, tenantID, folderID)
	if err != nil {
		return 0, nil, err
	}
	items, failed := s.collectMoveItems(ctx, tenantID, ownerID, ids, wholeThread)
	if len(items) > maxBatchMove {
		// 卡在碰服务器之前：挪了一半再说「太多」，另一半还留在原地，比不挪更糟。
		return 0, nil, apierr.Invalid("MAIL_MOVE_TOO_MANY", fmt.Sprintf("这些会话展开后共 %d 封，一次最多移动 %d 封，请少勾几行", len(items), maxBatchMove))
	}
	moved := 0
	groups := map[moveGroup][]batchMoveItem{}
	var order []moveGroup
	for _, it := range items {
		switch {
		case target.accountID != 0 && it.accountID != target.accountID:
			failed = append(failed, it.id) // 别的信箱的信挪不进这个信箱的文件夹
		case it.folder == "SENT":
			failed = append(failed, it.id)
		case it.folder == target.erp:
			moved++ // 已经在那里了
		default:
			g := moveGroup{it.accountID, it.folder}
			if _, ok := groups[g]; !ok {
				order = append(order, g)
			}
			groups[g] = append(groups[g], it)
		}
	}
	for _, g := range order {
		n, bad := s.moveGroup(ctx, tenantID, ownerID, g, groups[g], target)
		moved += n
		failed = append(failed, bad...)
	}
	return moved, failed, nil
}

// collectMoveItems 把 id 换成库里的行；wholeThread 时展开成整条会话。
// 不是自己的、不存在的，直接算失败。
func (s *Service) collectMoveItems(ctx context.Context, tenantID, ownerID int64, ids []int64, wholeThread bool) ([]batchMoveItem, []int64) {
	seen := map[int64]bool{}
	var items []batchMoveItem
	var failed []int64
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		row, err := s.q.GetInbound(ctx, store.GetInboundParams{TenantID: tenantID, ID: id})
		if err != nil || row.OwnerID != ownerID {
			failed = append(failed, id)
			continue
		}
		if wholeThread && row.ThreadKey != "" {
			members, err := s.q.ListInboundThreadMembers(ctx, store.ListInboundThreadMembersParams{
				TenantID: tenantID, OwnerID: ownerID, AccountID: row.AccountID, ThreadKey: row.ThreadKey,
			})
			if err == nil && len(members) > 0 {
				for _, m := range members {
					if m.ID != id && seen[m.ID] {
						continue
					}
					seen[m.ID] = true
					items = append(items, batchMoveItem{id: m.ID, accountID: row.AccountID, uid: m.ImapUid, folder: m.Folder, messageID: m.MessageID, archived: m.ArchivedAt.Valid})
				}
				continue
			}
		}
		items = append(items, batchMoveItem{id: id, accountID: row.AccountID, uid: row.ImapUid, folder: row.Folder, messageID: row.MessageID, archived: row.ArchivedAt.Valid})
	}
	return items, failed
}

// moveGroup 在服务器上一次 MOVE 挪一组，然后逐封改行的身份。
func (s *Service) moveGroup(ctx context.Context, tenantID, ownerID int64, g moveGroup, items []batchMoveItem, target moveTarget) (int, []int64) {
	allFailed := func() []int64 {
		out := make([]int64, 0, len(items))
		for _, it := range items {
			out = append(out, it.id)
		}
		return out
	}
	acct, err := s.ownedAccount(ctx, tenantID, ownerID, g.accountID)
	if err != nil {
		return 0, allFailed()
	}
	source, err := s.hostFolder(ctx, acct, g.folder)
	if err != nil {
		return 0, allFailed()
	}
	uids := make([]uint32, 0, len(items))
	for _, it := range items {
		uids = append(uids, uint32(it.uid))
	}
	movedUIDs, err := s.mailbox.MoveMessages(ctx, acct, source, uids, target.host)
	if err != nil {
		s.log.Warn("batch move rejected by host", "account", g.accountID, "from", source, "to", target.host, "n", len(uids), "err", err)
		return 0, allFailed()
	}
	moved := 0
	var failed []int64
	for _, it := range items {
		newUID, ok := movedUIDs[uint32(it.uid)]
		if !ok {
			u, found, err := s.mailbox.FindUIDByMessageID(ctx, acct, target.host, it.messageID)
			if err != nil || !found {
				s.log.Warn("moved on host but lost track of the new uid", "id", it.id, "to", target.host, "err", err)
				failed = append(failed, it.id)
				continue
			}
			newUID = u
		}
		if err := s.mergeRepoint(ctx, tenantID, it.accountID, it.folder, it.uid, target.erp, int64(newUID), it.messageID); err != nil {
			failed = append(failed, it.id)
			continue
		}
		if target.erp == roleInbox && it.archived {
			if err := s.q.ClearInboundArchived(ctx, store.ClearInboundArchivedParams{TenantID: tenantID, ID: it.id}); err != nil {
				s.log.Warn("moved to inbox but could not clear archived_at", "id", it.id, "err", err)
			}
		}
		// 挪进垃圾邮件：平反标记要一起去掉，否则 folder 是 JUNK 而视图仍然
		// 说它在收件箱。见 ClearInboundNotJunk 上的说明。
		//
		// 无条件清，不先读一遍它是不是真的置着：一次写比一次读加一次写便宜，
		// 而「本来就是 FALSE」再写一次 FALSE 什么也不会发生。
		if target.erp == roleJunk {
			if err := s.q.ClearInboundNotJunk(ctx, store.ClearInboundNotJunkParams{TenantID: tenantID, ID: it.id}); err != nil {
				s.log.Warn("moved to junk but could not clear not_junk", "id", it.id, "err", err)
			}
		}
		moved++
	}
	return moved, failed
}
