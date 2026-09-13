package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"

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
	secret    []byte
}

// NewOffice 组一份接入信息。地址或密钥缺一个都算没配——只有地址没有密钥
// 尤其不能算配好了，那等于把一台会出去抓文件的服务器敞开着。
func NewOffice(publicURL, secret string) *Office {
	publicURL = strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if publicURL == "" || strings.TrimSpace(secret) == "" {
		return nil
	}
	return &Office{PublicURL: publicURL, secret: []byte(secret)}
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
// 第二个人打开是秒开。附件存进去之后不会变（一封信的原件不会改），所以键里
// 不需要版本。OnlyOffice 要求键只含字母数字和 -_.=，最长 128；这里取
// sha256 的前 20 个字节做 base64url，22 个字符。
func officeDocKey(fileKey string) string {
	sum := sha256.Sum256([]byte(fileKey))
	return base64.RawURLEncoding.EncodeToString(sum[:20])
}

// OfficeOpen 是交给浏览器的那一份：从哪儿加载编辑器、配置、签名。
type OfficeOpen struct {
	DocsURL    string
	ConfigJSON string
	Token      string
}

// OfficePreviewConfig 签一份"在在线 Office 里只读打开这个附件"的配置。
//
// 权限跟着邮件走，不跟着附件走，和 PreviewInboundAttachment 同一条规矩：
// 附件 id 猜得出来，所以先按 tenant+id 取邮件、比对 owner，不是自己的信一律
// 「不存在」。不复用 GetInbound：它顺手把信标成已读，点预览不该有这个副作用。
func (s *Service) OfficePreviewConfig(
	ctx context.Context, tenantID int64, op Operator, inboundID, attachmentID int64, lang string,
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
	// 一小时有效的签名地址，和「下载」那颗按钮拿的是同一种。Document Server
	// 会在几秒内取走，一小时是绰绰有余。
	url, err := s.files.PresignGetInline(ctx, att.FileKey, att.ContentType)
	if err != nil {
		return OfficeOpen{}, apierr.Internal("MAIL_PREVIEW_FAILED", "无法生成文件地址")
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(att.FileName)), ".")

	// 配置的形状是 OnlyOffice 定的（docs-api/usage-api/config）。只读：不给编辑、
	// 不给批注、不给聊天；下载和打印给——人在这一页要做的就是看清楚再拿走。
	cfg := map[string]any{
		"documentType": docType,
		"document": map[string]any{
			"fileType": ext,
			"key":      officeDocKey(att.FileKey),
			"title":    att.FileName,
			"url":      url,
			"permissions": map[string]any{
				"edit": false, "review": false, "comment": false, "chat": false,
				"fillForms": false, "modifyFilter": false, "modifyContentControl": false,
				"copy": true, "download": true, "print": true,
			},
		},
		"editorConfig": map[string]any{
			"mode": "view",
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
			},
		},
	}
	// 签整份配置。Document Server 拿同一把密钥验，任何一个字段被改都对不上。
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims(cfg)).SignedString(s.office.secret)
	if err != nil {
		return OfficeOpen{}, apierr.Internal("MAIL_PREVIEW_FAILED", "无法签名预览配置")
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return OfficeOpen{}, apierr.Internal("MAIL_PREVIEW_FAILED", "无法生成预览配置")
	}
	return OfficeOpen{DocsURL: s.office.PublicURL, ConfigJSON: string(raw), Token: token}, nil
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
