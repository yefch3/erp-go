package app

import "testing"

// 草稿附件没有数据库行可以挂"这是谁的"，全部的信任都落在 key 的形状上。
// 这条界线和发送时 PendingAttachment.allowedFor 认的是同一条——它松一分，
// 别人就能拿一个猜来的 key 看到不属于自己公司的文件。
func TestOnlyThisTenantsUploadKeysMayBePreviewed(t *testing.T) {
	const tenant = 4
	for _, c := range []struct {
		name string
		key  string
		want bool
	}{
		{"本公司刚传上去的", "mail-attachments/4/9f2c1ab3d4e5f6a7b8c90d1e2f3a4b5c.pdf", true},
		{"本公司、没有扩展名", "mail-attachments/4/9f2c1ab3d4e5f6a7b8c90d1e2f3a4b5c", true},

		{"别家公司的", "mail-attachments/1/9f2c1ab3d4e5f6a7b8c90d1e2f3a4b5c.pdf", false},
		{"公司号是前缀的另一家（41 不是 4）", "mail-attachments/41/abc.pdf", false},
		{"收到的邮件附件——那条路按信箱排，能指名就能指名同事的箱",
			"mail/inbound/4/51/att/42349-1-image.png", false},
		{"别的用途的对象", "mail-images/4/abc.png", false},
		{"空的", "", false},
		{"想用 .. 跳出自己的前缀", "mail-attachments/4/../1/abc.pdf", false},
		// 编码过的 .. 照样含有字面的 ".."，一并挡掉——这里宁可严一格：
		// 正经的 key 是 32 位随机十六进制加扩展名，不会长这样。
		{"编码过的往上跳", "mail-attachments/4/..%2F..%2Fetc", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := draftAttachmentKeyAllowed(c.key, tenant); got != c.want {
				t.Fatalf("draftAttachmentKeyAllowed(%q) = %v，想要 %v", c.key, got, c.want)
			}
		})
	}
}

// 发送那条路和预览这条路必须认同一条界线：一边放行另一边不放，就意味着
// 「看得到但发不出去」或者更糟的反过来。
func TestPreviewAndSendAgreeOnWhichKeysAreOurs(t *testing.T) {
	const tenant = 4
	for _, key := range []string{
		"mail-attachments/4/abc.pdf",
		"mail-attachments/1/abc.pdf",
		"mail/inbound/4/51/att/1-1-x.png",
		"",
	} {
		preview := draftAttachmentKeyAllowed(key, tenant)
		send := PendingAttachment{FileKey: key}.allowedFor(tenant)
		if preview != send {
			t.Fatalf("key %q：预览放行=%v，发送放行=%v，两条界线对不上", key, preview, send)
		}
	}
}

// 浏览器对 PDF 报 application/octet-stream 是常见的，那样一份 PDF 会白白
// 失去预览。类型说不清时按扩展名再猜一次。
func TestContentTypeFallsBackToTheFileName(t *testing.T) {
	for _, c := range []struct {
		name, stored, file, want string
	}{
		{"存储里的类型就对", "application/pdf", "a.pdf", "application/pdf"},
		// 带参数的会被 previewable 归一成裸类型，正是签内联地址要用的那个。
		{"存储里含参数", "application/pdf; charset=binary", "a.pdf", "application/pdf"},
		{"存储里说不清，靠扩展名认出来", "application/octet-stream", "报价单.pdf", "application/pdf"},
		{"图片同理", "application/octet-stream", "logo.png", "image/png"},
		{"认不出来就原样", "application/octet-stream", "合同.docx",
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"没有扩展名", "application/octet-stream", "README", "application/octet-stream"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := draftContentType(c.stored, c.file); got != c.want {
				t.Fatalf("draftContentType(%q, %q) = %q，想要 %q", c.stored, c.file, got, c.want)
			}
		})
	}
}

// 认出来之后，能不能预览还是 previewable 说了算——Word 在这条路上只能下载。
func TestWhatActuallyGetsAPreviewOnThisPath(t *testing.T) {
	for _, c := range []struct {
		file string
		want bool
	}{
		{"报价单.pdf", true},
		{"logo.png", true},
		{"照片.jpeg", true},
		{"合同.docx", false}, // 在线 Office 那条路草稿走不了，只能下载
		{"报表.xlsx", false}, // 同上；浏览器自己画表格那一档不靠这个字段
		{"存档.zip", false},
	} {
		t.Run(c.file, func(t *testing.T) {
			got := previewable(draftContentType("application/octet-stream", c.file)) != ""
			if got != c.want {
				t.Fatalf("%s 能直接预览=%v，想要 %v", c.file, got, c.want)
			}
		})
	}
}
