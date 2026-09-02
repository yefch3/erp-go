package app

import "testing"

// 列表上那枚回形针：签名 logo 不算，真附件算，没被正文指着的部件也算。
func TestListedAttachmentsIgnoreWhatTheBodyAlreadyShows(t *testing.T) {
	logo := ParsedAttachment{FileName: "logo.png", ContentType: "image/png", ContentID: "logo@erp"}
	pdf := ParsedAttachment{FileName: "contract.pdf", ContentType: "application/pdf"}
	body := `<p>Best regards,</p><img src="cid:logo@erp">`

	cases := []struct {
		name string
		mail ParsedMail
		want bool
	}{
		{"只有一个被正文指着的 logo：不亮", ParsedMail{BodyHTML: body, Attachments: []ParsedAttachment{logo}}, false},
		{"logo 加一份合同：亮", ParsedMail{BodyHTML: body, Attachments: []ParsedAttachment{logo, pdf}}, true},
		{"有 Content-ID 但正文没指着它：亮——客户端给真附件标 inline 的事天天发生",
			ParsedMail{BodyHTML: "<p>hi</p>", Attachments: []ParsedAttachment{logo}}, true},
		{"没有 Content-ID 的图片：亮", ParsedMail{BodyHTML: body,
			Attachments: []ParsedAttachment{{FileName: "photo.jpg", ContentType: "image/jpeg"}}}, true},
		{"一个部件都没有：不亮", ParsedMail{BodyHTML: body}, false},
		{"正文是纯文本、部件带 Content-ID：亮（纯文本指不着任何东西）",
			ParsedMail{BodyText: "hi", Attachments: []ParsedAttachment{logo}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasListedAttachments(c.mail); got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}
