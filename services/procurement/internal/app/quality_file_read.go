package app

import (
	"context"
	"fmt"
	"io"
	"mime"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
)

// Match the upload limit; do not trust the browser-supplied size in the row.
const MaxQualityFileBytes = 64 << 20

type QualityFileContent struct {
	FileName, ContentType string
	Data                  []byte
}

type qualityObjectReader interface {
	OpenQualityObject(context.Context, string) (io.ReadCloser, error)
}

// ReadQualityFile is called after the grpc adapter authorizes the task or its
// enclosing order. No object key or signed URL comes from the HTTP caller.
func (s *Service) ReadQualityFile(ctx context.Context, tenantID, taskID, fileID, poID int64) (QualityFileContent, error) {
	var out QualityFileContent
	var key string
	err := s.pool.QueryRow(ctx, `SELECT f.object_key,f.file_name,f.content_type
		FROM quality_inspection_files f JOIN quality_inspection_tasks t
		  ON t.tenant_id=f.tenant_id AND t.id=f.task_id
		WHERE f.tenant_id=$1 AND f.task_id=$2 AND f.id=$3 AND ($4=0 OR t.po_id=$4)`,
		tenantID, taskID, fileID, poID).Scan(&key, &out.FileName, &out.ContentType)
	if err == pgx.ErrNoRows {
		return out, apierr.NotFound("QUALITY_FILE_NOT_FOUND", "质检附件不存在")
	}
	if err != nil {
		return out, err
	}
	if mediaType, _, err := mime.ParseMediaType(out.ContentType); err == nil {
		out.ContentType = mediaType
	} else {
		out.ContentType = "application/octet-stream"
	}
	reader, ok := s.files.(qualityObjectReader)
	if !ok {
		return out, apierr.Conflict("QUALITY_FILES_UNAVAILABLE", "附件服务暂不可用")
	}
	stream, err := reader.OpenQualityObject(ctx, key)
	if err != nil {
		return out, err
	}
	defer stream.Close()
	data, err := io.ReadAll(io.LimitReader(stream, MaxQualityFileBytes+1))
	if err != nil {
		return out, fmt.Errorf("read quality attachment %d: %w", fileID, err)
	}
	if len(data) > MaxQualityFileBytes {
		return out, apierr.Invalid("QUALITY_FILE_TOO_LARGE", "附件超过 64 MB，无法读取")
	}
	out.Data = data
	return out, nil
}
