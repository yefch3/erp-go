package app

import (
	"strings"
	"testing"
)

// 自家的追踪像素必须在送到浏览器之前拆掉。
//
// 不拆的后果不是「多一次请求」，而是这个功能整个不可信：本公司的人打开自己
// 发出的那封信，或者打开一封把原文引回来的回复，浏览器就会去拉那张 1×1 的图，
// 于是系统把客户记成「已读」——而客户可能根本没见过这封信。
func TestOwnPixelIsRemovedButOtherPicturesAreNot(t *testing.T) {
	const self = "mercova.example.com"

	tests := []struct {
		name    string
		html    string
		removed bool
	}{
		{
			name:    "自家像素",
			html:    `<p>hi</p><img src="https://mercova.example.com/api/public/mail-open/abc-123" width="1" height="1">`,
			removed: true,
		},
		{
			name:    "自家像素，http",
			html:    `<img src="http://mercova.example.com/api/public/mail-open/abc-123">`,
			removed: true,
		},
		{
			name:    "自家像素，主机名大小写不同",
			html:    `<img src="https://MERCOVA.EXAMPLE.COM/api/public/mail-open/abc">`,
			removed: true,
		},
		{
			// 同一台主机上的真图片必须留着：附件预览、签名档图标都从这里来。
			name:    "自家主机上的真图片",
			html:    `<img src="https://mercova.example.com/api/files/logo.png">`,
			removed: false,
		},
		{
			// 别人的像素是别人的事，交给图片缓存处理，不归这里管。
			name:    "别人家的追踪像素",
			html:    `<img src="https://tracker.other.test/api/public/mail-open/xyz">`,
			removed: false,
		},
		{
			name:    "内嵌图片",
			html:    `<img src="cid:logo@corp">`,
			removed: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stripOwnPixel(tc.html, self)
			gone := !strings.Contains(got, "mail-open") || strings.Contains(got, blankPixel)
			hasBlank := strings.Contains(got, blankPixel)
			if tc.removed {
				if !hasBlank {
					t.Errorf("像素没有被替换掉：%s", got)
				}
			} else {
				if hasBlank {
					t.Errorf("不该动的图片被替换了：%s", got)
				}
				if got != tc.html {
					t.Errorf("正文被改动了：\n原: %s\n后: %s", tc.html, got)
				}
			}
			_ = gone
		})
	}
}

// 没配公网地址时，外面就不存在我们的像素，正文一个字节都不该被动。
func TestStripOwnPixelDoesNothingWithoutAPublicHost(t *testing.T) {
	html := `<img src="https://anywhere.test/api/public/mail-open/abc">`
	if got := stripOwnPixel(html, ""); got != html {
		t.Errorf("没有 selfHost 时正文被改了：%s", got)
	}
}

// 注入和拆除必须认同一条路径。分成两处字符串，改了一处而没改另一处，读信时
// 就会重新开始误报自己的已读——而且是静默的。
func TestInjectAndStripAgreeOnTheRoute(t *testing.T) {
	const base = "https://mercova.example.com"
	html := InjectOpenPixel(`<p>正文</p>`, base, "key-abc")
	if !strings.Contains(html, openPixelPath) {
		t.Fatalf("注入的像素不在约定的路径上：%s", html)
	}
	if out := stripOwnPixel(html, "mercova.example.com"); strings.Contains(out, "/api/public/mail-open/") {
		t.Errorf("刚注入的像素没有被拆掉：%s", out)
	}
}
