#!/bin/sh
# 查一个域名的发信身份三件套：SPF、DKIM、DMARC。只读，只查公开 DNS。
#
# 为什么这个仓库需要它：这套系统**不是邮件服务器**。每一封信都是以员工
# 自己的邮箱身份、用他自己的授权码，连到他的邮箱服务商的 SMTP 发出去的
# （services/mail/internal/adapter/provider/pick.go：「mail will be sent
# over SMTP as each employee's own mailbox」）。
#
# 这件事决定了三条记录该怎么写，而网上大部分教程是写给「在 VPS 上自建
# Postfix」的人的，照抄会出事：
#
#   * SPF 要授权的是**邮箱服务商的发信服务器**（include:），不是这台
#     跑 ERP 的 VPS 的 IP。信根本不从这台机器出去，把它的 IP 写进去
#     不解决任何问题；而如果照教程把原来的 include 换成了 ip4:，
#     全公司的信当场开始 SPF 失败。
#   * DKIM 的密钥对由**邮箱服务商**生成、在他们后台开启，这个仓库里
#     没有任何出站签名代码（grep dkim 只有解析收件头的部分）。
#   * DMARC 是纯策略，谁托管都一样，但它依赖前两条先立起来。
#
# 用法：
#   sh scripts/check-mail-dns.sh yourcompany.com
#
# 退出码：0 = 三条都在且看起来合理；1 = 有缺失或可疑，正文里说了是哪条。
set -eu

domain="${1:-}"
if [ -z "$domain" ]; then
	echo "用法：sh scripts/check-mail-dns.sh <域名>" >&2
	echo "例：  sh scripts/check-mail-dns.sh yourcompany.com" >&2
	exit 2
fi

if ! command -v dig >/dev/null 2>&1; then
	echo "需要 dig（macOS 自带；Debian/Ubuntu：apt install dnsutils）" >&2
	exit 2
fi

# 用公共解析器，避开本机 DNS 缓存和内网劫持——查的是「全世界看到的是什么」。
RESOLVER="${MAIL_DNS_RESOLVER:-8.8.8.8}"
q() { dig +short "$2" "$1" "@$RESOLVER" 2>/dev/null | sed 's/^"//; s/"$//; s/" "//g'; }

bad=0
echo "域名：$domain（解析器 $RESOLVER）"
echo

# ── 谁在收这个域名的信 ────────────────────────────────────────
# MX 是判断「这个域名托管在谁那里」最直接的证据，而托管方决定了 SPF 该
# include 谁、DKIM 去哪个后台开。
echo "── 邮箱托管在哪（MX） ──"
mx=$(q "$domain" MX || true)
if [ -z "$mx" ]; then
	echo "  ✗ 没有 MX 记录——这个域名收不了信。"
	bad=1
else
	printf '%s\n' "$mx" | sed 's/^/  /'
	case "$mx" in
		*qq.com*|*exmail*) echo "  → 腾讯企业邮箱" ;;
		*aliyun*|*mxhichina*) echo "  → 阿里云企业邮箱" ;;
		*263*) echo "  → 263 企业邮箱" ;;
		*google*|*googlemail*) echo "  → Google Workspace" ;;
		*outlook*|*microsoft*) echo "  → Microsoft 365" ;;
		*) echo "  → 认不出来是哪家，下面的 SPF include 要按服务商文档填" ;;
	esac
fi
echo

# ── SPF ───────────────────────────────────────────────────────
echo "── SPF（谁有权代表这个域名发信） ──"
spf=$(q "$domain" TXT | grep -i '^v=spf1' || true)
if [ -z "$spf" ]; then
	echo "  ✗ 没有 SPF 记录。收信方无从判断发信来源是否合法，很容易进垃圾箱。"
	bad=1
else
	count=$(printf '%s\n' "$spf" | wc -l | tr -d ' ')
	printf '%s\n' "$spf" | sed 's/^/  /'
	# 一个域名只允许有一条 SPF。两条的后果不是「取并集」，是**两条都失效**
	# （PermError），比一条都没有还糟——这是加记录时最常见的事故。
	if [ "$count" -gt 1 ]; then
		echo "  ✗ 有 $count 条 SPF！一个域名只能有一条，多了会 PermError，等于全都不作数。"
		echo "    要加来源就合并进同一条的 include: 里，不要新开一条 TXT。"
		bad=1
	fi
	case "$spf" in
		*ip4:*|*ip6:*)
			echo "  ! 记录里写了裸 IP。确认那是真正把信发出去的机器——"
			echo "    这套系统经员工邮箱服务商中转，跑 ERP 的那台 VPS 的 IP 写进来没有用。" ;;
	esac
	case "$spf" in
		*" -all"*) echo "  · 结尾 -all（严格：不在名单里的一律拒收）" ;;
		*" ~all"*) echo "  · 结尾 ~all（软失败：不在名单里的标记可疑，先这样最稳）" ;;
		*" ?all"*|*" +all"*)
			echo "  ✗ 结尾是 ?all 或 +all，等于谁都能冒充你发信，SPF 形同虚设。"
			bad=1 ;;
		*) echo "  ! 没看到 all 机制结尾，多数收信方会当成宽松处理。" ;;
	esac
fi
echo

# ── DKIM ──────────────────────────────────────────────────────
echo "── DKIM（邮件有没有数字签名） ──"
# DKIM 查不了「有没有」，只能查「某个 selector 在不在」——selector 是服务商
# 定的，DNS 上没有目录可以列举。所以这里探几个常见的，探不到不等于没配。
# 先探一个不可能存在的 selector。有的域名挂了 *._domainkey 通配符，
# 那样逐个探测毫无意义——每个名字都「查得到」。
wildcard=$(q "zz-no-such-selector-zz._domainkey.${domain}" TXT || true)
if [ -n "$wildcard" ]; then
	echo "  这个域名挂了 *._domainkey 通配符，逐个探 selector 问不出东西："
	printf '%s\n' "$wildcard" | sed 's/^/    /'
	case "$wildcard" in
		*p=\;*|*"p="|*"p= "*)
			echo "  · 通配返回的 p= 是空的——按 RFC 6376 这是「本域不签名，所有 selector 作废」，"
			echo "    是一种明确的声明，不是漏配。" ;;
	esac
else
	found_dkim=""
	for sel in default s1 s2 s1024 dkim mail google selector1 selector2 mxdomain qcloud; do
		rec=$(q "${sel}._domainkey.${domain}" TXT | grep -i 'v=dkim1\|k=rsa\|p=' || true)
		[ -n "$rec" ] || continue
		# p= 后面空着 = 这个 selector 被吊销了，不是「配好了」。
		key=$(printf '%s' "$rec" | sed -n 's/.*[;[:space:]]*p=\([A-Za-z0-9+/=]*\).*/\1/p')
		if [ -z "$key" ]; then
			echo "  · ${sel}._domainkey 存在但 p= 是空的——这个 selector 已作废，不会签名。"
		else
			echo "  ✓ ${sel}._domainkey（公钥 ${#key} 字符）"
			found_dkim="yes"
		fi
	done
	if [ -z "$found_dkim" ]; then
		echo "  ? 常见 selector 都没查到。这**不能**证明没配——selector 名字由服务商定，"
		echo "    DNS 不支持列举。去邮箱服务商后台看「DKIM/域名签名」那一项，"
		echo "    它会告诉你 selector 叫什么。"
	fi
fi
echo

# ── DMARC ─────────────────────────────────────────────────────
echo "── DMARC（前两条没过时怎么办） ──"
dmarc=$(q "_dmarc.$domain" TXT | grep -i '^v=dmarc1' || true)
if [ -z "$dmarc" ]; then
	echo "  ✗ 没有 DMARC 记录。"
	echo "    建议从最温和的一档起步，先只收报告、不影响投递："
	echo "    _dmarc  TXT  \"v=DMARC1; p=none; rua=mailto:admin@$domain\""
	bad=1
else
	printf '%s\n' "$dmarc" | sed 's/^/  /'
	case "$dmarc" in
		*p=none*)   echo "  · p=none：只观察不拦截。适合刚上线的头几周，看 rua 报告确认没误伤，再收紧。" ;;
		*p=quarantine*) echo "  · p=quarantine：验不过的丢进垃圾箱。" ;;
		*p=reject*) echo "  · p=reject：验不过的直接退信。最严，确认 SPF/DKIM 稳了再上。" ;;
	esac
	case "$dmarc" in
		*rua=*) ;;
		*) echo "  ! 没有 rua=，收不到汇总报告，等于蒙着眼睛调策略。" ;;
	esac
fi
echo

if [ "$bad" -ne 0 ]; then
	echo "结论：有缺失，见上面标 ✗ 的几条。"
	exit 1
fi
echo "结论：三条都在，看起来合理。"
