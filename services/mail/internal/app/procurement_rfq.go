package app

import (
	"bytes"
	"context"
	"strings"

	"github.com/sgao19/erp-go/pkg/apierr"
)

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
