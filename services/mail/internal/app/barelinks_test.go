package app

import (
	"strings"
	"testing"
)

// 用户报的那一封：HTML 邮件，正文里的 Zoom 地址是光秃秃的文字。
//
// 生产上核对过：is_html = 真，整封信 <a> 标签数为 0，地址在正文里但不是锚点。
// 发信方就是这么写的，很多事务性邮件都这样。
func TestABareURLInsideHTMLBecomesALink(t *testing.T) {
	out := LinkifyBareURLs(`<p>Zoom: https://amplitude.zoom.us/j/93698437696</p>`)
	if !strings.Contains(out, `href="https://amplitude.zoom.us/j/93698437696"`) {
		t.Fatalf("裸写的网址没补成链接：\n%s", out)
	}
	if !strings.Contains(out, `target="_blank"`) || !strings.Contains(out, `rel="noopener noreferrer"`) {
		t.Errorf("补出来的链接缺 target/rel：\n%s", out)
	}
}

// 最要紧的一条：已经是链接的不能再包一层。
//
// <a> 套 <a> 浏览器会拆开，结果比不动还糟。
func TestAnExistingLinkIsNeverWrappedAgain(t *testing.T) {
	in := `<p><a href="https://x.com/a">https://x.com/a</a></p>`
	out := LinkifyBareURLs(in)
	if strings.Count(out, "<a ") != 1 {
		t.Fatalf("锚点被套了第二层：\n%s", out)
	}
}

// 正则扫原始 HTML 会把属性里的地址也包起来，包出坏掉的标签。按树走就不会。
func TestAURLInsideAnAttributeIsLeftAlone(t *testing.T) {
	in := `<img src="https://cdn.x.com/a.png" alt="pic">`
	if out := LinkifyBareURLs(in); strings.Contains(out, "<a ") {
		t.Fatalf("属性里的地址被当成正文补了链接：\n%s", out)
	}
}

func TestAURLInsideStyleIsLeftAlone(t *testing.T) {
	in := `<style>body{background:url(https://cdn.x.com/bg.png)}</style><p>hi</p>`
	if out := LinkifyBareURLs(in); strings.Contains(out, "<a ") {
		t.Fatalf("样式表里的地址被补了链接：\n%s", out)
	}
}

func TestTrailingPunctuationStaysOutOfTheLink(t *testing.T) {
	out := LinkifyBareURLs(`<p>详见 https://x.com/a。</p>`)
	if !strings.Contains(out, `href="https://x.com/a"`) {
		t.Errorf("句号被吞进地址了：\n%s", out)
	}
	if !strings.Contains(out, "。") {
		t.Errorf("句号丢了：\n%s", out)
	}
}

func TestBalancedParenthesesBelongToTheURL(t *testing.T) {
	out := LinkifyBareURLs(`<p>https://en.wikipedia.org/wiki/Go_(programming_language)</p>`)
	if !strings.Contains(out, `href="https://en.wikipedia.org/wiki/Go_(programming_language)"`) {
		t.Errorf("配对的括号被剥掉了：\n%s", out)
	}
}

func TestSeveralURLsInOneParagraphEachBecomeALink(t *testing.T) {
	out := LinkifyBareURLs(`<p>A https://a.com 和 B https://b.com 完</p>`)
	if n := strings.Count(out, "<a "); n != 2 {
		t.Fatalf("补出了 %d 个链接，应该是 2：\n%s", n, out)
	}
	if !strings.Contains(out, "完") {
		t.Errorf("末尾的文字丢了：\n%s", out)
	}
}

// 没有裸地址时原样退回，不做「解析再序列化」那一趟——那会把原文规范化
// （补全标签、改引号），对没有变化的正文是无谓的扰动。
func TestBodiesWithNothingToDoComeBackUntouched(t *testing.T) {
	for _, in := range []string{
		`<p>没有网址</p>`,
		`<p><a href="https://x.com">已经是链接</a></p>`,
		`<div class='x'>单引号属性也别动</div>`,
	} {
		if out := LinkifyBareURLs(in); out != in {
			t.Errorf("正文被无谓地改写了：\n原：%s\n后：%s", in, out)
		}
	}
}

// 补链接是锦上添花，不值得为它丢掉一封信的正文。
func TestUnparseableInputComesBackAsItWas(t *testing.T) {
	in := `<p>未闭合 https://x.com/a`
	if out := LinkifyBareURLs(in); !strings.Contains(out, "x.com/a") {
		t.Errorf("正文丢了：\n%s", out)
	}
}
