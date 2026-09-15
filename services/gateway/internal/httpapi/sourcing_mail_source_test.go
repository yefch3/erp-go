package httpapi

import (
	"testing"

	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

func TestApplyMailSourceKeepsMailLinkWithoutCopyingAttachment(t *testing.T) {
	req := &prv1.CreateCaseRequest{
		SourceMailId:       9597,
		SourceAttachmentId: 21246,
		SourceFileName:     "customer-inquiry.zip",
		SourceContentType:  "application/zip",
		SourceFileData:     []byte("attachment bytes"),
	}

	applyMailSource(req, &mailv1.InboundMail{FromName: "客户联系人"})

	if req.GetSourceMailId() != 9597 {
		t.Fatalf("来源邮件应保留，实际 %d", req.GetSourceMailId())
	}
	if req.GetContactName() != "客户联系人" {
		t.Fatalf("联系人应来自邮件发件人，实际 %q", req.GetContactName())
	}
	if req.GetSourceAttachmentId() != 0 || req.GetSourceFileName() != "" ||
		req.GetSourceContentType() != "" || len(req.GetSourceFileData()) != 0 {
		t.Fatal("邮件转询盘不应把附件编号或文件内容传入采购")
	}
}
