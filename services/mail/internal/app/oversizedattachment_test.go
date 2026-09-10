package app

import (
	"bytes"
	"encoding/base64"
	"testing"
)

// 超过上限的入站附件，不留半截。
//
// 原来这里是 io.LimitReader 读满就停——**而它不报错**。一个 30 MB 的附件
// 被截成正好 25 MB 存了下来，入库时还把 25 MB 记成它的真实大小。界面上那
// 一行看起来完全正常：名字对、大小是个合理的数字、能点下载。下下来的文件
// 打不开，而且没有任何地方说过这件事——用户唯一能察觉的时刻，是双击之后
// 发现文件损坏，那时他还分不清是我们截的还是对方发坏的。
//
// 注释当时写的是「超过这个大小就拒绝」，代码做的是截断。不一致的那一半
// 正好是会骗人的那一半。
func TestAnOversizedAttachmentIsNotStoredAsATruncatedFile(t *testing.T) {
	const payload = 30 << 20 // 解码后 30 MB，超过 25 MB 的上限
	raw := mailWithAttachment(bytes.Repeat([]byte("A"), payload))

	parsed, err := ParseMail(raw)
	if err != nil {
		t.Fatalf("整封信应该照常解析出来，只是那个附件不留内容：%v", err)
	}
	if len(parsed.Attachments) != 1 {
		t.Fatalf("附件那一行应该留着，拿到 %d 行", len(parsed.Attachments))
	}
	a := parsed.Attachments[0]

	if len(a.Data) != 0 {
		t.Errorf("留了 %d 字节的残缺内容——正是这个 bug 本身", len(a.Data))
	}
	if !a.Oversized {
		t.Error("没有标成超限，下游就不知道该说实话")
	}
	if a.TrueSize != payload {
		t.Errorf("真实大小记成了 %d，应该是 %d——记成截断后的长度，"+
			"界面上就连「它其实多大」都查不到", a.TrueSize, payload)
	}
	// 名字和类型要留着：那一行还得能告诉人「有过这么个文件」。
	if a.FileName != "big.bin" {
		t.Errorf("文件名丢了：%q", a.FileName)
	}
}

// 正常大小的附件一切照旧——这条是防止上面那个改动误伤多数情况。
func TestANormalAttachmentIsStillKeptWhole(t *testing.T) {
	body := bytes.Repeat([]byte("B"), 1024)
	parsed, err := ParseMail(mailWithAttachment(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Attachments) != 1 {
		t.Fatalf("拿到 %d 个附件", len(parsed.Attachments))
	}
	a := parsed.Attachments[0]
	if a.Oversized {
		t.Error("1 KB 的附件被当成超限了")
	}
	if !bytes.Equal(a.Data, body) {
		t.Errorf("内容变了：%d 字节，应该是 %d", len(a.Data), len(body))
	}
	if a.TrueSize != int64(len(body)) {
		t.Errorf("大小记成了 %d，应该是 %d", a.TrueSize, len(body))
	}
}

// 超限的附件后面还有一个正常附件时，跳过前者不能把后者吃掉。
//
// 单独钉一条是因为跳过的做法是「把这个 part 剩下的读完丢掉」，而 part 的
// 边界由 MIME 分隔符定——理论上读到边界就停，但这属于「理论上对、要看着
// 它对」的地方：读过头一个字节，后面那个附件就没了。
func TestSkippingAnOversizedAttachmentDoesNotSwallowTheNextOne(t *testing.T) {
	var raw bytes.Buffer
	raw.WriteString("From: c@x.com\r\nTo: me@263.net\r\nSubject: two\r\n")
	raw.WriteString("Message-ID: <two@mid>\r\nMIME-Version: 1.0\r\n")
	raw.WriteString("Content-Type: multipart/mixed; boundary=BOUND\r\n\r\n")
	raw.WriteString("--BOUND\r\nContent-Type: text/plain\r\n\r\n正文\r\n")
	raw.WriteString(attachmentPart("big.bin", bytes.Repeat([]byte("A"), 30<<20)))
	raw.WriteString(attachmentPart("small.bin", []byte("still here")))
	raw.WriteString("--BOUND--\r\n")

	parsed, err := ParseMail(raw.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Attachments) != 2 {
		t.Fatalf("跳过大的之后小的没了：只剩 %d 个附件", len(parsed.Attachments))
	}
	small := parsed.Attachments[1]
	if small.FileName != "small.bin" || string(small.Data) != "still here" {
		t.Errorf("后面那个附件被读坏了：%q / %q", small.FileName, small.Data)
	}
}

func attachmentPart(name string, body []byte) string {
	return "--BOUND\r\nContent-Type: application/octet-stream; name=\"" + name + "\"\r\n" +
		"Content-Disposition: attachment; filename=\"" + name + "\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		base64.StdEncoding.EncodeToString(body) + "\r\n"
}

func mailWithAttachment(body []byte) []byte {
	var raw bytes.Buffer
	raw.WriteString("From: c@x.com\r\nTo: me@263.net\r\nSubject: big\r\n")
	raw.WriteString("Message-ID: <big@mid>\r\nMIME-Version: 1.0\r\n")
	raw.WriteString("Content-Type: multipart/mixed; boundary=BOUND\r\n\r\n")
	raw.WriteString("--BOUND\r\nContent-Type: text/plain\r\n\r\n正文\r\n")
	raw.WriteString(attachmentPart("big.bin", body))
	raw.WriteString("--BOUND--\r\n")
	return raw.Bytes()
}
