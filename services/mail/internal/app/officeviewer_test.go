package app

import (
	"regexp"
	"testing"
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

// 缓存键：同一个对象键永远同一个值，形状符合 OnlyOffice 的要求。
func TestOfficeDocKeyIsStableAndWellFormed(t *testing.T) {
	a := officeDocKey("mail/12/att/7/packing.xlsx")
	b := officeDocKey("mail/12/att/7/packing.xlsx")
	if a != b {
		t.Fatalf("同一个对象键要得到同一个缓存键：%q vs %q", a, b)
	}
	if c := officeDocKey("mail/12/att/8/packing.xlsx"); c == a {
		t.Fatal("不同的对象键不该撞成同一个缓存键")
	}
	// OnlyOffice：只许 0-9 a-z A-Z -._=，最长 128。
	if !regexp.MustCompile(`^[0-9A-Za-z._=-]{1,128}$`).MatchString(a) {
		t.Fatalf("缓存键的形状不合要求：%q", a)
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

// 地址或密钥缺一个都算没配。只有地址没有密钥尤其不能算配好了。
func TestNewOfficeRequiresBothAddressAndSecret(t *testing.T) {
	if NewOffice("", "s") != nil || NewOffice("/docs", "") != nil || NewOffice("  ", "  ") != nil {
		t.Fatal("缺地址或缺密钥都该是 nil")
	}
	o := NewOffice("https://erp.example/docs/", "s")
	if o == nil || o.PublicURL != "https://erp.example/docs" {
		t.Fatalf("末尾的斜杠该去掉：%+v", o)
	}
}
