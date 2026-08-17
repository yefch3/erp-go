package httpapi

import (
	"strings"
	"testing"
)

// Google's rule, and the whole reason this check exists: https on a real
// domain, or localhost. Anything else it refuses with a page that explains
// nothing — so we refuse first, with words that name the alternative.
func TestGoogleWillRefuseKnowsWhichRedirectsGoogleTakes(t *testing.T) {
	cases := []struct {
		name     string
		redirect string
		refuse   bool
		mentions string
	}{
		{"本机开发", "http://localhost:8080/api/oauth/google/callback", false, ""},
		{"本机回环", "http://127.0.0.1:8080/api/oauth/google/callback", false, ""},
		{"生产域名", "https://erp.example.com/api/oauth/google/callback", false, ""},
		// The IP-only phase between moving to the cloud and buying a domain.
		{"裸 IP + http", "http://100.22.230.55/api/oauth/google/callback", true, "IP"},
		// Even with TLS, Google wants a name.
		{"裸 IP + https", "https://100.22.230.55/api/oauth/google/callback", true, "域名"},
		// A domain without TLS is refused for the scheme, not the host.
		{"域名但 http", "http://erp.example.com/api/oauth/google/callback", true, "https"},
		{"空值", "", true, "无效"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			why := googleWillRefuse(c.redirect)
			if c.refuse && why == "" {
				t.Fatalf("%s 应当被我们提前拦下，但放行了——用户会撞上 Google 的天书页面", c.redirect)
			}
			if !c.refuse && why != "" {
				t.Fatalf("%s 是 Google 接受的地址，却被拦了：%s", c.redirect, why)
			}
			if c.refuse && !strings.Contains(why, c.mentions) {
				t.Errorf("拒绝理由没说清关键点 %q：%s", c.mentions, why)
			}
			// Every refusal must name the way forward, not just the problem.
			if c.refuse && c.mentions != "无效" && !strings.Contains(why, "授权码") {
				t.Errorf("拒绝理由没给出替代路径（密码/授权码登录）：%s", why)
			}
		})
	}
}
