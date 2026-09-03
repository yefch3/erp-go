package app

import (
	"net/mail"
	"regexp"
	"strings"
)

// MailParty 是邮件头里的一个人：显示名可空，地址小写。
//
// 「回复全部」要的就是这个——把 To 和 Cc 两段头拆成一个个地址，去掉自己，
// 剩下的进抄送。拆在服务端而不是前端：前端对着 "Name <a@x>, b@y" 这种东西
// 写解析器，第一个带逗号的显示名（"Gomez, Ana" <…>）就会把它拆错。
type MailParty struct {
	Name  string
	Email string
}

// parseParties 把一段地址头拆成人。
//
// 先按 RFC 5322 解（能处理带引号、带逗号的显示名）；解不动的头——现实里
// 的邮件头什么样都有，分号分隔、没引号的中文名、尖括号不配对——退回逐段
// 松散解析，宁可多认几个也别整段丢掉。地址小写、去重、保持原顺序。
func parseParties(header string) []MailParty {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil
	}
	var out []MailParty
	seen := map[string]bool{}
	add := func(name, email string) {
		email = strings.ToLower(strings.TrimSpace(email))
		// 只认长得像地址的。这一份会被「回复全部」原样拿去发信：一个从坏头里
		// 抠出来的 "jane doe jane@x.com" 进了抄送，就是一次注定被退的投递。
		if !looksLikeAddress(email) || seen[email] {
			return
		}
		seen[email] = true
		out = append(out, MailParty{Name: strings.TrimSpace(name), Email: email})
	}
	if addrs, err := mail.ParseAddressList(header); err == nil {
		for _, a := range addrs {
			add(a.Name, a.Address)
		}
		return out
	}
	// 松散那条路：整段头里只要有一个人写得不规范（显示名后面直接跟地址，没有
	// 尖括号），RFC 那条路就整段失败，于是每一段都走到这里。分号和逗号都当
	// 分隔符——Outlook 导出的头常用分号。
	//
	// 每一段只**抠**地址，不整段当地址：地址是那一段里唯一带 @ 的词，剩下的
	// 才是名字。
	for _, piece := range strings.FieldsFunc(header, func(r rune) bool { return r == ',' || r == ';' }) {
		m := addressInText.FindStringIndex(piece)
		if m == nil {
			continue
		}
		email := piece[m[0]:m[1]]
		name := strings.Trim(strings.TrimSpace(piece[:m[0]]+piece[m[1]:]), ` "'<>`)
		add(name, email)
	}
	return out
}

// 一段文字里的邮箱地址：本地部分 @ 至少带一个点的域名。
var addressInText = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// looksLikeAddress 挡的是拿去发信会被退的东西：带空格的、没有 @ 的、域名
// 没有点的。net/mail 认 a@b，我们不认——客户邮件里不会有那种地址，倒是坏头
// 里抠出来的碎片长那样。
func looksLikeAddress(email string) bool {
	if email == "" || strings.ContainsAny(email, " \t<>\"'") {
		return false
	}
	at := strings.IndexByte(email, '@')
	if at <= 0 || at != strings.LastIndexByte(email, '@') {
		return false
	}
	return strings.Contains(email[at+1:], ".")
}
