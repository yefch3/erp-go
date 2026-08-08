package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"path"
	"strings"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// Size caps. Not arbitrary: recipient servers commonly refuse a message much
// over 25 MB, and the refusal comes back as a bounce per recipient rather
// than as one error while composing. Better to say no here.
const (
	MaxAttachmentBytes = 10 << 20 // 10 MB per file
	MaxCampaignBytes   = 20 << 20 // 20 MB per send, under the usual 25 MB wall
	MaxImageBytes      = 2 << 20  // 2 MB — a logo far exceeding this is a mistake
)

// Files is object storage. Bytes go straight from the browser to the bucket
// through a presigned URL, so they never occupy the gateway.
type Files interface {
	PresignPut(ctx context.Context, key string) (string, int32, error)
	// PresignGet is the read side, with the name the browser should save the
	// file under: object keys are random, so without it a customer's quotation
	// downloads as a hex string with an extension.
	PresignGet(ctx context.Context, key, saveAs string) (string, error)
	// PresignGetInline is the same read asking the browser to render rather
	// than save. Only ever called with a content type this service has decided
	// is safe to display.
	PresignGetInline(ctx context.Context, key, contentType string) (string, error)
	// Stat reads what is actually stored. The size a client reports after
	// uploading is a claim; without this the caps would be advisory only.
	Stat(ctx context.Context, key string) (int64, string, error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Remove(ctx context.Context, key string) error
	// Put writes bytes we already hold. The outbound path never needs this —
	// browsers upload straight to storage with a presigned URL — but incoming
	// mail arrives through us, so its raw MIME and attachments are ours to
	// store.
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
}

// Attachment is one file travelling with a send.
type Attachment struct {
	ID          int64
	FileName    string
	FileKey     string
	FileSize    int64
	ContentType string
	// Short-lived, signed, and carrying the original file name. Empty when
	// storage is unreachable or the file was never stored — the caller shows
	// the attachment either way, because "there was a file called this" is
	// still true and worth knowing.
	DownloadURL string
	// Set only for the handful of types worth looking at without leaving the
	// page, and safe to render from the storage origin. Empty otherwise, which
	// is how the UI knows not to offer a preview it cannot honour.
	PreviewURL string
	// The name the body points at when it embeds this part inline. Not sent to
	// the client: it exists so the reader can drop the parts the body has
	// already shown, which is how a signature logo stops being listed beside
	// the signed contract.
	ContentID string
}

// previewable decides what may be rendered inline.
//
// An allowlist, not a blocklist. The interesting types are images and PDFs —
// what somebody actually wants to glance at before deciding — and everything
// else is a download. text/html is deliberately absent: rendering a sender's
// markup, even on another origin, is running their code for no benefit, since
// nobody previews an .html attachment on purpose.
func previewable(contentType string) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch ct {
	case "application/pdf",
		"image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp":
		return ct
	}
	return ""
}

// signDownloads fills in the download URL for each attachment.
//
// Best effort per file: one unreachable object must not cost the caller the
// whole list, so a failure leaves that URL empty and the rest usable.
func (s *Service) signDownloads(ctx context.Context, atts []Attachment) []Attachment {
	if s.files == nil {
		return atts
	}
	for i, a := range atts {
		if a.FileKey == "" {
			continue
		}
		url, err := s.files.PresignGet(ctx, a.FileKey, a.FileName)
		if err != nil {
			s.log.Warn("could not sign an attachment download",
				"file", a.FileName, "err", err)
			continue
		}
		atts[i].DownloadURL = url

		// A preview is a nicety; failing to sign one must not cost the file
		// its download link, so it is attempted separately and last.
		if ct := previewable(a.ContentType); ct != "" {
			if pv, err := s.files.PresignGetInline(ctx, a.FileKey, ct); err == nil {
				atts[i].PreviewURL = pv
			} else {
				s.log.Warn("could not sign an attachment preview",
					"file", a.FileName, "err", err)
			}
		}
	}
	return atts
}

// PresignAttachment issues a short-lived upload URL for a file to be sent
// with a campaign.
//
// Campaign-scoped rather than message-scoped: every recipient of one send
// gets the same file, and a row per recipient would store the same key
// hundreds of times. What does scale with recipients is egress — a 5 MB file
// to 500 people is 2.5 GB through the provider — so the caller is told the
// running total when it registers.
func (s *Service) PresignAttachment(ctx context.Context, tenantID int64, fileName string) (key, url string, expires int32, err error) {
	if s.files == nil {
		return "", "", 0, apierr.Invalid("NT_STORAGE_UNAVAILABLE", "文件存储未配置")
	}
	if strings.TrimSpace(fileName) == "" {
		return "", "", 0, apierr.Invalid("NT_FILE_NAME_REQUIRED", "缺少文件名")
	}
	key, err = objectKey("mail-attachments", tenantID, fileName)
	if err != nil {
		return "", "", 0, err
	}
	url, expires, err = s.files.PresignPut(ctx, key)
	if err != nil {
		return "", "", 0, err
	}
	return key, url, expires, nil
}

// RegisterAttachment records a file the browser has already uploaded.
//
// The size is read back from storage rather than taken from the caller: the
// upload bypassed this service entirely, so a client is free to claim one
// number and store another. A cap checked against a claim is not a cap.
func (s *Service) RegisterAttachment(
	ctx context.Context, tenantID, campaignID int64, fileName, fileKey string, op Operator,
) (Attachment, error) {
	return s.registerAttachmentTx(ctx, s.q, tenantID, campaignID,
		PendingAttachment{FileName: fileName, FileKey: fileKey}, op)
}

// registerAttachmentTx is the same work against a caller-supplied queries
// handle, so it can run inside the transaction that creates a send.
func (s *Service) registerAttachmentTx(
	ctx context.Context, q *store.Queries, tenantID, campaignID int64,
	f PendingAttachment, op Operator,
) (Attachment, error) {
	fileName, fileKey := f.FileName, f.FileKey
	if s.files == nil {
		return Attachment{}, apierr.Invalid("NT_STORAGE_UNAVAILABLE", "文件存储未配置")
	}
	if !f.allowedFor(tenantID) {
		return Attachment{}, apierr.Invalid("NT_FILE_KEY_INVALID", "文件标识无效")
	}
	size, contentType, err := s.files.Stat(ctx, fileKey)
	if err != nil {
		return Attachment{}, apierr.Invalid("NT_FILE_NOT_UPLOADED", "文件尚未上传完成")
	}
	if size <= 0 {
		return Attachment{}, apierr.Invalid("NT_FILE_EMPTY", "文件为空")
	}
	// Rejecting an upload deletes it, because an object no row points at is an
	// orphan nobody can reach. A forward's file is the opposite case: it is
	// the inbox's copy, this send merely refers to it, and deleting it here
	// would destroy the received mail's attachment because the forward of it
	// was too big to send.
	discard := func() {
		if !f.serverDerived {
			_ = s.files.Remove(ctx, fileKey)
		}
	}
	if size > MaxAttachmentBytes {
		discard()
		return Attachment{}, apierr.Invalid("NT_FILE_TOO_LARGE", "单个附件不能超过 10 MB")
	}
	used, err := q.SumAttachmentSize(ctx, store.SumAttachmentSizeParams{
		TenantID: tenantID, CampaignID: campaignID,
	})
	if err != nil {
		return Attachment{}, err
	}
	if used+size > MaxCampaignBytes {
		discard()
		return Attachment{}, apierr.Invalid("NT_ATTACHMENTS_TOO_LARGE",
			"这封邮件的附件总大小超过 20 MB，多数邮件服务器会拒收")
	}

	id, err := q.AddAttachment(ctx, store.AddAttachmentParams{
		TenantID: tenantID, CampaignID: campaignID, FileName: fileName,
		FileKey: fileKey, FileSize: size, ContentType: contentType,
		UploadedBy: op.ID,
	})
	if err != nil {
		discard()
		return Attachment{}, err
	}
	return Attachment{ID: id, FileName: fileName, FileKey: fileKey,
		FileSize: size, ContentType: contentType}, nil
}

func (s *Service) ListAttachments(ctx context.Context, tenantID, campaignID int64) ([]store.ListAttachmentsRow, error) {
	return s.q.ListAttachments(ctx, store.ListAttachmentsParams{
		TenantID: tenantID, CampaignID: campaignID,
	})
}

// SignedAttachments is ListAttachments with download URLs, for the sent-mail
// view: a file somebody attached last week is exactly the file they come back
// looking for.
func (s *Service) SignedAttachments(ctx context.Context, tenantID, campaignID int64) ([]Attachment, error) {
	rows, err := s.ListAttachments(ctx, tenantID, campaignID)
	if err != nil {
		return nil, err
	}
	out := make([]Attachment, 0, len(rows))
	for _, r := range rows {
		out = append(out, Attachment{
			ID: r.ID, FileName: r.FileName, FileKey: r.FileKey,
			FileSize: r.FileSize, ContentType: r.ContentType,
		})
	}
	return s.signDownloads(ctx, out), nil
}

// ---------------------------------------------------------------- images

// Image is a picture referenced from inside an HTML body or signature.
type Image struct {
	ID          int64
	Token       string
	FileName    string
	FileSize    int64
	ContentType string
}

// imageTypes is a whitelist, not a "looks harmless" check. These objects are
// served from a public, unauthenticated route, so only formats mail clients
// actually render are allowed through.
var imageTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

func (s *Service) PresignImage(ctx context.Context, tenantID int64, fileName string) (key, url string, expires int32, err error) {
	if s.files == nil {
		return "", "", 0, apierr.Invalid("NT_STORAGE_UNAVAILABLE", "文件存储未配置")
	}
	key, err = objectKey("mail-images", tenantID, fileName)
	if err != nil {
		return "", "", 0, err
	}
	url, expires, err = s.files.PresignPut(ctx, key)
	if err != nil {
		return "", "", 0, err
	}
	return key, url, expires, nil
}

// RegisterImage records an uploaded picture and mints the token its public
// URL uses.
//
// The URL has to be permanent, not presigned: a recipient may open the mail
// weeks later, by which time a presigned link has expired and every mail
// already sent shows a broken image. So the token is the credential, and it
// lasts until somebody withdraws it.
func (s *Service) RegisterImage(
	ctx context.Context, tenantID int64, fileName, fileKey string, op Operator,
) (Image, error) {
	if s.files == nil {
		return Image{}, apierr.Invalid("NT_STORAGE_UNAVAILABLE", "文件存储未配置")
	}
	if !strings.HasPrefix(fileKey, "mail-images/"+itoa(int(tenantID))+"/") {
		return Image{}, apierr.Invalid("NT_FILE_KEY_INVALID", "文件标识无效")
	}
	size, contentType, err := s.files.Stat(ctx, fileKey)
	if err != nil {
		return Image{}, apierr.Invalid("NT_FILE_NOT_UPLOADED", "文件尚未上传完成")
	}
	if !imageTypes[strings.ToLower(contentType)] {
		_ = s.files.Remove(ctx, fileKey)
		return Image{}, apierr.Invalid("NT_IMAGE_TYPE", "只支持 PNG / JPEG / GIF / WebP 图片")
	}
	if size <= 0 || size > MaxImageBytes {
		_ = s.files.Remove(ctx, fileKey)
		return Image{}, apierr.Invalid("NT_IMAGE_TOO_LARGE",
			"图片不能超过 2 MB——邮件里的 logo 远小于这个尺寸")
	}
	token, err := randomToken()
	if err != nil {
		return Image{}, err
	}
	id, err := s.q.AddImage(ctx, store.AddImageParams{
		TenantID: tenantID, Token: token, FileName: fileName, FileKey: fileKey,
		FileSize: size, ContentType: contentType, UploadedBy: op.ID,
	})
	if err != nil {
		_ = s.files.Remove(ctx, fileKey)
		return Image{}, err
	}
	return Image{ID: id, Token: token, FileName: fileName,
		FileSize: size, ContentType: contentType}, nil
}

// OpenImage resolves a public token to the stored bytes.
//
// Deliberately not tenant-scoped: the caller is a recipient's mail client
// with no session of any kind. The token is the whole credential, which is
// why it is random and why withdrawal has to work.
func (s *Service) OpenImage(ctx context.Context, token string) ([]byte, string, error) {
	row, err := s.q.ResolveImage(ctx, token)
	if err != nil {
		return nil, "", apierr.NotFound("NT_IMAGE_NOT_FOUND", "图片不存在")
	}
	rc, err := s.files.Get(ctx, row.FileKey)
	if err != nil {
		return nil, "", err
	}
	defer rc.Close()
	// Bounded by the 2 MB cap enforced at registration, so reading it whole
	// is safe; LimitReader is the belt to that braces.
	b, err := io.ReadAll(io.LimitReader(rc, MaxImageBytes+1))
	if err != nil {
		return nil, "", err
	}
	return b, row.ContentType, nil
}

func (s *Service) ListImages(ctx context.Context, tenantID int64) ([]store.ListImagesRow, error) {
	return s.q.ListImages(ctx, tenantID)
}

func (s *Service) WithdrawImage(ctx context.Context, tenantID, id int64) error {
	n, err := s.q.WithdrawImage(ctx, store.WithdrawImageParams{TenantID: tenantID, ID: id})
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.NotFound("NT_IMAGE_NOT_FOUND", "图片不存在")
	}
	// The object stays. Mails already sent still reference it, and deleting
	// the bytes would turn every past mail's logo into a broken image
	// retroactively; withdrawing only stops it being served from now on.
	return nil
}

// ---------------------------------------------------------------- helpers

// objectKey namespaces by tenant and randomises the name. The caller's file
// name contributes only its extension: a name like "../../etc/passwd" has
// nothing else worth keeping.
func objectKey(prefix string, tenantID int64, name string) (string, error) {
	id, err := randomToken()
	if err != nil {
		return "", err
	}
	ext := strings.ToLower(path.Ext(path.Base(name)))
	if len(ext) > 8 || strings.ContainsAny(ext, `/\`) {
		ext = ""
	}
	return prefix + "/" + itoa(int(tenantID)) + "/" + id + ext, nil
}

func randomToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
