package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"net/url"
	"path"
	"strings"
)

func (s *Service) inquiryUpload(ctx context.Context, tenant int64, op Operator, in InquiryCommand) (InquiryResult, error) {
	if s.files == nil {
		return InquiryResult{}, apierr.Conflict("INQUIRY_FILES", "附件服务未连接")
	}
	if len(in.FileData) == 0 || len(in.FileData) > 8*1024*1024 || strings.TrimSpace(in.FileName) == "" {
		return InquiryResult{}, apierr.Invalid("INQUIRY_FILE", "请选择不超过 8MB 的文件")
	}
	if in.View == "QUOTATIONS" {
		return InquiryResult{}, apierr.Permission("INQUIRY_FILE", "销售报价接收页只读")
	}
	v, e := s.readInquiry(ctx, tenant, inquiryID(in.ID), op, in.View)
	if e != nil {
		return InquiryResult{}, e
	}
	if in.View == "SALES" && (!v.CanEdit || v.State == "INQUIRING") {
		return InquiryResult{}, apierr.Permission("INQUIRY_FILE", "当前询盘不能修改附件")
	}
	token := make([]byte, 16)
	if _, e = rand.Read(token); e != nil {
		return InquiryResult{}, e
	}
	key := fmt.Sprintf("inquiry/%d/%s/%s", tenant, in.ID, hex.EncodeToString(token))
	name := path.Base(strings.ReplaceAll(in.FileName, "\\", "/"))
	e = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var revision int64
		if e := tx.QueryRow(ctx, `SELECT inquiry_revision FROM sourcing_cases WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, inquiryID(in.ID)).Scan(&revision); e != nil {
			return e
		}
		if revision != in.Revision {
			return apierr.Conflict("INQUIRY_CHANGED", "询盘已变化，请刷新")
		}
		if e := s.files.Put(ctx, key, in.FileData, "application/octet-stream"); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `INSERT INTO inquiry_attachments(tenant_id,case_id,revision,kind,object_key,name,created_by) VALUES($1,$2,$3,$4,$5,$6,$7)`, tenant, inquiryID(in.ID), revision, in.View, key, name, op.ID)
		return e
	})
	if e != nil {
		_ = s.files.Remove(ctx, key)
		return InquiryResult{}, e
	}
	return InquiryResult{Attachment: &InquiryAttachment{Key: key, Name: name}}, nil
}
func (s *Service) inquiryDownload(ctx context.Context, tenant int64, op Operator, in InquiryCommand) (InquiryResult, error) {
	v, e := s.readInquiry(ctx, tenant, inquiryID(in.ID), op, in.View)
	if e != nil {
		return InquiryResult{}, e
	}
	allowed := false
	name := ""
	for _, a := range v.Body.Attachments {
		if a.Key == in.FileKey {
			allowed = true
			name = a.Name
		}
	}
	for _, q := range v.Quotes {
		for _, a := range q.Body.Attachments {
			if a.Key == in.FileKey {
				allowed = true
				name = a.Name
			}
		}
	}
	if !allowed || s.files == nil {
		return InquiryResult{}, apierr.NotFound("INQUIRY_FILE", "附件不存在")
	}
	if in.Action == "download" {
		return InquiryResult{URL: "/api/inquiry-files?" + url.Values{"id": {in.ID}, "view": {in.View}, "key": {in.FileKey}}.Encode()}, nil
	}
	reader, ok := s.files.(interface {
		Read(context.Context, string) ([]byte, error)
	})
	if !ok {
		return InquiryResult{}, apierr.Conflict("INQUIRY_FILES", "附件服务未连接")
	}
	data, e := reader.Read(ctx, in.FileKey)
	return InquiryResult{FileData: data, FileName: name}, e
}
func validateInquiryAttachments(ctx context.Context, tx pgx.Tx, tenant, id int64, kind string, attachments []InquiryAttachment) error {
	for _, a := range attachments {
		var allowed bool
		e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM inquiry_attachments WHERE tenant_id=$1 AND case_id=$2 AND kind=$3 AND object_key=$4 AND name=$5 AND ($3='SALES' OR revision=(SELECT inquiry_revision FROM sourcing_cases WHERE tenant_id=$1 AND id=$2))) OR ($3='SALES' AND EXISTS(SELECT 1 FROM sourcing_cases WHERE tenant_id=$1 AND id=$2 AND source_file_key=$4 AND source_file_name=$5))`, tenant, id, kind, a.Key, a.Name).Scan(&allowed)
		if e != nil {
			return e
		}
		if !allowed {
			return apierr.Permission("INQUIRY_FILE", "附件不属于本询盘")
		}
	}
	return nil
}
