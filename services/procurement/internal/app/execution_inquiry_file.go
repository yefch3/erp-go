package app

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
)

const maxExecutionInquiryFile = 10 << 20

type ExecutionInquiryFile struct {
	ID, RequirementID, SizeBytes, UploadedByID        int64
	FileName, ContentType, UploadedByName, UploadedAt string
}

func (s *Service) ListExecutionInquiryFiles(ctx context.Context, tenantID, requirementID int64) ([]ExecutionInquiryFile, error) {
	rows, err := s.pool.Query(ctx, `
		WITH current_requirement AS (
			SELECT contract_id FROM purchase_requirements WHERE tenant_id=$1 AND id=$2
		)
		SELECT f.id,f.requirement_id,f.file_name,f.content_type,f.size_bytes,f.uploaded_by_id,f.uploaded_by_name,f.uploaded_at::text
		FROM purchase_execution_inquiry_files f
		JOIN purchase_requirements source_requirement ON source_requirement.tenant_id=f.tenant_id AND source_requirement.id=f.requirement_id
		JOIN current_requirement current ON TRUE
		WHERE f.tenant_id=$1
		  AND (f.requirement_id=$2 OR (current.contract_id>0 AND source_requirement.contract_id=current.contract_id))
		ORDER BY f.uploaded_at DESC,f.id DESC`, tenantID, requirementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExecutionInquiryFile{}
	for rows.Next() {
		var f ExecutionInquiryFile
		if err := rows.Scan(&f.ID, &f.RequirementID, &f.FileName, &f.ContentType, &f.SizeBytes, &f.UploadedByID, &f.UploadedByName, &f.UploadedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Service) UploadExecutionInquiryFile(ctx context.Context, tenantID, requirementID int64, name, contentType string, data []byte, op Operator) (ExecutionInquiryFile, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(data) == 0 {
		return ExecutionInquiryFile{}, apierr.Invalid("EXECUTION_FILE_REQUIRED", "请选择需要上传的资料")
	}
	if len(data) > maxExecutionInquiryFile {
		return ExecutionInquiryFile{}, apierr.Invalid("EXECUTION_FILE_TOO_LARGE", "单个资料不能超过 10 MB")
	}
	if _, err := s.GetRequirement(ctx, tenantID, requirementID); err != nil {
		return ExecutionInquiryFile{}, err
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO purchase_execution_inquiry_files(tenant_id,requirement_id,file_name,content_type,size_bytes,file_data,uploaded_by_id,uploaded_by_name) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, tenantID, requirementID, name, contentType, len(data), data, op.ID, op.Name).Scan(&id)
	if err != nil {
		return ExecutionInquiryFile{}, err
	}
	files, err := s.ListExecutionInquiryFiles(ctx, tenantID, requirementID)
	if err != nil {
		return ExecutionInquiryFile{}, err
	}
	for _, f := range files {
		if f.ID == id {
			return f, nil
		}
	}
	return ExecutionInquiryFile{}, apierr.NotFound("EXECUTION_FILE_NOT_FOUND", "资料不存在")
}

func (s *Service) DownloadExecutionInquiryFile(ctx context.Context, tenantID, requirementID, id int64) (string, string, []byte, error) {
	var name, contentType string
	var data []byte
	err := s.pool.QueryRow(ctx, `
		WITH current_requirement AS (
			SELECT contract_id FROM purchase_requirements WHERE tenant_id=$1 AND id=$2
		)
		SELECT f.file_name,f.content_type,f.file_data
		FROM purchase_execution_inquiry_files f
		JOIN purchase_requirements source_requirement ON source_requirement.tenant_id=f.tenant_id AND source_requirement.id=f.requirement_id
		JOIN current_requirement current ON TRUE
		WHERE f.tenant_id=$1 AND f.id=$3
		  AND (f.requirement_id=$2 OR (current.contract_id>0 AND source_requirement.contract_id=current.contract_id))`, tenantID, requirementID, id).Scan(&name, &contentType, &data)
	if err == pgx.ErrNoRows {
		return "", "", nil, apierr.NotFound("EXECUTION_FILE_NOT_FOUND", "资料不存在")
	}
	return name, contentType, data, err
}

func (s *Service) DeleteExecutionInquiryFile(ctx context.Context, tenantID, requirementID, id int64) error {
	cmd, err := s.pool.Exec(ctx, `
		DELETE FROM purchase_execution_inquiry_files f
		USING purchase_requirements source_requirement,purchase_requirements current_requirement
		WHERE f.tenant_id=$1 AND f.id=$3
		  AND source_requirement.tenant_id=f.tenant_id AND source_requirement.id=f.requirement_id
		  AND current_requirement.tenant_id=$1 AND current_requirement.id=$2
		  AND (f.requirement_id=$2 OR (current_requirement.contract_id>0 AND source_requirement.contract_id=current_requirement.contract_id))`, tenantID, requirementID, id)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return apierr.NotFound("EXECUTION_FILE_NOT_FOUND", "资料不存在")
	}
	return nil
}
