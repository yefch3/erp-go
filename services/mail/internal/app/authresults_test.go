package app

import "testing"

func TestParseAuthResults(t *testing.T) {
	tests := []struct {
		name, header string
		wantSPF      string
		wantDKIM     string
	}{
		{
			name: "真实的 Gmail 头",
			header: "mx.google.com; " +
				"dkim=pass header.i=@roblox.com header.s=s1 header.b=Xy1zAb; " +
				"spf=pass (google.com: domain of noreply@email.roblox.com designates " +
				"209.85.220.41 as permitted sender) smtp.mailfrom=noreply@email.roblox.com; " +
				"dmarc=pass (p=REJECT sp=REJECT dis=NONE) header.from=email.roblox.com",
			wantSPF:  "email.roblox.com",
			wantDKIM: "roblox.com",
		},
		{
			// 这一条是 valueAfter 要跳过括号的原因：注释里就带着一个
			// "domain of ..." ，早期版本从注释里抠出了错的域名。
			name: "SPF 注释里含有会误导的文本",
			header: "mx.example.com; spf=pass (example.com: domain of " +
				"attacker@evil.test designates 1.2.3.4 as permitted sender) " +
				"smtp.mailfrom=real@supplier.cn",
			wantSPF: "supplier.cn",
		},
		{
			name:     "header.d 优先于 header.i",
			header:   "mx; dkim=pass header.d=supplier.cn header.i=@mail.supplier.cn",
			wantDKIM: "supplier.cn",
		},
		{
			// 没通过就不是身份。宁可什么都不说，也不能把一个签名没证明的
			// 域名摆出来当依据。
			name:     "验证失败时不报告任何域名",
			header:   "mx; dkim=fail header.i=@evil.test; spf=softfail smtp.mailfrom=a@evil.test",
			wantSPF:  "",
			wantDKIM: "",
		},
		{
			name:     "neutral 与 none 同样不算",
			header:   "mx; spf=neutral smtp.mailfrom=a@b.test; dkim=none",
			wantSPF:  "",
			wantDKIM: "",
		},
		{
			name:     "空头",
			header:   "",
			wantSPF:  "",
			wantDKIM: "",
		},
		{
			name:     "大小写与多余空白",
			header:   "mx;   DKIM=PASS   header.d=Supplier.CN  ;  SPF=Pass smtp.mailfrom=X@Supplier.CN.",
			wantSPF:  "supplier.cn",
			wantDKIM: "supplier.cn",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spf, dkim := parseAuthResults(tc.header)
			if spf != tc.wantSPF {
				t.Errorf("spf = %q，期望 %q", spf, tc.wantSPF)
			}
			if dkim != tc.wantDKIM {
				t.Errorf("dkim = %q，期望 %q", dkim, tc.wantDKIM)
			}
		})
	}
}

func TestDomainOf(t *testing.T) {
	for in, want := range map[string]string{
		"@roblox.com":        "roblox.com",
		"noreply@email.test": "email.test",
		"supplier.cn":        "supplier.cn",
		"\"quoted@a.test\"":  "a.test",
		"trailing.dot.test.": "trailing.dot.test",
		"UPPER@CASE.TEST":    "case.test",
		"a@b@weird.test":     "weird.test",
	} {
		if got := domainOf(in); got != want {
			t.Errorf("domainOf(%q) = %q，期望 %q", in, got, want)
		}
	}
}
