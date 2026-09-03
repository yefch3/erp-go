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
	"github.com/sgao19/erp-go/services/export/internal/store"
)

// Files is object storage. Bytes never pass through this service: the browser
// PUTs straight to storage with a signed URL, and only the record comes here.
type Files interface {
	PresignPut(ctx context.Context, key string) (url string, expires int32, err error)
	PresignGet(ctx context.Context, key string) (string, error)
}

// FileView is a stored file plus a link to fetch it.
type FileView struct {
	Row         store.ListContractAttachmentsRow
	DownloadURL string
}

// fileKinds are the roles a contract document can play. DRAFT is what we sent
// out, SIGNED is what came back with a signature on it.
var fileKinds = map[string]bool{"DRAFT": true, "SIGNED": true, "OTHER": true}

// File sources. A person uploading a scan and an e-signature platform posting
// a completed envelope both produce a SIGNED file, but they are not equally
// good evidence, so the two are recorded apart and shown apart.
const (
	// SourceManual is what every authenticated upload is, without exception.
	SourceManual = "MANUAL"
	// SourcePlatform is reserved for the e-signature webhook. Nothing on the
	// REST path may set it - see RegisterContractFile.
	SourcePlatform = "PLATFORM"
)

// PresignContractFile hands back a key and a URL to PUT the bytes to. Nothing
// is recorded yet: an upload that never finishes leaves no row, only an
// orphaned object a later sweep can collect.
func (s *Service) PresignContractFile(ctx context.Context, tenantID, contractID int64, fileName, contentType string, op Operator) (key, url string, expires int32, err error) {
	if fileName == "" {
		return "", "", 0, apierr.Invalid("EX_FILE_NAME_REQUIRED", "文件名必填")
	}
	// A signed upload URL is a write, so it is gated like one: the person
	// asking for it must be allowed to attach paper to this contract.
	view, err := s.GetContract(ctx, tenantID, contractID, 0)
	if err != nil {
		return "", "", 0, err
	}
	if err := s.mustOwnContract(ctx, op, view); err != nil {
		return "", "", 0, err
	}
	key = contractObjectKey(tenantID, contractID, fileName)
	url, expires, err = s.files.PresignPut(ctx, key)
	if err != nil {
		return "", "", 0, err
	}
	return key, url, expires, nil
}

// contractObjectKey namespaces by tenant and contract so one look at the
// bucket says what an object belongs to, and a random prefix keeps two files
// of the same name from overwriting each other.
func contractObjectKey(tenantID, contractID int64, fileName string) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	base := path.Base(strings.ReplaceAll(fileName, "\\", "/"))
	return fmt.Sprintf("contracts/%d/%d/%s-%s", tenantID, contractID, hex.EncodeToString(buf), base)
}

// RegisterContractFile records an object the browser has already uploaded.
func (s *Service) RegisterContractFile(ctx context.Context, tenantID, contractID int64, in FileInput, op Operator) (FileView, error) {
	if in.Key == "" || in.FileName == "" {
		return FileView{}, apierr.Invalid("EX_FILE_KEY_REQUIRED", "文件标识和文件名必填")
	}
	// Without this a caller could register a key belonging to another
	// contract and read somebody else's paperwork through our own list.
	if !strings.HasPrefix(in.Key, fmt.Sprintf("contracts/%d/%d/", tenantID, contractID)) {
		return FileView{}, apierr.Invalid("EX_FILE_KEY_MISMATCH", "文件标识与合同不匹配")
	}
	kind := in.Kind
	if kind == "" {
		kind = "OTHER"
	}
	if !fileKinds[kind] {
		return FileView{}, apierr.Invalid("EX_FILE_KIND_INVALID", "文件类型不支持").
			WithMeta("kind", kind)
	}
	view, err := s.GetContract(ctx, tenantID, contractID, 0)
	if err != nil {
		return FileView{}, err
	}
	if err := s.mustOwnContract(ctx, op, view); err != nil {
		return FileView{}, err
	}
	// Default to the version being worked on: that is the paper this file is
	// almost always about.
	versionID := in.VersionID
	if versionID == 0 {
		versionID = view.Version.ID
	}

	id, err := s.q.CreateContractAttachment(ctx, store.CreateContractAttachmentParams{
		TenantID: tenantID, ContractID: contractID, ContractVersionID: versionID,
		Kind: kind, FileName: in.FileName, FileKey: in.Key,
		ContentType: in.ContentType, SizeBytes: in.Size,
		UploadedBy: op.ID, UploaderName: op.Name,
		// Hardcoded, never read from the request. An employee holding a scan
		// and an e-signature platform posting a completed envelope must stay
		// distinguishable, and a field the client can set is not a
		// distinction - it is a suggestion.
		Source: SourceManual,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return FileView{}, apierr.Conflict("EX_FILE_ALREADY_REGISTERED", "该文件已登记")
		}
		return FileView{}, err
	}
	if kind == "SIGNED" && view.Contract.EntrySource == "EXISTING_CONTRACT" {
		if err := s.q.ClearContractFilePending(ctx, store.ClearContractFilePendingParams{TenantID: tenantID, ID: contractID}); err != nil {
			return FileView{}, err
		}
	}
	row, err := s.q.GetContractAttachment(ctx, store.GetContractAttachmentParams{TenantID: tenantID, ID: id})
	if err != nil {
		return FileView{}, err
	}
	return s.withDownloadURL(ctx, store.ListContractAttachmentsRow(row)), nil
}

// FileInput is what the caller says about an object it just uploaded.
type FileInput struct {
	Key         string
	FileName    string
	ContentType string
	Size        int64
	Kind        string
	VersionID   int64
}

// ListContractFiles is scoped like reading the contract itself, approvers
// included: being asked to sign off on a deal without being able to open the
// contract that was attached to it would be approving blind.
func (s *Service) ListContractFiles(ctx context.Context, tenantID, contractID int64, op Operator) ([]FileView, error) {
	if _, err := s.GetContractFor(ctx, tenantID, contractID, 0, op); err != nil {
		return nil, err
	}
	rows, err := s.q.ListContractAttachments(ctx,
		store.ListContractAttachmentsParams{TenantID: tenantID, ContractID: contractID})
	if err != nil {
		return nil, err
	}
	out := make([]FileView, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.withDownloadURL(ctx, row))
	}
	return out, nil
}

// RemoveContractFile drops the record. The object itself is left in storage
// for a sweep to collect: deleting it here would make an accidental click
// unrecoverable, and a signed contract is not something to lose to one.
func (s *Service) RemoveContractFile(ctx context.Context, tenantID, id int64, op Operator) (bool, error) {
	// The attachment id says nothing about who owns it, so the contract it
	// hangs off has to be read before the row can be dropped.
	att, err := s.q.GetContractAttachment(ctx, store.GetContractAttachmentParams{TenantID: tenantID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, apierr.NotFound("EX_FILE_NOT_FOUND", "文件不存在")
		}
		return false, err
	}
	view, err := s.GetContract(ctx, tenantID, att.ContractID, 0)
	if err != nil {
		return false, err
	}
	if err := s.mustOwnContract(ctx, op, view); err != nil {
		return false, err
	}
	// The countersigned copy is why the contract is in force. Letting it be
	// deleted afterwards would leave an effective contract with nothing behind
	// it, which is exactly the state the upload requirement exists to prevent.
	if att.Kind == "SIGNED" && view.Contract.CurrentVersionID != 0 {
		return false, apierr.Conflict("EX_SIGNED_COPY_LOCKED",
			"合同已生效，签署件是生效依据，不能删除")
	}
	rows, err := s.q.DeleteContractAttachment(ctx,
		store.DeleteContractAttachmentParams{TenantID: tenantID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, apierr.NotFound("EX_FILE_NOT_FOUND", "文件不存在")
		}
		return false, err
	}
	if rows == 0 {
		return false, apierr.NotFound("EX_FILE_NOT_FOUND", "文件不存在")
	}
	return true, nil
}

func (s *Service) withDownloadURL(ctx context.Context, row store.ListContractAttachmentsRow) FileView {
	url, err := s.files.PresignGet(ctx, row.FileKey)
	if err != nil {
		// A missing link degrades the row to "listed but not downloadable",
		// which beats failing the whole list.
		return FileView{Row: row}
	}
	return FileView{Row: row, DownloadURL: url}
}
