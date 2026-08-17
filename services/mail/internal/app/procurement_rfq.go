package app

import (
	"bytes"
	"context"
	"strings"

	"github.com/sgao19/erp-go/pkg/apierr"
)

type ProcurementOrderAttachment struct {
	FileName, ContentType string
	Data                  []byte
}

// SendProcurementRFQ queues one transactional message as the employee whose
// mailbox is designated as the procurement shared identity by the gateway.
func (s *Service) SendProcurementRFQ(ctx context.Context, tenantID, senderEmployeeID int64, recipientName, recipientEmail, subject, body, fileName string, data []byte) (CampaignResult, error) {
	if senderEmployeeID <= 0 {
		return CampaignResult{}, apierr.Invalid("NT_PROCUREMENT_SENDER_NOT_CONFIGURED", "尚未配置采购公共发件邮箱")
	}
	if strings.TrimSpace(recipientEmail) == "" {
		return CampaignResult{}, apierr.Invalid("NT_EMAIL_REQUIRED", "请填写供应商邮箱")
	}
	if len(data) == 0 || len(data) > MaxAttachmentBytes || !strings.HasSuffix(strings.ToLower(fileName), ".xlsx") {
		return CampaignResult{}, apierr.Invalid("NT_RFQ_ATTACHMENT_INVALID", "工厂询价附件无效")
	}
	if s.files == nil {
		return CampaignResult{}, apierr.Invalid("NT_STORAGE_UNAVAILABLE", "文件存储未配置")
	}
	key, err := objectKey("mail-attachments", tenantID, fileName)
	if err != nil {
		return CampaignResult{}, err
	}
	if err := s.files.Put(ctx, key, bytes.NewReader(data), int64(len(data)), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"); err != nil {
		return CampaignResult{}, err
	}
	result, err := s.CreateCampaign(ctx, tenantID, CampaignInput{
		Subject: strings.TrimSpace(subject), Body: strings.TrimSpace(body), Format: "TEXT", Kind: "TRANSACTIONAL",
		Recipients:  []Recipient{{Name: strings.TrimSpace(recipientName), Email: strings.TrimSpace(recipientEmail)}},
		Attachments: []PendingAttachment{{FileName: fileName, FileKey: key}},
	}, Operator{ID: senderEmployeeID})
	if err != nil {
		_ = s.files.Remove(ctx, key)
		return CampaignResult{}, err
	}
	return result, nil
}

// SendProcurementOrder queues the approved supplier-facing order documents.
// The caller supplies both the versioned workbook and its print-ready PDF;
// bytes are validated before they enter object storage.
func (s *Service) SendProcurementOrder(ctx context.Context, tenantID, senderEmployeeID int64, recipientName, recipientEmail, subject, body string, files []ProcurementOrderAttachment) (CampaignResult, error) {
	if senderEmployeeID <= 0 {
		return CampaignResult{}, apierr.Invalid("NT_PROCUREMENT_SENDER_NOT_CONFIGURED", "尚未配置采购发件邮箱")
	}
	if strings.TrimSpace(recipientEmail) == "" {
		return CampaignResult{}, apierr.Invalid("NT_EMAIL_REQUIRED", "请填写供应商邮箱")
	}
	if len(files) == 0 || len(files) > 2 {
		return CampaignResult{}, apierr.Invalid("NT_PO_ATTACHMENTS_INVALID", "采购单附件无效")
	}
	if s.files == nil {
		return CampaignResult{}, apierr.Invalid("NT_STORAGE_UNAVAILABLE", "文件存储未配置")
	}
	pending := make([]PendingAttachment, 0, len(files))
	keys := make([]string, 0, len(files))
	cleanup := func() {
		for _, key := range keys {
			_ = s.files.Remove(ctx, key)
		}
	}
	for _, file := range files {
		name := strings.TrimSpace(file.FileName)
		lower := strings.ToLower(name)
		valid := len(file.Data) > 0 && len(file.Data) <= MaxAttachmentBytes &&
			(strings.HasSuffix(lower, ".xlsx") || (strings.HasSuffix(lower, ".pdf") && bytes.HasPrefix(file.Data, []byte("%PDF-"))))
		if !valid {
			cleanup()
			return CampaignResult{}, apierr.Invalid("NT_PO_ATTACHMENT_INVALID", "采购单附件无效")
		}
		contentType := file.ContentType
		if contentType == "" {
			contentType = "application/pdf"
			if strings.HasSuffix(lower, ".xlsx") {
				contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
			}
		}
		key, err := objectKey("mail-attachments", tenantID, name)
		if err != nil {
			cleanup()
			return CampaignResult{}, err
		}
		if err := s.files.Put(ctx, key, bytes.NewReader(file.Data), int64(len(file.Data)), contentType); err != nil {
			cleanup()
			return CampaignResult{}, err
		}
		keys = append(keys, key)
		pending = append(pending, PendingAttachment{FileName: name, FileKey: key})
	}
	result, err := s.CreateCampaign(ctx, tenantID, CampaignInput{
		Subject: strings.TrimSpace(subject), Body: strings.TrimSpace(body), Format: "TEXT", Kind: "TRANSACTIONAL",
		Recipients: []Recipient{{Name: strings.TrimSpace(recipientName), Email: strings.TrimSpace(recipientEmail)}}, Attachments: pending,
	}, Operator{ID: senderEmployeeID})
	if err != nil {
		cleanup()
		return CampaignResult{}, err
	}
	return result, nil
}
