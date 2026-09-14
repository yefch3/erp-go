package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 在线 Office 预览：附件在 OnlyOffice Document Server 里打开，浏览器里就是一个
// 完整的 Excel / Word / PPT，能翻、能搜、能选、能复制。
//
// 我们这边只做一件事：签一份"打开这个文件"的配置。文件本身不经过这里——
// Document Server 拿配置里那条签名地址（和下载按钮用的是同一种，一小时有效）
// 自己去对象存储取。所以这条路上没有转换、没有中间文件、什么都不落盘，
// 这正是它取代"转成 PDF 再看"的理由。
//
// 签名不是装饰：Document Server 只认签过的配置。不签的话，能碰到它的人都可以
// 让它去取任意地址上的文件——它是一台会主动出去抓东西的服务器。密钥只在
// 服务器的 .env 里，没有默认值，和 MAIL_CRED_KEY 一个规矩。

// PreviewOffice 是 PreviewKind 的第三档：在线 Office 里打开。
const PreviewOffice = "office"

// Office 是 Document Server 的接入信息。为 nil 就是没配：办公文档退回"转成
// PDF"那条老路（还配着 Gotenberg 的话），表格退回浏览器自己画的那一页。
type Office struct {
	// 浏览器从哪儿加载编辑器。生产上是前门 nginx 转发的 /docs，本地是 Vite
	// 代理的同一个路径。
	PublicURL string
	// Document Server 从哪儿找我们：取文件、回存改动。**这是容器网里的地址**
	// （http://mail:9011），不经过前门，公网碰不到——见 officeedit.go 开头
	// 那段。空 = 只读，编辑那一半整个关掉。
	InternalURL string
	secret      []byte
}

// NewOffice 组一份接入信息。**三样缺一不可**，缺了就当没配（回 nil），办公
// 文档退回转 PDF 那条老路。
//
// 密钥缺了尤其不能算配好：那等于把一台会出去抓文件的服务器敞开着。
//
// internalURL 也是必需的，虽然只读用不上它——因为文件从此只由我们递出去
// （见 officeedit.go 开头）。留一条"没有内网地址就让它自己去 S3 取"的退路
// 听起来稳妥，实际是留了一条**签名关着**的路：Document Server 给自己发出的
// 请求加不加签名是一个全局开关，为了那条退路把它关掉，回调就验不了身份了。
// 两条路里必须死掉一条，死的是那条。
func NewOffice(publicURL, secret, internalURL string) *Office {
	publicURL = strings.TrimRight(strings.TrimSpace(publicURL), "/")
	internalURL = strings.TrimRight(strings.TrimSpace(internalURL), "/")
	if publicURL == "" || internalURL == "" || strings.TrimSpace(secret) == "" {
		return nil
	}
	return &Office{PublicURL: publicURL, InternalURL: internalURL, secret: []byte(secret)}
}

// 哪些扩展名交给在线 Office。比 officeExtensions（转 PDF 那份）多了 csv：
// OnlyOffice 的表格编辑器认它。PDF 和图片不在这里——浏览器自己看得更快。
var officeViewerExtensions = map[string]string{
	".doc": "word", ".docx": "word", ".rtf": "word", ".odt": "word", ".txt": "word",
	".xls": "cell", ".xlsx": "cell", ".ods": "cell", ".csv": "cell",
	".ppt": "slide", ".pptx": "slide", ".odp": "slide",
}

// officeDocumentType 说这个文件在 OnlyOffice 里是哪一种编辑器，认不得回空串。
func officeDocumentType(fileName string) string {
	return officeViewerExtensions[strings.ToLower(filepath.Ext(strings.TrimSpace(fileName)))]
}

// officeDocKey 是 Document Server 的缓存键：同一个键就是同一份文件。
//
// 从对象键推出来，所以同一个附件永远是同一个键——服务器解析过一次的文件
// 第二个人打开是秒开。OnlyOffice 要求键只含字母数字和 -_.=，最长 128；这里取
// sha256 的前 20 个字节做 base64url，22 个字符。
//
// **版本必须进去。** 能改之后，同一个附件的内容会变，而 Document Server 是
// 按这个键缓存的：键不变，它会把改之前那一份直接端出来，人看到的是"我明明
// 改了，再打开还是旧的"——它不报错，只是给你一份过期的东西。
func officeDocKey(fileKey string, version int32) string {
	sum := sha256.Sum256([]byte(fileKey + "\x00v" + strconv.FormatInt(int64(version), 10)))
	return base64.RawURLEncoding.EncodeToString(sum[:20])
}

// OfficeOpen 是交给浏览器的那一份：从哪儿加载编辑器、配置、签名。
type OfficeOpen struct {
	DocsURL    string
	ConfigJSON string
	Token      string
	// 这一次是以什么身份打开的，给界面用：编辑模式下要告诉人"改动会存成
	// 新一版，原件不动"，只读时不该说这句话。
	Editable bool
	// 打开的是第几版。0 = 客户发来的原件。
	Version int32
}

// OfficePreviewConfig 签一份"在在线 Office 里打开这个附件"的配置。
//
// 权限跟着邮件走，不跟着附件走，和 PreviewInboundAttachment 同一条规矩：
// 附件 id 猜得出来，所以先按 tenant+id 取邮件、比对 owner，不是自己的信一律
// 「不存在」。不复用 GetInbound：它顺手把信标成已读，点预览不该有这个副作用。
//
// edit 是"以可编辑的方式打开"。默认不开，人要在预览页上明确点一下「编辑」
// ——不然一次误触的键盘就能给附件添一个版本，而列表上从此挂着「已改」。
//
// 权限上没有第二道门：这封信只有主人打得开（上面那条 owner 比对），主人对
// 自己的附件就该能改。真正的保护不是"不给改"，是**原件永远不动**。
func (s *Service) OfficePreviewConfig(
	ctx context.Context, tenantID int64, op Operator, inboundID, attachmentID int64, lang string, edit bool,
) (OfficeOpen, error) {
	ownerID := op.ID
	if s.office == nil {
		return OfficeOpen{}, apierr.Invalid("MAIL_OFFICE_NOT_CONFIGURED", "在线 Office 预览尚未配置")
	}
	if s.files == nil {
		return OfficeOpen{}, apierr.Invalid("MAIL_PREVIEW_NO_STORAGE", "附件存储尚未配置")
	}
	row, err := s.q.GetInbound(ctx, store.GetInboundParams{TenantID: tenantID, ID: inboundID})
	if err != nil || row.OwnerID != ownerID {
		return OfficeOpen{}, errNotFound()
	}
	atts, err := s.q.ListInboundAttachments(ctx, store.ListInboundAttachmentsParams{
		TenantID: tenantID, InboundID: inboundID,
	})
	if err != nil {
		return OfficeOpen{}, apierr.Internal("MAIL_PREVIEW_FAILED", "无法读取附件列表")
	}
	var att *store.ListInboundAttachmentsRow
	for i := range atts {
		if atts[i].ID == attachmentID {
			att = &atts[i]
			break
		}
	}
	if att == nil || att.FileKey == "" {
		return OfficeOpen{}, apierr.NotFound("MAIL_PREVIEW_ATTACHMENT_NOT_FOUND", "附件不存在或未保存")
	}
	docType := officeDocumentType(att.FileName)
	if docType == "" {
		return OfficeOpen{}, apierr.Invalid("MAIL_PREVIEW_FILE_TYPE", "这个附件类型暂不支持在线打开，请下载查看")
	}
	// 打开的是最新那一版；一次没改过就是客户发来的原件。
	objKey, version := latestAttachmentObject(*att)

	// 文件不交给 Document Server 自己去对象存储取，而是它回来找我们要。
	// 理由（以及这样换来的是什么）见 officeedit.go 开头那一大段。
	claims := officeURLClaims{
		Tenant: tenantID, Inbound: inboundID, Attachment: attachmentID, Operator: op.ID,
	}
	fileTok, err := s.office.signURLToken(claims, officePurposeFetch, officeFetchTokenTTL)
	if err != nil {
		return OfficeOpen{}, apierr.Internal("MAIL_PREVIEW_FAILED", "无法生成文件地址")
	}
	url := s.office.InternalURL + "/office/file?t=" + fileTok
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(att.FileName)), ".")

	// 配置的形状是 OnlyOffice 定的（docs-api/usage-api/config）。只读：不给编辑、
	// 不给批注、不给聊天；下载和打印给——人在这一页要做的就是看清楚再拿走。
	cfg := map[string]any{
		"documentType": docType,
		"document": map[string]any{
			"fileType": ext,
			"key":      officeDocKey(objKey, version),
			"title":    att.FileName,
			"url":      url,
			"permissions": map[string]any{
				"edit": edit, "review": false, "comment": false, "chat": false,
				"fillForms": edit, "modifyFilter": edit, "modifyContentControl": edit,
				"copy": true, "download": true, "print": true,
			},
		},
		"editorConfig": map[string]any{
			"mode": officeMode(edit),
			"lang": officeLang(lang),
			// 看信的人是谁。不给的话编辑器一打开先弹一个「输入用于协作的名称」
			// ——只读的页面问这个，人只会觉得是坏了。
			"user": map[string]any{
				"id":   strconv.FormatInt(op.ID, 10),
				"name": firstNonEmpty(strings.TrimSpace(op.Name), strconv.FormatInt(op.ID, 10)),
			},
			"customization": map[string]any{
				// 空白处别放它自己的广告和反馈入口：这是 ERP 里的一页。
				"feedback": false, "help": false,
				"compactHeader": true, "toolbarNoTabs": false,
				// 不装插件：自带的那个 AI 插件一开就往页面里拉二十几个脚本，
				// 而一个只读的查看器用不上任何插件。
				"plugins": false,
				// 同上面 user 那条：匿名名字的那个框，永远不要弹。
				"anonymous": map[string]any{"request": false},
				// 工具栏上那颗"保存"。不开的话唯一的保存时机是"最后一个人
				// 关掉编辑器之后十来秒"——人点了叉，回到列表，那里还没有
				// 「已改」的标记，他只会以为没存上。有一颗按钮就有了一个
				// 确定的时刻。
				"forcesave": edit,
			},
		},
	}
	if edit {
		// 改完往哪儿回。签的是另一段 token：取文件那段一小时就过期，而人
		// 可能开着编辑器改一下午。
		saveTok, err := s.office.signURLToken(claims, officePurposeSave, officeSaveTokenTTL)
		if err != nil {
			return OfficeOpen{}, apierr.Internal("MAIL_PREVIEW_FAILED", "无法生成回存地址")
		}
		cfg["editorConfig"].(map[string]any)["callbackUrl"] = s.office.InternalURL + "/office/save?t=" + saveTok
	}
	// 签整份配置。Document Server 拿同一把密钥验，任何一个字段被改都对不上。
	//
	// **带到期时间。** 这份签名是交到浏览器手里的，而它用的密钥和 Document
	// Server 给自己请求签名用的是同一把（OnlyOffice 就是这么设计的，换不掉）。
	// 没有 exp 的话，一份泄出去的配置签名永久有效。这条不是唯一的防线——
	// 取件和回存那两条路都要求"签过的那一份里写着这一次要干的事"，光有一份
	// 配置签名过不去——但让它自己也会过期没有坏处。
	// 拷一份再加 exp：cfg 本身要原样交给浏览器（下面 json.Marshal 的就是它），
	// 就地加一个字段会让配置里凭空多出一个 OnlyOffice 没要过的 exp。
	signed := make(jwt.MapClaims, len(cfg)+1)
	for k, v := range cfg {
		signed[k] = v
	}
	// 到期时间对齐"人可能开着编辑器改多久"（officeSaveTokenTTL），不是取件
	// 那一小时：OnlyOffice 在 websocket 断线重连时用的是浏览器里那份原始配置
	// 和签名，给一小时的话，开着改了一下午再抖一次网就连不回去了。
	//
	// 它本来也不是这条路上的主要防线——取件和回存都要求"签过的那一份里写着
	// 这一次要干的事"，光攥着一份配置签名过不去。这里要挡的只是"泄出去的
	// 一份配置签名永久有效"。
	signed["exp"] = time.Now().Add(officeSaveTokenTTL).Unix()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, signed).SignedString(s.office.secret)
	if err != nil {
		return OfficeOpen{}, apierr.Internal("MAIL_PREVIEW_FAILED", "无法签名预览配置")
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return OfficeOpen{}, apierr.Internal("MAIL_PREVIEW_FAILED", "无法生成预览配置")
	}
	return OfficeOpen{
		DocsURL: s.office.PublicURL, ConfigJSON: string(raw), Token: token,
		Editable: edit, Version: version,
	}, nil
}

// officeLang 把我们的语言码翻成 OnlyOffice 认的。认不得的给英文——编辑器
// 自己的界面文案，错一种不如给一种它一定有的。
func officeLang(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "zh", "zh-cn":
		return "zh-CN"
	case "es":
		return "es"
	default:
		return "en"
	}
}

// officeMode 是 editorConfig.mode。只有 "edit" 和 "view" 两档。
func officeMode(editable bool) string {
	if editable {
		return "edit"
	}
	return "view"
}
