package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/product/internal/store"
)

// AttachmentView is a stored attachment plus a freshly minted download URL.
// The URL is never persisted: it expires, the key does not.
type AttachmentView struct {
	Row         store.ProductAttachment
	DownloadURL string
}

// PresignUpload hands back a key and a URL the browser PUTs the file to.
// Nothing is recorded yet - an upload that never completes leaves no row,
// only an orphan object that a later sweep can collect.
func (s *Service) PresignUpload(ctx context.Context, tenantID, productID int64, fileName, contentType string) (key, url string, expires int32, err error) {
	if fileName == "" {
		return "", "", 0, apierr.Invalid("PD_FILE_NAME_REQUIRED", "文件名必填")
	}
	if _, err := s.GetProduct(ctx, tenantID, productID); err != nil {
		return "", "", 0, err
	}
	key = objectKey(tenantID, productID, fileName)
	url, expires, err = s.files.PresignPut(ctx, key)
	if err != nil {
		return "", "", 0, err
	}
	return key, url, expires, nil
}

// objectKey namespaces by tenant and product so one glance at the bucket says
// what an object belongs to, and a random suffix keeps same-named uploads
// from overwriting each other.
func objectKey(tenantID, productID int64, fileName string) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	base := path.Base(strings.ReplaceAll(fileName, "\\", "/"))
	return fmt.Sprintf("products/%d/%d/%s-%s", tenantID, productID, hex.EncodeToString(buf), base)
}

// Register records an object the browser already uploaded.
func (s *Service) Register(ctx context.Context, tenantID, productID int64, key, fileName string, size int64, contentType string, operatorID int64) (AttachmentView, error) {
	if key == "" || fileName == "" {
		return AttachmentView{}, apierr.Invalid("PD_FILE_KEY_REQUIRED", "文件标识和文件名必填")
	}
	// A client could otherwise register a key belonging to another product.
	if !strings.HasPrefix(key, fmt.Sprintf("products/%d/%d/", tenantID, productID)) {
		return AttachmentView{}, apierr.Invalid("PD_FILE_KEY_MISMATCH", "文件标识与产品不匹配")
	}
	if _, err := s.GetProduct(ctx, tenantID, productID); err != nil {
		return AttachmentView{}, err
	}
	row, err := s.q.CreateAttachment(ctx, store.CreateAttachmentParams{
		TenantID: tenantID, ProductID: productID, FileName: fileName, FileKey: key,
		FileSize: size, ContentType: contentType, UploadedBy: operatorID,
	})
	if err != nil {
		return AttachmentView{}, err
	}
	return s.withDownloadURL(ctx, row), nil
}

func (s *Service) ListAttachments(ctx context.Context, tenantID, productID int64) ([]AttachmentView, error) {
	rows, err := s.q.ListAttachments(ctx, store.ListAttachmentsParams{TenantID: tenantID, ProductID: productID})
	if err != nil {
		return nil, err
	}
	out := make([]AttachmentView, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.withDownloadURL(ctx, r))
	}
	return out, nil
}

// RemoveAttachment drops the row first and the object second: a missing
// object with no row is invisible, a row pointing at nothing is a broken link.
func (s *Service) RemoveAttachment(ctx context.Context, tenantID, id int64) error {
	row, err := s.q.GetAttachment(ctx, store.GetAttachmentParams{TenantID: tenantID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("PD_FILE_NOT_FOUND", "附件不存在")
		}
		return err
	}
	rows, err := s.q.DeleteAttachment(ctx, store.DeleteAttachmentParams{TenantID: tenantID, ID: id})
	if err != nil {
		return err
	}
	if rows == 0 {
		return apierr.NotFound("PD_FILE_NOT_FOUND", "附件不存在")
	}
	if err := s.files.Remove(ctx, row.FileKey); err != nil {
		// The row is already gone, so the user's view is correct; the object
		// is garbage that a sweep can collect. Do not fail the request.
		return nil
	}
	return nil
}

func (s *Service) withDownloadURL(ctx context.Context, row store.ProductAttachment) AttachmentView {
	url, err := s.files.PresignGet(ctx, row.FileKey)
	if err != nil {
		// A missing URL degrades the row to "listed but not downloadable",
		// which beats failing the whole list.
		return AttachmentView{Row: row}
	}
	return AttachmentView{Row: row, DownloadURL: url}
}
