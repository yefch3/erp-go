package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"
)

// 够让 http.DetectContentType 认成 PNG 的最短字节：它只看开头那八个字节的签名。
var tinyPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR-enough-to-sniff")

func dataURL(mime string, b []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// Foxmail 把截图粘进正文，就是这个样子。
func TestAPictureCarriedInTheBodyIsDecoded(t *testing.T) {
	got := dataImagesIn(`<p>见截图</p><img alt="" src="` + dataURL("image/png", tinyPNG) + `">`)
	if len(got) != 1 {
		t.Fatalf("found %d pictures, want 1", len(got))
	}
	if got[0].contentType != "image/png" || !bytes.Equal(got[0].data, tinyPNG) {
		t.Errorf("decoded %q as %s", got[0].data, got[0].contentType)
	}
	if !strings.HasPrefix(got[0].key, dataImageKeyPrefix) {
		t.Errorf("key %q is not a data key", got[0].key)
	}
}

// 类型看字节，不看它自称什么：存下来的东西会带着这里记的类型交给浏览器。
func TestOnlyRealPicturesAreTakenOutOfTheBody(t *testing.T) {
	script := []byte(`<html><script>alert(1)</script></html>`)
	for name, src := range map[string]string{
		"html calling itself png": dataURL("image/png", script),
		"html":                    dataURL("text/html", script),
		"svg":                     dataURL("image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)),
		"percent-encoded":         "data:image/png,%89PNG%0D%0A%1A%0A",
		"not base64":              "data:image/png;base64,@@@@",
		"empty":                   "data:image/png;base64,",
	} {
		if got := dataImagesIn(`<img src="` + src + `">`); len(got) != 0 {
			t.Errorf("%s was taken as %s", name, got[0].contentType)
		}
	}
}

func TestAnOversizedPictureInTheBodyIsLeftAlone(t *testing.T) {
	big := append(append([]byte{}, tinyPNG...), make([]byte, cacheMaxImageBytes)...)
	if got := dataImagesIn(`<img src="` + dataURL("image/png", big) + `">`); len(got) != 0 {
		t.Fatalf("a %d-byte picture was taken", len(got[0].data))
	}
}

// 会话里一轮轮引用的同一张图，折行折得不一样也还是同一张，只存一次。
func TestTheSamePictureQuotedThroughAThreadIsTakenOnce(t *testing.T) {
	u := dataURL("image/png", tinyPNG)
	wrapped := u[:30] + "\r\n" + u[30:]
	body := `<img src="` + u + `"><blockquote><img src="` + wrapped + `"><img src='` + u + `'></blockquote>`
	if got := dataImagesIn(body); len(got) != 1 {
		t.Fatalf("found %d, want 1", len(got))
	}
}

// 打开信时换成存好的那一份，而且要在净化之前换：净化器不放行 data:，
// 过一遍就只剩一个没有 src 的 <img>，之后再换已经无从换起。
func TestAPictureCarriedInTheBodyIsShownFromStorage(t *testing.T) {
	u := dataURL("image/png", tinyPNG)
	body := `<p>见截图</p><img src="` + u[:40] + "\n" + u[40:] + `">`
	stored := "https://files.example/mail/inbound-img/1/2/ab.png"
	cached := imageSwap{dataImageKey(u): stored}

	got := SanitizeForReading((&Service{}).localiseImages(context.Background(), body, withInlinePictures(nil, cached)))
	if !strings.Contains(got, `src="`+stored+`"`) {
		t.Errorf("not shown from storage: %s", got)
	}
	if strings.Contains(got, "base64") {
		t.Errorf("the picture itself is still in the body: %.200s", got)
	}
}

// 净化之前那一轮只加自带的图。外链图片的缓存是按净化之后的样子记的，
// 混进来会在错的那一边换。传进来的 map 不能被改：会话视图里是按信共用的。
func TestOnlyInlinePicturesJoinTheSwapBeforeSanitising(t *testing.T) {
	embedded := imageSwap{"cid:logo@x": "https://files/logo.png"}
	inline := dataImageKeyPrefix + "abc"
	cached := imageSwap{"https://sender/a%20b.png": "https://files/remote.png", inline: "https://files/inline.png"}

	got := withInlinePictures(embedded, cached)
	if got["cid:logo@x"] == "" || got[inline] == "" {
		t.Errorf("lost an entry: %v", got)
	}
	if _, ok := got["https://sender/a%20b.png"]; ok {
		t.Errorf("a remote picture joined the pre-sanitiser swap: %v", got)
	}
	if len(embedded) != 1 {
		t.Errorf("the embedded swap was modified: %v", embedded)
	}
	if same := withInlinePictures(embedded, imageSwap{"https://x/y.png": "z"}); len(same) != 1 {
		t.Errorf("nothing inline, yet the swap changed: %v", same)
	}
}

// 改回发件人原地址时，这条地址是在净化之后原样写回正文的，所以自己把关。
func TestSenderAddressOnlyTakesPlainWebAddresses(t *testing.T) {
	for in, want := range map[string]string{
		"https://buyer.example/logo.png":         "https://buyer.example/logo.png",
		"http://buyer.example/a.png?x=1&amp;y=2": "http://buyer.example/a.png?x=1&amp;y=2",
		dataImagePrefixForTest:                   "",
		"javascript:alert(1)":                    "",
		`https://x.example/a.png" onerror="x()`:  "",
		"https://x.example/a b.png":              "",
		"https://x.example/<b>.png":              "",
		"":                                       "",
	} {
		if got := senderAddress(in); got != want {
			t.Errorf("senderAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

var dataImagePrefixForTest = dataImageKeyPrefix + "abc"
