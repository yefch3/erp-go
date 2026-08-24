package pdffont

import (
	"strings"
	"testing"

	"codeberg.org/go-pdf/fpdf"
)

// 粗体和常规必须都注册上。
//
// 这是一个变体字体，两种字重出自同一份字节；只注册常规那一份，代码照样编译、
// PDF 照样生成、中文照样显示，只是**所有粗体都悄悄没了**——标题和表头会和正文
// 一样粗。这种东西不会在构建里响，只会在客户收到单据之后才被看见。
func TestRegisterGivesBothWeights(t *testing.T) {
	for _, style := range []string{"", "B"} {
		pdf := fpdf.New("P", "mm", "A4", "")
		Register(pdf)
		pdf.AddPage()
		pdf.SetFont(Name, style, 12)
		if err := pdf.Error(); err != nil {
			t.Fatalf("字重 %q 没注册上：%v", style, err)
		}
		pdf.Cell(40, 10, "报价单 Quotation")
		if err := pdf.Error(); err != nil {
			t.Fatalf("字重 %q 写不出中文：%v", style, err)
		}
	}
}

// 只注册常规时，取粗体必须是错——否则上面那条测试什么也没在证明。
func TestMissingWeightIsAnError(t *testing.T) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddUTF8FontFromBytes(Name, "", data) // 故意只注册一半
	pdf.AddPage()
	pdf.SetFont(Name, "B", 12)
	if pdf.Error() == nil {
		t.Fatal("少注册一个字重竟然不报错，那 TestRegisterGivesBothWeights 就是空的")
	}
}

// 字体是嵌进程序里的，不是运行时去磁盘找的。少了这一条，容器里缺个文件就是
// 周五晚上的线上故障；嵌进来则要么编译不过，要么一定在。
func TestFontIsEmbeddedAndLooksLikeATrueTypeFile(t *testing.T) {
	if len(data) < 1<<20 {
		t.Fatalf("字体只有 %d 字节，中日韩字体不可能这么小——多半是嵌错了文件", len(data))
	}
	// TrueType 的魔数：0x00010000，或者 OpenType 的 "OTTO"、变体字体的 "true"。
	head := string(data[:4])
	if data[0] != 0x00 && head != "OTTO" && head != "true" && head != "ttcf" {
		t.Fatalf("开头不像字体文件：% x", data[:4])
	}
	if strings.TrimSpace(Name) == "" {
		t.Fatal("字体名是空的，SetFont 会找不到它")
	}
}
