package app

import (
	"net/mail"
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
		if email == "" || !strings.Contains(email, "@") || seen[email] {
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
	// 松散那条路：分号和逗号都当分隔符——Outlook 导出的头常用分号。
	for _, piece := range strings.FieldsFunc(header, func(r rune) bool { return r == ',' || r == ';' }) {
		email, name := looseAddress(piece)
		if email == "" {
			// looseAddress 只认尖括号；裸地址它给不出来。
			if strings.Contains(piece, "@") {
				email = strings.Trim(strings.TrimSpace(piece), `"' `)
			}
		}
		add(name, email)
	}
	return out
}
