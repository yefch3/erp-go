package app

import (
	"regexp"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// 哪些扩展名交给在线 Office，各是哪种编辑器。
func TestOfficeDocumentTypeByExtension(t *testing.T) {
	cases := map[string]string{
		"报价.docx": "word", "合同.DOC": "word", "说明.rtf": "word", "a.odt": "word", "readme.txt": "word",
		"装箱单.xlsx": "cell", "old.xls": "cell", "list.csv": "cell", "b.ods": "cell",
		"deck.pptx": "slide", "老.ppt": "slide", "c.odp": "slide",
		// 浏览器自己看得更快的，不归这里。
		"scan.pdf": "", "photo.png": "", "archive.zip": "", "noext": "",
		// 名字两边的空白不该影响判断。
		"  报价.xlsx  ": "cell",
	}
	for name, want := range cases {
		if got := officeDocumentType(name); got != want {
			t.Errorf("%q: want %q, got %q", name, want, got)
		}
	}
}

// 缓存键：同一个对象键 + 同一版永远同一个值，形状符合 OnlyOffice 的要求。
func TestOfficeDocKeyIsStableAndWellFormed(t *testing.T) {
	a := officeDocKey("mail/12/att/7/packing.xlsx", 0)
	b := officeDocKey("mail/12/att/7/packing.xlsx", 0)
	if a != b {
		t.Fatalf("同一个对象键要得到同一个缓存键：%q vs %q", a, b)
	}
	if c := officeDocKey("mail/12/att/8/packing.xlsx", 0); c == a {
		t.Fatal("不同的对象键不该撞成同一个缓存键")
	}
	// **换一版必须换键。** 不换的话 Document Server 会把改之前那一份直接
	// 端出来，人看到的是「我明明改了，再打开还是旧的」——它不报错。
	if v1 := officeDocKey("mail/12/att/7/packing.xlsx", 1); v1 == a {
		t.Fatal("版本不同的必须是不同的缓存键")
	}
	if officeDocKey("k", 2) == officeDocKey("k", 3) {
		t.Fatal("相邻两版也不能撞")
	}
	// OnlyOffice：只许 0-9 a-z A-Z -._=，最长 128。
	shape := regexp.MustCompile(`^[0-9A-Za-z._=-]{1,128}$`)
	for _, k := range []string{a, officeDocKey("mail/12/att/7/packing.xlsx", 12)} {
		if !shape.MatchString(k) {
			t.Fatalf("缓存键的形状不合要求：%q", k)
		}
	}
}

// 编辑器是开着还是锁着，只有这两档。
func TestOfficeMode(t *testing.T) {
	if officeMode(true) != "edit" || officeMode(false) != "view" {
		t.Fatal("mode 只有 edit / view")
	}
}

// 这一版算在谁头上：优先回调里报的那个人。
//
// 一份文件可以两个人一起改，而地址里那段 token 记的是**开编辑器的那一个**。
// 两个人改、第一个人先走，全算在他头上就错了。
func TestOfficeEditorPrefersTheUserTheCallbackNames(t *testing.T) {
	if got := officeEditor([]string{"77"}, 12); got != 77 {
		t.Fatalf("回调报了人就用它：%d", got)
	}
	if got := officeEditor([]string{" 88 ", "99"}, 12); got != 88 {
		t.Fatalf("两边的空白不该影响：%d", got)
	}
	// 没报人、报的是空、报的不是数字：退回开编辑器的那个，而不是记成 0。
	for _, users := range [][]string{nil, {}, {""}, {"anonymous"}, {"-3"}, {"0"}} {
		if got := officeEditor(users, 12); got != 12 {
			t.Fatalf("%v 该退回 12，得到 %d", users, got)
		}
	}
}

// 语言码翻译：认得的翻过去，认不得的给英文。
func TestOfficeLang(t *testing.T) {
	cases := map[string]string{"zh": "zh-CN", "ZH": "zh-CN", "zh-CN": "zh-CN", "es": "es", "en": "en", "fr": "en", "": "en"}
	for in, want := range cases {
		if got := officeLang(in); got != want {
			t.Errorf("%q: want %q, got %q", in, want, got)
		}
	}
}

// 三样缺一不可。缺密钥尤其不能算配好了：那等于把一台会出去抓文件的服务器
// 敞开着。缺内网地址也不行——留一条"让它自己去 S3 取"的退路，就等于留着
// 一条签名关着的路，而那条路上的回调验不了身份。
func TestNewOfficeRequiresAllThree(t *testing.T) {
	for _, c := range [][3]string{
		{"", "s", "http://mail:9011"},
		{"/docs", "", "http://mail:9011"},
		{"/docs", "s", ""},
		{"  ", "  ", "  "},
	} {
		if o := NewOffice(c[0], c[1], c[2]); o != nil {
			t.Fatalf("%v 该是 nil，得到 %+v", c, o)
		}
	}
	o := NewOffice("https://erp.example/docs/", "s", "http://mail:9011/")
	if o == nil || o.PublicURL != "https://erp.example/docs" || o.InternalURL != "http://mail:9011" {
		t.Fatalf("末尾的斜杠该去掉：%+v", o)
	}
}

// 地址里那段 token：我们自己签、自己验，而且**不能**用 Document Server
// 那把密钥验得过——两把钥匙混用，就是"拿 A 处的 token 去 B 处用"。
func TestOfficeURLTokenRoundTrip(t *testing.T) {
	o := NewOffice("/docs", "the-docs-secret", "http://mail:9011")
	in := officeURLClaims{Tenant: 7, Inbound: 42, Attachment: 9, Operator: 3}
	tok, err := o.signURLToken(in, officePurposeFetch, officeFetchTokenTTL)
	if err != nil {
		t.Fatal(err)
	}
	got, err := o.parseURLToken(tok, officePurposeFetch)
	if err != nil {
		t.Fatalf("自己签的该自己验得过：%v", err)
	}
	if got.Tenant != 7 || got.Inbound != 42 || got.Attachment != 9 || got.Operator != 3 {
		t.Fatalf("解出来的不是签进去的：%+v", got)
	}
	// **取件的 token 不能拿去回存。** 取件那段连只读预览都会签出来，而且就摆
	// 在浏览器看得见的地方；没有这一句，它就是回存那条路的门票。
	if _, err := o.parseURLToken(tok, officePurposeSave); err == nil {
		t.Fatal("取件的 token 不该能用在回存上")
	}
	save, err := o.signURLToken(in, officePurposeSave, officeSaveTokenTTL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.parseURLToken(save, officePurposeFetch); err == nil {
		t.Fatal("回存的 token 也不该能用在取件上")
	}
	// 换一把密钥的服务验不过。
	other := NewOffice("/docs", "another-secret", "http://mail:9011")
	if _, err := other.parseURLToken(tok, officePurposeFetch); err == nil {
		t.Fatal("别人签的 token 不该验得过")
	}
	// 用 Document Server 那把密钥签的东西，到不了这条路上。
	if _, err := o.docsSigned("Bearer " + tok); err == nil {
		t.Fatal("地址 token 不该能当成 Document Server 的签名")
	}
	// 缺了主体（哪封信、哪个附件）的 token 不算数：验得过签名但指不着东西。
	empty, err := o.signURLToken(officeURLClaims{}, officePurposeFetch, officeFetchTokenTTL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.parseURLToken(empty, officePurposeFetch); err == nil {
		t.Fatal("没有主体的 token 该被拒")
	}
	// 过期的不认。
	old, err := o.signURLToken(in, officePurposeFetch, -time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.parseURLToken(old, officePurposeFetch); err == nil {
		t.Fatal("过期的 token 该被拒")
	}
}

// docsSigned 取的是 Document Server **签过的那一份内容**，不是请求里的明文。
//
// 两种形状都要认：取件请求是 {"payload":{...}}（本地拿真的 9.4.0 验过），
// 回调正文里那个 token 字段是把内容摊在 claims 上。
func TestDocsSignedReturnsWhatWasSigned(t *testing.T) {
	o := NewOffice("/docs", "the-docs-secret", "http://mail:9011")
	sign := func(c jwt.MapClaims) string {
		t.Helper()
		s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte("the-docs-secret"))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	wrapped := sign(jwt.MapClaims{"payload": map[string]any{"url": "http://mail:9011/office/file?t=abc"}})
	got, err := o.docsSigned("Bearer " + wrapped)
	if err != nil {
		t.Fatalf("它自己签的该验得过：%v", err)
	}
	if got["url"] != "http://mail:9011/office/file?t=abc" {
		t.Fatalf("该把 payload 里那层拆出来：%v", got)
	}
	// 头上没有 Bearer 前缀也认：前缀是可配的，值才是关键。
	flat := sign(jwt.MapClaims{"status": float64(2), "url": "http://docs/cache/x"})
	got, err = o.docsSigned(flat)
	if err != nil {
		t.Fatalf("不带前缀也该认：%v", err)
	}
	if got["status"] != float64(2) {
		t.Fatalf("摊平的那种形状也要认：%v", got)
	}
	bad, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"status": 2}).
		SignedString([]byte("not-the-secret"))
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"", "   ", "Bearer ", "Bearer not-a-jwt", "Bearer " + bad} {
		if _, err := o.docsSigned(h); err == nil {
			t.Fatalf("%q 该被拒", h)
		}
	}
}

// 回调的状态和地址只从签过的那一份里读。
//
// **这是这个口最要紧的一条。** url 是我们要拿去发请求的东西；它要是能由调用方
// 在明文正文里随便写，这个口就成了一台"替你去容器网里任意地址取东西"的机器
// ——而且取回来的字节会被存成"附件的新一版"。
func TestSignedCallbackIgnoresThePlainBody(t *testing.T) {
	o := NewOffice("/docs", "the-docs-secret", "http://mail:9011")
	sign := func(c jwt.MapClaims, secret string) string {
		t.Helper()
		s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(secret))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	real := jwt.MapClaims{"status": 2, "url": "http://docs/cache/out.xlsx", "users": []string{"7"}}

	// 正文里的 token 是首选：JWT_IN_BODY 开着时它签的就是这份正文。
	cb, err := o.signedCallback(sign(real, "the-docs-secret"), "")
	if err != nil {
		t.Fatalf("签过的回调该认：%v", err)
	}
	if cb.Status != 2 || cb.URL != "http://docs/cache/out.xlsx" || len(cb.Users) != 1 {
		t.Fatalf("读出来的不是签进去的：%+v", cb)
	}
	// 正文里没有 token 时退到 Authorization 头，两种形状都行。
	cb, err = o.signedCallback("", "Bearer "+sign(jwt.MapClaims{"payload": real}, "the-docs-secret"))
	if err != nil || cb.URL != "http://docs/cache/out.xlsx" {
		t.Fatalf("头上那份也该认：%+v %v", cb, err)
	}

	// 一份**别的用途**的签名（比如交给浏览器的那份配置）不该被当成回调：
	// 它是用同一把密钥签的，光看签名分不出来，靠的是"里面有没有 status"。
	cfgLike := sign(jwt.MapClaims{
		"documentType": "cell",
		"document":     map[string]any{"key": "k", "url": "http://mail:9011/office/file?t=x"},
	}, "the-docs-secret")
	if _, err := o.signedCallback("", "Bearer "+cfgLike); err == nil {
		t.Fatal("配置的签名不该能当成回调")
	}
	// 别人签的、空的，一律拒。
	for _, c := range [][2]string{{"", ""}, {sign(real, "not-the-secret"), ""}, {"", "Bearer not-a-jwt"}} {
		if _, err := o.signedCallback(c[0], c[1]); err == nil {
			t.Fatalf("%v 该被拒", c)
		}
	}
}
