package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path"
	"strings"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

// The certificate file. The row records that a certificate exists — name,
// number, validity; the file is the paper itself, which is what a customer's
// auditor actually asks to see. The file_key column shipped with B4 but
// nothing could ever fill it: no upload path, no download path. This wires
// both, the same presign shape product/export/procurement already run.

// Files is the object storage the certificate scans live in. Optional: main
// always wires it, isolated tests may not, and a nil store degrades the file
// endpoints to a clean error instead of a panic.
type Files interface {
	PresignPut(ctx context.Context, key string) (url string, expires int32, err error)
	PresignGet(ctx context.Context, key string) (string, error)
	Remove(ctx context.Context, key string) error
}

// UseFiles hands the service its object storage. Same late-binding idiom as
// procurement's UseMatchTolerance: the constructor keeps its signature and
// every existing call site keeps compiling.
func (s *Service) UseFiles(f Files) { s.files = f }

// CertificateFilePresign is a short-lived upload capability.
type CertificateFilePresign struct {
	Key       string
	UploadURL string
	Expires   int32
}

// FactoryCertificateView is a stored certificate plus a freshly minted
// download URL. The URL is never persisted: it expires, the key does not.
type FactoryCertificateView struct {
	Row      store.FactoryCertificate
	FileURL  string
	FileName string
}

// PresignFactoryCertificateFile hands back a key and a URL the browser PUTs
// the scan to. Nothing is recorded yet — an upload that never completes (or
// a dialog cancelled after uploading) leaves only an orphan object a later
// sweep can collect, never a row.
func (s *Service) PresignFactoryCertificateFile(ctx context.Context, tenantID, factoryID int64, fileName string) (CertificateFilePresign, error) {
	if s.files == nil {
		return CertificateFilePresign{}, apierr.Conflict("MD_FILES_UNAVAILABLE", "文件存储未配置")
	}
	if strings.TrimSpace(fileName) == "" {
		return CertificateFilePresign{}, apierr.Invalid("MD_FILE_NAME_REQUIRED", "文件名必填")
	}
	if _, err := s.GetFactory(ctx, tenantID, factoryID); err != nil {
		return CertificateFilePresign{}, err
	}
	key := certificateObjectKey(tenantID, factoryID, fileName)
	url, expires, err := s.files.PresignPut(ctx, key)
	if err != nil {
		return CertificateFilePresign{}, err
	}
	return CertificateFilePresign{Key: key, UploadURL: url, Expires: expires}, nil
}

// validateCertificateFileKey refuses a key that this factory's presign could
// not have minted — otherwise a client could register another factory's (or
// another tenant's) object as this factory's credential.
func validateCertificateFileKey(tenantID, factoryID int64, key string) error {
	if key == "" {
		return nil // a certificate without a scan is a legal record
	}
	if !strings.HasPrefix(key, fmt.Sprintf("factory-certificates/%d/%d/", tenantID, factoryID)) {
		return apierr.Invalid("MD_FACTORY_CERT_KEY_MISMATCH", "文件标识与工厂不匹配")
	}
	return nil
}

// withFileURL degrades a failed presign to "listed but not downloadable",
// which beats failing the whole list.
func (s *Service) withFileURL(ctx context.Context, row store.FactoryCertificate) FactoryCertificateView {
	v := FactoryCertificateView{Row: row}
	if row.FileKey == "" {
		return v
	}
	v.FileName = certificateDisplayName(row.FileKey)
	if s.files != nil {
		if url, err := s.files.PresignGet(ctx, row.FileKey); err == nil {
			v.FileURL = url
		}
	}
	return v
}

// certificateObjectKey namespaces by tenant and factory so one glance at the
// bucket says what an object belongs to, and a random prefix keeps
// same-named uploads from colliding.
func certificateObjectKey(tenantID, factoryID int64, fileName string) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	base := path.Base(strings.ReplaceAll(fileName, "\\", "/"))
	return fmt.Sprintf("factory-certificates/%d/%d/%s-%s", tenantID, factoryID, hex.EncodeToString(buf), base)
}

// certificateDisplayName recovers the original file name from a key minted
// by certificateObjectKey; the random prefix is hex, so the first '-' is
// always the separator.
func certificateDisplayName(key string) string {
	base := path.Base(key)
	if _, name, ok := strings.Cut(base, "-"); ok {
		return name
	}
	return base
}
