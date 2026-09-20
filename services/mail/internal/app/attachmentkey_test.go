package app

import (
	"strings"
	"testing"
)

// 同一封信里两个同名附件，必须落在两个不同的位置。
//
// 2026-09-20 查实的事故：公司 4 收到一封转发的船期更新，正文里一张船舶动态
// 截图（149 KB）、签名里一个 logo（4.3 KB），两个部件都叫 image.png。存储
// 位置当时只用「信编号 + 文件名」算，两者撞在一起，后写的 logo 把截图盖掉，
// 而截图那一行还指着那个位置——于是正文里 600 像素宽的位置显示的是被拉糊的
// logo，日志里一个字都没有。全库 461 组、1,015 个文件。

func TestTwoAttachmentsWithTheSameNameGetDifferentKeys(t *testing.T) {
	// 就是那封信：email_inbound 42349，公司 4，信箱 51。
	first := inboundAttachmentKey(4, 51, 42349, 0, "image.png")
	second := inboundAttachmentKey(4, 51, 42349, 1, "image.png")
	if first == second {
		t.Fatalf("同名附件算出了同一个位置，后一个会把前一个覆盖掉：%s", first)
	}
	if first != "mail/inbound/4/51/att/42349-1-image.png" {
		t.Fatalf("第一个附件的位置变了：%s", first)
	}
	if second != "mail/inbound/4/51/att/42349-2-image.png" {
		t.Fatalf("第二个附件的位置变了：%s", second)
	}
}

func TestAttachmentKeyIsStableAcrossReparses(t *testing.T) {
	// 补数据的工具重新解析原件时，要算出和当初该有的完全一样的位置；
	// 不稳定的话（比如拿时间或随机数做后缀）补一次就多一份垃圾。
	a := inboundAttachmentKey(4, 51, 42349, 0, "image.png")
	b := inboundAttachmentKey(4, 51, 42349, 0, "image.png")
	if a != b {
		t.Fatalf("同样的输入算出了两个位置：%s / %s", a, b)
	}
}

func TestAttachmentKeyKeepsMessagesApart(t *testing.T) {
	// 不同的信、不同的信箱、不同的公司，位置都不能撞。
	same := inboundAttachmentKey(4, 51, 42349, 0, "image.png")
	for _, other := range []string{
		inboundAttachmentKey(4, 51, 42350, 0, "image.png"), // 另一封信
		inboundAttachmentKey(4, 43, 42349, 0, "image.png"), // 另一个信箱
		inboundAttachmentKey(1, 51, 42349, 0, "image.png"), // 另一家公司
	} {
		if same == other {
			t.Fatalf("不该相同的两个位置撞了：%s", same)
		}
	}
}

func TestAttachmentKeyStillSanitisesTheFileName(t *testing.T) {
	// 文件名来自发件人，不能让它带着路径跑出这封信自己的目录。
	const prefix = "mail/inbound/4/51/att/42349-1-"
	got := inboundAttachmentKey(4, 51, 42349, 0, "../../etc/passwd")
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("附件跑出了它该在的目录：%s", got)
	}
	if strings.ContainsAny(strings.TrimPrefix(got, prefix), "/\\") ||
		strings.Contains(strings.TrimPrefix(got, prefix), "..") {
		t.Fatalf("文件名里的路径没被处理干净：%s", got)
	}
}
