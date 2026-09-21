package app

import (
	"context"
	"mime"
	"path/filepath"
	"strings"

	"github.com/sgao19/erp-go/pkg/apierr"
)

// 写信窗口里还没发出去的附件，也要能看一眼再发。
//
// 收到的附件有一行记录，"这是谁的"从那一行上读得出来（见 signDownloads 和
// OfficePreviewConfig）。**草稿的附件没有那一行**——浏览器把文件直接传进对象
// 存储，拿到一个 key，而登记要等到发送那一刻、和写入同一个事务里做。所以这
// 条路只能靠 key 本身来授权。
//
// 靠得住的原因：key 是 PresignAttachment 生成的，形状是
// mail-attachments/<公司号>/<32 位随机十六进制><扩展名>——公司号在里面，随机
// 段猜不出来。这也正是发送时 PendingAttachment.allowedFor 用的同一条界线，
// 一条界线两处用，不另立一套。
//
// 不给 "office" 那一档：在线 Office 的取件地址是按「哪封信的哪个附件」签出来
// 的（officeviewer.go 的 officeURLClaims），草稿的附件两样都还没有。Word 和
// PPT 在这里就只能下载——说实话好过给一个点了打不开的按钮。表格不受影响：
// 前端自己解得动，拿下载地址就能画出来。
const draftAttachmentPrefix = "mail-attachments/"

// DraftAttachmentRef 是浏览器手里的一个待发附件。
type DraftAttachmentRef struct {
	FileKey string
	// 原始文件名。key 里只留了扩展名，名字一直在浏览器手里——下载要拿它当
	// 保存名。它是展示用的，不参与授权。
	FileName string
}

// PreviewDraftAttachments 给这些待发附件签出下载和预览地址。
//
// 逐个尽力：一个取不到不该连累其余的（和 signDownloads 同样的取舍）。取不到
// 的那个地址留空，前端据此只显示"下载"或什么都不显示。
func (s *Service) PreviewDraftAttachments(
	ctx context.Context, tenantID int64, refs []DraftAttachmentRef,
) ([]Attachment, error) {
	if s.files == nil {
		return nil, apierr.Invalid("NT_STORAGE_UNAVAILABLE", "文件存储未配置")
	}
	out := make([]Attachment, 0, len(refs))
	for _, r := range refs {
		if !draftAttachmentKeyAllowed(r.FileKey, tenantID) {
			// 不报错、不解释是哪一条不合格：能走到这里的 key 要么是本人刚传
			// 的，要么是别人瞎猜的，后者不该从错误信息里学到东西。
			continue
		}
		a := Attachment{FileKey: r.FileKey, FileName: strings.TrimSpace(r.FileName)}

		// 大小和类型读存储里真实的那一份，不听调用方报的——和 RegisterAttachment
		// 同一个道理：调用方报的是说法，存储里的才是事实。
		if size, ct, err := s.files.Stat(ctx, r.FileKey); err == nil {
			a.FileSize, a.ContentType = size, ct
		} else {
			s.log.Warn("could not stat a draft attachment", "file", a.FileName, "err", err)
			continue
		}
		if url, err := s.files.PresignGet(ctx, r.FileKey, a.FileName); err == nil {
			a.DownloadURL = url
		} else {
			s.log.Warn("could not sign a draft attachment download", "file", a.FileName, "err", err)
		}

		// 预览是锦上添花，签不出来不该连下载一起赔进去——所以单独试、放最后。
		if ct := previewable(draftContentType(a.ContentType, a.FileName)); ct != "" {
			a.PreviewKind = PreviewDirect
			if pv, err := s.files.PresignGetInline(ctx, r.FileKey, ct); err == nil {
				a.PreviewURL = pv
			} else {
				s.log.Warn("could not sign a draft attachment preview", "file", a.FileName, "err", err)
			}
		}
		out = append(out, a)
	}
	return out, nil
}

// draftAttachmentKeyAllowed 是这条路的全部信任边界。
//
// 和发送那边 PendingAttachment.allowedFor 判的是同一件事：key 必须坐在本公司
// 的上传前缀底下。故意不放宽到 mail/inbound/——那条路径是按信箱编号排的，能
// 指名它的人就能指名同事的信箱。
func draftAttachmentKeyAllowed(key string, tenantID int64) bool {
	if key == "" {
		return false
	}
	// 不许用 .. 往上跳出自己的前缀。前缀比对本身挡得住绝大多数，这一条是
	// 防着有人用 mail-attachments/4/../../ 这种写法绕过去。
	if strings.Contains(key, "..") {
		return false
	}
	return strings.HasPrefix(key, draftAttachmentPrefix+itoa(int(tenantID))+"/")
}

// draftContentType 补一个类型。
//
// 对象存储里存的类型来自浏览器上传时报的那个，有些浏览器对 PDF 报的是
// application/octet-stream——那样一份 PDF 就白白失去了预览。类型说不清时按
// 文件名的扩展名再猜一次。
func draftContentType(stored, fileName string) string {
	if ct := previewable(stored); ct != "" {
		return ct
	}
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext == "" {
		return stored
	}
	if guessed := mime.TypeByExtension(ext); guessed != "" {
		return guessed
	}
	return stored
}
