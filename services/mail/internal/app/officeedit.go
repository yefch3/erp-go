package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 在线 Office 的**写**那一半：人在浏览器里改完，Document Server 回头敲我们
// 一下，我们把改出来的文件存成附件的新一版。
//
// **原件永远不动。** 客户发来的那一份是将来对账、扯皮时唯一拿得出的东西，
// 覆盖它省下的那点事换不回一次"我们手里没有你原本发的版本"。所以改动都在
// mail_attachment_revisions 里另起一行（见迁移 00065）。
//
// ---- 为什么文件要由我们自己递出去 ----
//
// 从前 document.url 给的是对象存储的签名地址，Document Server 自己去取。那
// 条路有一个绕不过去的毛病：Document Server 给自己发出的每一个请求加签名
// （token.enable.request.outbox），而**同一个开关既管取文件也管回调**。为了
// 让 S3 别被那个多余的 Authorization 头呛到（#443：S3 见到签名地址上再带一个
// Authorization 就答 400），当时把整个 outbox 关掉了。
//
// 关掉的代价在只读时看不见，一旦能写就要命：**回调没有签名**，任何能碰到这个
// 口的东西都能往附件里塞一版。
//
// 所以换个方向解：文件不再由 Document Server 去 S3 取，而是找我们要——
// 我们从 S3 读出来、按流递过去。这样 outbox 可以开着，取文件和回调两头都带
// 签名，两头都验得了，S3 也再没机会见到那个头。顺带没了"签名地址一小时过期"
// 这件事。
//
// ---- 身份怎么验 ----
//
// 一、地址里我们自己签的那一小段 token（officeURLClaims）：哪个租户、哪封信、
//     哪个附件、谁开的，**以及这段 token 是给哪条路用的**。取件的不能拿去回存。
// 二、Document Server 的签名，而且**必须签着这一次请求本身**：
//       · 取件：签过的那份里得有这条地址（含同一个 t=），
//       · 回存：状态和"去哪儿取改完的文件"这两件事只从**签过的那一份**里读，
//         明文正文一个字都不信。
//
// 第二条为什么非得"签着这一次请求"：Document Server 用的密钥（DOCS_JWT_SECRET）
// 同时也是我们签那份**交给浏览器**的配置用的密钥——OnlyOffice 就是这么设计的，
// 换不掉。于是"验得过这把密钥"根本不能证明"来自 Document Server"：任何一个
// 打开过预览的人，手上都有一份用这把密钥签出来的东西。2026-09-14 的复查抓到
// 的正是这条——拿配置的签名当门票、再自己编一个 url，就能让邮件服务去取
// 容器网里任意一个地址的东西，然后把取回来的字节存成"附件的新一版"。
//
// 所以门不是"签名验得过"，是"签过的那一份里写着这次要干的事"。伪造不了，
// 因为伪造的人没有密钥；重放不了别的请求，因为签的内容对不上。
//
// 这个口**不发布到宿主机**，只在 compose 网里（见 docker-compose 的 expose）。

// officeURLPurpose 把地址里那段 token 的密钥和 Document Server 用的那把分开。
// 同一把密钥签两种意思不同的东西，是"拿 A 处的 token 去 B 处用"的温床。
const officeURLPurpose = "erp-office-url-v1"

// 取文件的 token 短命：Document Server 拿到配置后几秒内就来取。
// 回调的 token 长命：人可能开着编辑器改一下午，关掉窗口才回调。
const (
	officeFetchTokenTTL = 1 * time.Hour
	officeSaveTokenTTL  = 24 * time.Hour
)

// MaxOfficeSaveBytes 是存回来的上限。比从前送去转 PDF 那道 25 MB 宽一档：一份表格改完
// 通常比原来大（多了样式、多了行），卡在原尺寸上会让"改完存不回去"这种最难
// 解释的失败变常见。
const MaxOfficeSaveBytes = 50 << 20 // 50 MB

// 去 Document Server 取改完的文件最多等多久。50 MB 走容器网，两分钟很宽裕；
// 没有上限的话，对面卡住就等于这边永远挂着一个 goroutine。
const officeFetchTimeout = 2 * time.Minute

// 这段 token 是给哪条路用的。取件的那段短命、给得很随意（只读预览也有），
// 回存的那段长命、只在真的要编辑时才签——两者绝不能互换。
const (
	officePurposeFetch = "f"
	officePurposeSave  = "s"
)

// officeURLClaims 是地址里那一小段。字段名短是因为它要进 URL。
type officeURLClaims struct {
	Tenant     int64 `json:"t"`
	Inbound    int64 `json:"i"`
	Attachment int64 `json:"a"`
	// 谁开的编辑器。存回来时记在"谁改的"上——除非回调自己报了人（见
	// SaveOfficeEdit 里 users 那一段）。
	Operator int64 `json:"u"`
	// 用途，见上面那两个常量。
	Purpose string `json:"p"`
	jwt.RegisteredClaims
}

func (o *Office) urlKey() []byte {
	// 从同一把密钥派生，所以不用再往 .env 里加一个谁都会忘的变量；加一段
	// 用途字符串，所以派生出来的这把签不了、也验不了 Document Server 那边的
	// token。
	return []byte(officeURLPurpose + ":" + string(o.secret))
}

func (o *Office) signURLToken(c officeURLClaims, purpose string, ttl time.Duration) (string, error) {
	c.Purpose = purpose
	c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(ttl))
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(o.urlKey())
}

// parseURLToken 解地址里那一段，并且认这段是不是给**这条路**用的。
//
// 用途对不上就拒。少了这一句，取件那段 token（只读预览也会签出来、而且就摆在
// 浏览器能看见的地方）可以直接拿去敲回存那条路。
func (o *Office) parseURLToken(raw, purpose string) (officeURLClaims, error) {
	var c officeURLClaims
	_, err := jwt.ParseWithClaims(raw, &c, func(*jwt.Token) (any, error) { return o.urlKey(), nil },
		jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return officeURLClaims{}, err
	}
	if c.Tenant <= 0 || c.Inbound <= 0 || c.Attachment <= 0 {
		return officeURLClaims{}, errors.New("office url token is missing its subject")
	}
	if c.Purpose != purpose {
		return officeURLClaims{}, fmt.Errorf("office url token is for %q, not %q", c.Purpose, purpose)
	}
	return c, nil
}

// docsSigned 把 Document Server 签过的那一份内容取出来。**返回的是它签的，
// 不是请求里的明文**——这条区别是这个口的全部安全性所在，理由见文件开头。
//
// 两种形状都认，因为两种都签过、都安全：
//   - `{"payload": {...}}`（取件请求是这个形状，本地拿真的 9.4.0 验过）
//   - 直接把内容摊在 claims 上（回调正文里那个 token 字段是这个形状）
//
// 头的形状由 Document Server 那边的 token.outbox 定，默认
// `Authorization: Bearer <jwt>`；前缀是可配的，所以没有前缀也照解。
func (o *Office) docsSigned(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "Bearer "))
	if raw == "" {
		return nil, errors.New("no Document Server signature on the request")
	}
	claims := jwt.MapClaims{}
	if _, err := jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) { return o.secret, nil },
		jwt.WithValidMethods([]string{"HS256"})); err != nil {
		return nil, err
	}
	if inner, ok := claims["payload"].(map[string]any); ok {
		return inner, nil
	}
	return claims, nil
}

// OfficeFileRequest 是 Document Server 来取文件那一下。
type OfficeFileRequest struct {
	Token         string
	Authorization string
}

// OfficeFile 是递出去的那一份。
type OfficeFile struct {
	Name        string
	ContentType string
	Bytes       []byte
}

// ServeOfficeFile 把附件（有改过的版本就给最新那一版）读出来递给 Document
// Server。
//
// 不做 owner 检查：这条路上没有"人"，只有一台服务器。该不该给，在签这段
// token 的那一刻就已经查过了（OfficePreviewConfig 里比对过 owner）。这里要
// 守住的是"token 是我们签的、请求是 Document Server 发的"这两件事。
func (s *Service) ServeOfficeFile(ctx context.Context, req OfficeFileRequest) (OfficeFile, error) {
	if s.office == nil || s.files == nil {
		return OfficeFile{}, errors.New("office viewer is not configured")
	}
	signed, err := s.office.docsSigned(req.Authorization)
	if err != nil {
		return OfficeFile{}, fmt.Errorf("document server signature: %w", err)
	}
	// 签过的那份里得写着**这一条**地址。只验"签名对得上"是不够的：同一把
	// 密钥也签着交给浏览器的那份配置，见文件开头。
	if u, _ := signed["url"].(string); !strings.Contains(u, "t="+req.Token) {
		return OfficeFile{}, errors.New("the Document Server signature is not for this file request")
	}
	c, err := s.office.parseURLToken(req.Token, officePurposeFetch)
	if err != nil {
		return OfficeFile{}, fmt.Errorf("office url token: %w", err)
	}
	att, err := s.attachmentForOffice(ctx, c.Tenant, c.Inbound, c.Attachment, c.Operator)
	if err != nil {
		return OfficeFile{}, err
	}
	key, _ := latestAttachmentObject(att)
	data, err := s.readCapped(ctx, key, MaxOfficeSaveBytes)
	if err != nil {
		return OfficeFile{}, fmt.Errorf("read %s: %w", key, err)
	}
	return OfficeFile{Name: att.FileName, ContentType: att.ContentType, Bytes: data}, nil
}

// latestAttachmentObject 说这个附件现在该打开哪一份：改过就是最新那一版，
// 没改过就是原件。第二个返回值是版本号，0 = 原件。
func latestAttachmentObject(a store.ListInboundAttachmentsRow) (string, int32) {
	if a.RevVersion > 0 && a.RevFileKey != "" {
		return a.RevFileKey, a.RevVersion
	}
	return a.FileKey, 0
}

// attachmentForOffice 取这个附件，**并且当场再问一次这封信现在归不归他**。
//
// 打开编辑器时查过一次，但回存那段 token 活 24 小时，而这中间信可能被转派、
// 人可能离职被收回。不重新问的话，一个已经不是主人的人只要标签页还开着，
// 就还能往这封信的附件里存版本——查过一次就当永远查过，是长效凭据最容易
// 出的那种错。
func (s *Service) attachmentForOffice(
	ctx context.Context, tenantID, inboundID, attachmentID, operatorID int64,
) (store.ListInboundAttachmentsRow, error) {
	row, err := s.q.GetInbound(ctx, store.GetInboundParams{TenantID: tenantID, ID: inboundID})
	if err != nil || row.OwnerID != operatorID {
		return store.ListInboundAttachmentsRow{}, errNotFound()
	}
	atts, err := s.q.ListInboundAttachments(ctx, store.ListInboundAttachmentsParams{
		TenantID: tenantID, InboundID: inboundID,
	})
	if err != nil {
		return store.ListInboundAttachmentsRow{}, err
	}
	for i := range atts {
		if atts[i].ID == attachmentID {
			if atts[i].FileKey == "" {
				return store.ListInboundAttachmentsRow{}, errors.New("attachment has no stored object")
			}
			return atts[i], nil
		}
	}
	return store.ListInboundAttachmentsRow{}, errNotFound()
}

// officeCallback 是 Document Server 回调的正文。只列我们用得到的。
// 完整清单见 OnlyOffice 的 callback handler 文档。
type officeCallback struct {
	// 1 = 有人在改；2 = 改完了，去 url 取；3 = 存的时候出错了；
	// 4 = 开过但一个字没改；6 = 人点了保存（forcesave）；7 = forcesave 出错。
	Status int      `json:"status"`
	URL    string   `json:"url"`
	Key    string   `json:"key"`
	Users  []string `json:"users"`
}

// signedCallback 从签过名的那一份里读状态和地址。
//
// **明文正文一个字都不信。** url 是我们要拿去发请求的东西，它要是能由调用方
// 随便写，这个口就成了一台"替你去容器网里任意地址取东西"的机器。
//
// 两个地方可能带着签过的那一份，按可靠性排：正文里的 token 字段（JWT_IN_BODY
// 开着时它签的就是这份正文），和 Authorization 头。哪个先解出带状态的内容就
// 用哪个；两个都没有就拒。
func (o *Office) signedCallback(bodyToken, authorization string) (officeCallback, error) {
	var last error
	for _, raw := range []string{bodyToken, authorization} {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		claims, err := o.docsSigned(raw)
		if err != nil {
			last = err
			continue
		}
		var cb officeCallback
		blob, err := json.Marshal(claims)
		if err != nil {
			last = err
			continue
		}
		if err := json.Unmarshal(blob, &cb); err != nil {
			last = err
			continue
		}
		// status 是这份内容"是不是一次回调"的唯一标志。没有它，一份别的
		// 用途的签名（比如交给浏览器的那份配置）也会走到这儿来。
		if cb.Status != 0 {
			return cb, nil
		}
		last = errors.New("the signed payload does not look like a callback")
	}
	if last == nil {
		last = errors.New("the callback carries no Document Server signature")
	}
	return officeCallback{}, last
}

// SaveOfficeEdit 处理一次回调。
//
// 返回 error 时调用方要回 `{"error":1}`：Document Server 见到非 0 会**重试**，
// 一段时间后放弃并把文件留在它自己的缓存里。回 0 就是"我收下了"——收不下
// 却回 0，人改的东西就此蒸发，而且没有任何地方会说出来。
func (s *Service) SaveOfficeEdit(ctx context.Context, req OfficeFileRequest, body []byte) error {
	if s.office == nil || s.files == nil {
		return errors.New("office viewer is not configured")
	}
	c, err := s.office.parseURLToken(req.Token, officePurposeSave)
	if err != nil {
		return fmt.Errorf("office url token: %w", err)
	}
	// 正文只用来取出里面那个 token 字段；状态和地址都从签过的那一份里读。
	var plain struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &plain); err != nil {
		return fmt.Errorf("callback body: %w", err)
	}
	cb, err := s.office.signedCallback(plain.Token, req.Authorization)
	if err != nil {
		return fmt.Errorf("callback signature: %w", err)
	}
	switch cb.Status {
	case 1, 4:
		// 有人打开了，或者开过但没改。没有东西要存。
		return nil
	case 3, 7:
		// Document Server 自己没能把文件准备好。它不会再来了，所以这里只能
		// 记一笔——人那边看到的是"改的东西没了"，日志是唯一能对上号的线索。
		s.log.Error("document server could not hand back an edited attachment",
			"mail", c.Inbound, "attachment", c.Attachment, "status", cb.Status)
		return nil
	case 2, 6:
	default:
		// 不认识的状态照样回 0：重试一个我们看不懂的状态没有意义。
		s.log.Warn("unknown document server callback status",
			"status", cb.Status, "mail", c.Inbound, "attachment", c.Attachment)
		return nil
	}
	if strings.TrimSpace(cb.URL) == "" {
		return errors.New("callback says the file is ready but gives no url")
	}
	att, err := s.attachmentForOffice(ctx, c.Tenant, c.Inbound, c.Attachment, c.Operator)
	if err != nil {
		return err
	}
	data, err := s.fetchEdited(ctx, cb.URL)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%s.rev.%d", att.FileKey, time.Now().UnixNano())
	if err := s.files.Put(ctx, key, bytes.NewReader(data), int64(len(data)), att.ContentType); err != nil {
		return fmt.Errorf("store the edited attachment: %w", err)
	}
	ver, err := s.q.InsertAttachmentRevision(ctx, store.InsertAttachmentRevisionParams{
		TenantID:     c.Tenant,
		AttachmentID: c.Attachment,
		FileKey:      key,
		FileSize:     int64(len(data)),
		EditedBy:     officeEditor(cb.Users, c.Operator),
	})
	if err != nil {
		// 字节已经传上去了，但没有任何一行指得到它。这里不清的话，
		// Document Server 会重试整个回调（我们下面回 error:1），而每重试
		// 一次就再留一个谁也找不着的对象。
		if rmErr := s.files.Remove(ctx, key); rmErr != nil {
			s.log.Warn("could not remove an orphaned revision object", "key", key, "err", rmErr)
		}
		return fmt.Errorf("record the revision: %w", err)
	}
	s.log.Info("stored an edited attachment",
		"mail", c.Inbound, "attachment", c.Attachment, "version", ver, "bytes", len(data))
	return nil
}

// officeEditor 说这一版算在谁头上。
//
// 优先信回调里报的那个人：一份文件可以两个人一起改，而地址里那段 token 记的
// 是**开编辑器的那一个**——两个人改、第一个人先走，算在他头上就错了。
// 回调没报人或者报的不是我们认得的 id 时，退回 token 里那个。
func officeEditor(users []string, fallback int64) int64 {
	for _, u := range users {
		if id, err := strconv.ParseInt(strings.TrimSpace(u), 10, 64); err == nil && id > 0 {
			return id
		}
	}
	return fallback
}

// fetchEdited 去 Document Server 的缓存里把改完的文件取回来。
//
// 地址是它自己给的、指向它自己（compose 网里的 docs 容器）。带上限：一个
// 回调可以指向任意大小的东西，而这段是在内存里过的。
func (s *Service) fetchEdited(ctx context.Context, url string) ([]byte, error) {
	// 自己的超时，不指望调用方的 ctx：这条请求打的是另一台容器，它慢下来
	// 或者被人拿住时，挂着的这一下会一直占着一个 goroutine 和一条连接。
	ctx, cancel := context.WithTimeout(ctx, officeFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch the edited file: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch the edited file: document server answered %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxOfficeSaveBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read the edited file: %w", err)
	}
	if int64(len(data)) > MaxOfficeSaveBytes {
		return nil, apierr.Invalid("MAIL_OFFICE_SAVE_TOO_LARGE", "改完的文件过大，未能保存")
	}
	if len(data) == 0 {
		return nil, errors.New("document server handed back an empty file")
	}
	return data, nil
}
