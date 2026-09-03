package app

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// MaxZipBytes 是一封信的附件打包下载的总量上限。
//
// 生产上最大的一封是 23 MB，95% 在 3.8 MB 以内，所以 40 MB 目前是「全都装得
// 下」。这个数同时被网关的 gRPC 接收上限盯着（services/gateway/cmd/main.go）：
// 这边放宽了那边没放宽，超过 4 MB 的包会在网关变成一句看不懂的传输错误。两个
// 数要一起改。
const MaxZipBytes = 40 << 20

// AttachmentBundle 是打好的压缩包。
type AttachmentBundle struct {
	Content  []byte
	FileName string
}

// ZipInboundAttachments 把一封信的所有附件打成一个压缩包。
//
// 存在的理由很实在：生产上 835 封信带 3 个以上附件，最多的一封 40 个，一个一个
// 点下载不是办法。
//
// 权限和预览走同一条规矩：先按 tenant+id 取信、比对 owner，不是自己的信一律
// 「不存在」。这里不新增审计——单个附件的下载本来就不记（前端拿的是签名直链），
// 打包只是把十次点击并成一次，没有多给任何权限。真要记，该连单个下载一起记，
// 那是另一件事。
//
// 原件读不到的附件**跳过而不是失败**：一封信里有一个附件的对象丢了，不该让
// 另外九个也下载不了。跳过的会记进日志，包里也就少那一个。
func (s *Service) ZipInboundAttachments(
	ctx context.Context, tenantID, ownerID, inboundID int64,
) (AttachmentBundle, error) {
	if s.files == nil {
		return AttachmentBundle{}, apierr.Invalid("MAIL_ZIP_NO_STORAGE", "附件存储尚未配置")
	}
	row, err := s.q.GetInbound(ctx, store.GetInboundParams{TenantID: tenantID, ID: inboundID})
	if err != nil || row.OwnerID != ownerID {
		return AttachmentBundle{}, errNotFound()
	}
	atts, err := s.q.ListInboundAttachments(ctx, store.ListInboundAttachmentsParams{
		TenantID: tenantID, InboundID: inboundID,
	})
	if err != nil {
		return AttachmentBundle{}, apierr.Internal("MAIL_ZIP_FAILED", "无法读取附件列表")
	}

	// **包里的文件必须正好等于详情页列出来的文件**，所以这里走的是详情页那
	// 同一个函数、同一份数据，而不是另写一遍判断。
	//
	// 差别是有代价的：正文里已经显示过的内嵌图不算附件（不然「下载全部」会
	// 多出一堆签名档小图标），但 hideEmbedded 还要求那张图**真的解析成功**
	// 才隐藏——解析失败的仍然列在页面上，那它也该在包里。少了这半个条件，
	// 页面上看得见的文件会从包里消失，而且不响。
	all := make([]Attachment, 0, len(atts))
	for _, a := range atts {
		all = append(all, Attachment{
			ID: a.ID, FileName: a.FileName, ContentType: a.ContentType,
			FileSize: a.FileSize, FileKey: a.FileKey, ContentID: a.ContentID,
		})
	}
	listed := make([]Attachment, 0, len(all))
	for _, a := range hideEmbedded(all, row.BodyHtml, s.embeddedSwap(ctx, tenantID, inboundID)) {
		if a.FileKey == "" {
			continue
		}
		listed = append(listed, a)
	}
	if len(listed) == 0 {
		return AttachmentBundle{}, apierr.Invalid("MAIL_ZIP_NO_FILES", "这封邮件没有可下载的附件")
	}

	// 先按库里记的大小拦一道，省得读了 40 MB 才发现装不下。真正的上限在下面
	// 按读到的字节算——file_size 是入库时记的一个数，不是此刻的事实。
	var declared int64
	names := make([]string, 0, len(listed))
	for _, a := range listed {
		declared += a.FileSize
		names = append(names, a.FileName)
	}
	if declared > MaxZipBytes {
		return AttachmentBundle{}, errZipTooLarge()
	}
	entries := zipEntryNames(names)

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	var total int64
	packed := 0
	for i, a := range listed {
		data, err := s.readCapped(ctx, a.FileKey, MaxZipBytes-total)
		if err != nil {
			if errors.Is(err, errTooLarge) {
				return AttachmentBundle{}, errZipTooLarge()
			}
			// 这一个的原件读不到，别连累其余的。
			s.log.Warn("could not read an attachment for the bundle",
				"mail", inboundID, "file", a.FileName, "err", err)
			continue
		}
		total += int64(len(data))
		f, err := w.CreateHeader(&zip.FileHeader{
			Name: entries[i],
			// 存进去不再压一遍：附件绝大多数是 xlsx/docx/pdf/jpg，本身已经压
			// 过，再压一次省不下几个字节，却要为 40 MB 付一遍 CPU。
			Method:   zip.Store,
			Modified: time.Now(),
		})
		if err != nil {
			return AttachmentBundle{}, apierr.Internal("MAIL_ZIP_FAILED", "打包失败")
		}
		if _, err := f.Write(data); err != nil {
			return AttachmentBundle{}, apierr.Internal("MAIL_ZIP_FAILED", "打包失败")
		}
		packed++
	}
	if err := w.Close(); err != nil {
		return AttachmentBundle{}, apierr.Internal("MAIL_ZIP_FAILED", "打包失败")
	}
	if packed == 0 {
		// 一个都没读到。空压缩包下载下来只会让人以为附件本来就是空的。
		return AttachmentBundle{}, apierr.Internal("MAIL_ZIP_UNREADABLE", "附件原件暂时读取不到，请稍后再试")
	}
	s.log.Info("attachment bundle built",
		"mail", inboundID, "files", packed, "skipped", len(listed)-packed, "bytes", buf.Len())
	return AttachmentBundle{Content: buf.Bytes(), FileName: bundleFileName(row.Subject, inboundID)}, nil
}

func errZipTooLarge() error {
	return apierr.Invalid("MAIL_ZIP_TOO_LARGE",
		fmt.Sprintf("附件总量超过 %d MB，请逐个下载", MaxZipBytes>>20))
}

// bundleFileName 是下载下来的压缩包叫什么。
//
// 用主题而不是邮件号：存到桌面上的一堆 zip 里，「附件-17312.zip」谁也认不出来。
// 主题来自发件人，所以要过一遍和压缩包内条目一样的清洗。
func bundleFileName(subject string, inboundID int64) string {
	name := strings.TrimSpace(subject)
	if name == "" {
		// 没主题的信是有的（自动回执、扫描件转发）。用邮件号，至少是唯一的。
		return fmt.Sprintf("邮件-%d-附件.zip", inboundID)
	}
	name = safeEntryName(name, 0)
	// 按**字符**截断，不按字节：按字节截会把一个汉字切成半个，名字尾巴变乱码。
	if r := []rune(name); len(r) > 60 {
		name = strings.TrimSpace(string(r[:60]))
	}
	return name + "-附件.zip"
}
