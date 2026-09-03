package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 转成 PDF 之后允许预览的文件类型。
//
// **按扩展名判断，不按 Content-Type。** 邮件里的 Content-Type 是发信那一方
// 写的，实践中大量附件标成 application/octet-stream —— 一份 .xlsx 报价单
// 因为发信服务器偷懒就变成「只能下载」，是这里唯一要避免的事。扩展名是
// 收信人自己看得见的那个事实，也是 LibreOffice 自己用来挑过滤器的依据。
var officeExtensions = map[string]bool{
	// Word
	".doc": true, ".docx": true, ".rtf": true, ".odt": true,
	// Excel
	".xls": true, ".xlsx": true, ".ods": true,
	// PowerPoint
	".ppt": true, ".pptx": true, ".odp": true,
}

// PreviewKind 的两个取值。空表示不能预览，只能下载。
const (
	// 地址已经填好，浏览器直接就能显示：图片和 PDF。
	PreviewDirect = "direct"
	// 要先转成 PDF：调 PreviewInboundAttachment 拿地址。
	PreviewConvert = "convert"
)

// convertibleToPDF 说这个附件值不值得送去转换。
//
// 名字里带扩展名才算：一个叫 "attachment" 的部件我们不知道它是什么，把它
// 喂给 LibreOffice 只是白等一次。
func convertibleToPDF(fileName string) bool {
	return officeExtensions[strings.ToLower(filepath.Ext(strings.TrimSpace(fileName)))]
}

// previewKeyFor 是转好的 PDF 存在哪。
//
// 从原件的对象键推出来，不进数据库：**同一个原件永远推出同一个键**，所以
// 「转过没有」这个问题就是「这个对象在不在」，不需要一张表来记，也不会和
// 真实存储状态对不上。两个人同时点预览会各转一次、写同一个键，内容一样，
// 后写的覆盖先写的，没有损失。
//
// 后缀写死 .pdf 而不是拼原文件名：原文件名是发信人给的，不能进对象键。
func previewKeyFor(fileKey string) string {
	if strings.TrimSpace(fileKey) == "" {
		return ""
	}
	return fileKey + ".preview.pdf"
}

// Converter 把一份办公文档变成 PDF。
//
// 实现是一个独立容器里的 LibreOffice（见 adapter/gotenberg）。放在接口后面
// 不只是为了测试：转换要读客户发来的、谁都没检查过的文件，把这件事关在别的
// 进程、别的容器里，是它唯一该待的地方。
type Converter interface {
	// ToPDF 收原始字节，返回 PDF 字节。fileName 只用来让转换器挑对过滤器
	// （.xls 和 .docx 不是一个读法），不会被当成路径。
	ToPDF(ctx context.Context, fileName string, data []byte) ([]byte, error)
}

// MaxConvertBytes 是送去转换的上限。
//
// 不是安全边界（转换在别的容器里），是等待边界：一份 80 MB 的表格能让
// LibreOffice 忙上几分钟，而点「预览」的人只想扫一眼。超过就老实说太大，
// 让他下载——下载本来就一直可用。
const MaxConvertBytes = 25 << 20 // 25 MB

// PreviewInboundAttachment 返回一个能把这个附件直接显示出来的地址。
//
// 图片和 PDF 在列表那一步就已经带着预览地址了（见 signDownloads），走到这里
// 的是办公文档：先看转好的 PDF 在不在对象存储里，不在就转一次存起来。所以
// 第二个人点开同一份报价单是一次 HEAD，不是又一次转换。
//
// 权限跟着邮件走，不跟着附件走：附件 id 是猜得出来的，所以先按 tenant+id 取
// 邮件、比对 owner，和 GetInbound 同一条规矩——不是自己的信一律「不存在」，
// 连「存在但不给你看」都不说。这里不复用 GetInbound 本身，因为它顺手把邮件
// 标成已读：点预览不该有这个副作用。
func (s *Service) PreviewInboundAttachment(
	ctx context.Context, tenantID, ownerID, inboundID, attachmentID int64,
) (string, error) {
	if s.files == nil {
		return "", apierr.Invalid("MAIL_PREVIEW_NO_STORAGE", "附件存储尚未配置")
	}
	row, err := s.q.GetInbound(ctx, store.GetInboundParams{TenantID: tenantID, ID: inboundID})
	if err != nil || row.OwnerID != ownerID {
		return "", errNotFound()
	}
	atts, err := s.q.ListInboundAttachments(ctx, store.ListInboundAttachmentsParams{
		TenantID: tenantID, InboundID: inboundID,
	})
	if err != nil {
		return "", apierr.Internal("MAIL_PREVIEW_FAILED", "无法读取附件列表")
	}
	var att *store.ListInboundAttachmentsRow
	for i := range atts {
		if atts[i].ID == attachmentID {
			att = &atts[i]
			break
		}
	}
	if att == nil || att.FileKey == "" {
		return "", apierr.NotFound("MAIL_PREVIEW_ATTACHMENT_NOT_FOUND", "附件不存在或未保存")
	}
	if !convertibleToPDF(att.FileName) {
		return "", apierr.Invalid("MAIL_PREVIEW_FILE_TYPE", "这个附件类型暂不支持预览，请下载查看")
	}

	// 已经转过就直接给地址。这是常路：一封信的附件往往被反复点开。
	key := previewKeyFor(att.FileKey)
	if _, _, err := s.files.Stat(ctx, key); err == nil {
		return s.signPreviewPDF(ctx, key)
	}
	if s.converter == nil {
		return "", apierr.Invalid("MAIL_PREVIEW_NOT_CONFIGURED", "文档预览尚未配置")
	}
	if att.FileSize > MaxConvertBytes {
		return "", apierr.Invalid("MAIL_PREVIEW_TOO_LARGE", "文件过大，暂不支持在线预览，请下载查看")
	}

	src, err := s.readCapped(ctx, att.FileKey, MaxConvertBytes)
	if err != nil {
		if errors.Is(err, errTooLarge) {
			return "", apierr.Invalid("MAIL_PREVIEW_TOO_LARGE", "文件过大，暂不支持在线预览，请下载查看")
		}
		return "", apierr.Internal("MAIL_PREVIEW_SOURCE_UNREADABLE", "读取原文件失败")
	}
	pdf, err := s.converter.ToPDF(ctx, att.FileName, src)
	if err != nil {
		s.log.Warn("could not convert an attachment for preview",
			"mail", inboundID, "attachment", attachmentID, "err", err)
		return "", apierr.Invalid("MAIL_PREVIEW_CONVERT_FAILED", "这个文件无法转换预览，请下载查看")
	}
	if err := s.files.Put(ctx, key, bytes.NewReader(pdf), int64(len(pdf)), "application/pdf"); err != nil {
		// 存不下不等于看不了：这一次照样把结果给出去，只是下次还得再转一遍。
		s.log.Warn("could not cache a converted preview", "key", key, "err", err)
	}
	return s.signPreviewPDF(ctx, key)
}

func (s *Service) signPreviewPDF(ctx context.Context, key string) (string, error) {
	url, err := s.files.PresignGetInline(ctx, key, "application/pdf")
	if err != nil {
		return "", apierr.Internal("MAIL_PREVIEW_FAILED", "无法生成预览地址")
	}
	return url, nil
}

var errTooLarge = errors.New("object exceeds the cap")

// readCapped 读一个对象，最多读 cap 字节。
//
// 库里那个 file_size 是入库时记下的一个数，不是对象存储此刻的事实。**上限
// 必须由读这一侧执行**：只信 file_size 的话，一行写着 1 KB、对象却有 100 MB
// 的附件会被整个读进内存——这个服务同时在跑所有公司的收信。
//
// 多读一个字节来分辨「正好到上限」和「超了」，不然 25 MB 整的文件会被误判。
func (s *Service) readCapped(ctx context.Context, key string, cap int64) ([]byte, error) {
	rc, err := s.files.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(io.LimitReader(rc, cap+1)); err != nil {
		return nil, err
	}
	if int64(buf.Len()) > cap {
		return nil, errTooLarge
	}
	return buf.Bytes(), nil
}
