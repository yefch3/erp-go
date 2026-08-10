package app

import (
	"bytes"
	"context"
	"net/url"
	"path"
	"strings"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// ImportImageFromURL copies a picture that already lives on a web server into
// our own storage and registers it like any uploaded one.
//
// Why copy rather than link. The obvious implementation of "insert a URL" is
// to write that address straight into the signature and be done. It works on
// the day it is set up and fails quietly afterwards: a signature is attached
// to every mail for years, and the marketing site it borrowed the logo from
// gets redesigned, moves host, or turns on hotlink protection. What the
// customer then sees is a broken-image icon at the bottom of every message —
// and nobody in the company sees it, because in the composer it still looks
// right. The same reasoning already decided how inbound pictures are handled;
// this is the outbound half of it.
//
// Copying also means the recipient's mail client only ever talks to us, so
// inserting somebody's logo does not quietly report our customers' opens to
// a third party.
//
// The fetch reuses the guarded client the inbound cache uses: an address the
// employee types is exactly as untrusted as one a stranger mailed us, and
// without the guard this endpoint is a machine inside the network fetching
// any URL on request — including the cloud metadata service.
func (s *Service) ImportImageFromURL(
	ctx context.Context, tenantID int64, rawURL string, op Operator,
) (Image, error) {
	if s.files == nil {
		return Image{}, apierr.Invalid("NT_STORAGE_UNAVAILABLE", "文件存储未配置")
	}
	u, err := parseImageURL(rawURL)
	if err != nil {
		return Image{}, err
	}
	img, err := fetchOneImage(ctx, imageClient(), u.String())
	if err != nil {
		// The reason is logged, not returned: it comes from somebody else's
		// server and can carry their internal hostnames. What the person
		// needs is the one instruction that always works.
		s.log.Info("could not import a picture from a URL", "host", u.Host, "err", err)
		return Image{}, apierr.Invalid("NT_IMAGE_FETCH_FAILED",
			"打不开这个图片地址。可以把图片存到本地，再用上传的方式插入")
	}
	// The cache path allows 3 MB and a wider type list because it is storing
	// whatever a stranger's mail already contains. This is our own outgoing
	// mail, so it holds to the same 2 MB and the same four types as an upload
	// — one rule for a signature picture, however it got here.
	if !imageTypes[img.contentType] {
		return Image{}, apierr.Invalid("NT_IMAGE_TYPE", "只支持 PNG / JPEG / GIF / WebP 图片")
	}
	if int64(len(img.data)) > MaxImageBytes {
		return Image{}, apierr.Invalid("NT_IMAGE_TOO_LARGE",
			"图片不能超过 2 MB——邮件里的 logo 远小于这个尺寸")
	}
	fileName := imageNameFromURL(u, img.contentType)
	key, err := objectKey("mail-images", tenantID, fileName)
	if err != nil {
		return Image{}, err
	}
	if err := s.files.Put(ctx, key,
		bytes.NewReader(img.data), int64(len(img.data)), img.contentType); err != nil {
		return Image{}, err
	}
	token, err := randomToken()
	if err != nil {
		return Image{}, err
	}
	id, err := s.q.AddImage(ctx, store.AddImageParams{
		TenantID: tenantID, Token: token, FileName: fileName, FileKey: key,
		FileSize: int64(len(img.data)), ContentType: img.contentType, UploadedBy: op.ID,
	})
	if err != nil {
		// An object no row points at is unreachable and uncountable, so it
		// goes rather than lingering in the bucket.
		_ = s.files.Remove(ctx, key)
		return Image{}, err
	}
	return Image{ID: id, Token: token, FileName: fileName,
		FileSize: int64(len(img.data)), ContentType: img.contentType}, nil
}

// parseImageURL accepts only what a picture on the public web can be.
//
// The scheme check is not the security boundary — the dialler's address check
// is, and it runs again on every redirect. This one exists so that a typo
// gets a sentence instead of a timeout.
func parseImageURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, apierr.Invalid("NT_IMAGE_URL_REQUIRED", "请填写图片地址")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, apierr.Invalid("NT_IMAGE_URL_INVALID", "图片地址无效")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, apierr.Invalid("NT_IMAGE_URL_SCHEME", "只支持 http、https 开头的图片地址")
	}
	return u, nil
}

// imageNameFromURL is what the picker will label the thumbnail with, and what
// ends up in the alt text of the inserted tag. Derived from the address
// because that is the only name there is, and corrected to match the bytes
// that actually arrived: plenty of logos are served from a path with no
// extension, or with one that disagrees with the content.
func imageNameFromURL(u *url.URL, contentType string) string {
	name := path.Base(u.Path)
	if name == "." || name == "/" || name == "" {
		// A host is not a file name, so nothing is stripped from it: trimming
		// what path.Ext finds would turn cdn.example.com into cdn.example.
		name = u.Hostname()
	} else {
		name = strings.TrimSuffix(name, path.Ext(name))
	}
	// Keep it short enough to read in the picker and to fit the column.
	if len(name) > 60 {
		name = name[:60]
	}
	if strings.TrimSpace(name) == "" {
		name = "image"
	}
	return name + extensionFor(contentType)
}
