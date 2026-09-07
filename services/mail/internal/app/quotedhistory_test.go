package app

import (
	"strings"
	"testing"
)

// A body long enough to clear minQuotedChars without the test being a wall of
// prose. Real quoted history is far longer than this.
func longQuote(lead string) string {
	return lead + strings.Repeat("<p>Previous message text carried forward. </p>", 12)
}

func mustSplit(t *testing.T, body string) (string, string) {
	t.Helper()
	fresh, quoted := SplitQuotedHistory(body)
	if quoted == "" {
		t.Fatalf("nothing was folded out of:\n%s", body)
	}
	return fresh, quoted
}

// ------------------------------------------------------ the shapes clients use

func TestTheQuoteIsFoundInEveryClientsShape(t *testing.T) {
	cases := map[string]string{
		"blockquote": `<div>Confirmed, please ship Monday.</div>` +
			longQuote(`<blockquote>`) + `</blockquote>`,
		"gmail": `<div>Confirmed, please ship Monday.</div>` +
			`<div class="gmail_quote gmail_quote_container">` + longQuote(``) + `</div>`,
		"outlook id": `<div>Confirmed, please ship Monday.</div>` +
			`<div id="divRplyFwdMsg">` + longQuote(``) + `</div>`,
		"thunderbird": `<div>Confirmed, please ship Monday.</div>` +
			`<div class="moz-cite-prefix">` + longQuote(``) + `</div>`,
		"on-wrote phrase": `<div>Confirmed, please ship Monday.</div>` +
			longQuote(`<p>On Mon, 3 Mar 2026 at 10:30, Ana Costa &lt;ana@buyer.com&gt; wrote:</p>`),
		"original message rule": `<div>Confirmed, please ship Monday.</div>` +
			longQuote(`<p>-----Original Message-----</p>`),
		"outlook header block": `<div>Confirmed, please ship Monday.</div>` +
			longQuote(`<p>From: Ana Costa Sent: Monday, 3 March 2026 10:30 To: Li Na</p>`),
		"中文 原始邮件": `<div>好的，周一发货。</div>` +
			longQuote(`<p>------------------ 原始邮件 ------------------</p>`),
		"中文 写道": `<div>好的，周一发货。</div>` +
			longQuote(`<p>在 2026年3月1日，Ana Costa 写道：</p>`),
		"中文 发件人块": `<div>好的，周一发货。</div>` +
			longQuote(`<p>发件人：Ana Costa 发送时间：2026年3月1日 收件人：李娜</p>`),
		// ERP 自己的写信框加的那一行：没有日期，只有一个人和一个地址。
		// 折不掉的话，我们发出的每一条回复都在会话里把历史再摊一遍。
		"ERP 自己的回复": `<div>好的，周一发货。</div>` +
			longQuote(`<p>李娜 &lt;lina@corp.example&gt; 写道：</p>`),
		"ERP 自己的回复 英文": `<div>Confirmed, please ship Monday.</div>` +
			longQuote(`<p>Li Na &lt;lina@corp.example&gt; wrote:</p>`),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			fresh, quoted := mustSplit(t, body)
			if !strings.Contains(fresh, "Monday") && !strings.Contains(fresh, "周一") {
				t.Fatalf("the sentence somebody wrote was folded away:\n%s", fresh)
			}
			if !strings.Contains(quoted, "Previous message text") {
				t.Fatalf("the history did not land in the fold:\n%s", quoted)
			}
			if strings.Contains(fresh, "Previous message text") {
				t.Fatalf("history stayed above the fold:\n%s", fresh)
			}
		})
	}
}

// The introducing line belongs with the quote, not above it. Leaving
// "On Monday Ana wrote:" stranded above a collapsed fold reads as a sentence
// that lost its ending.
func TestTheIntroducingLineGoesIntoTheFold(t *testing.T) {
	body := `<div>Confirmed, please ship Monday.</div>` +
		longQuote(`<p>On Mon, 3 Mar 2026 at 10:30, Ana Costa wrote:</p>`)
	fresh, quoted := mustSplit(t, body)
	if strings.Contains(fresh, "wrote:") {
		t.Fatalf("the introducing line was left above the fold:\n%s", fresh)
	}
	if !strings.Contains(quoted, "wrote:") {
		t.Fatalf("the introducing line went missing:\n%s", quoted)
	}
}

// Outlook draws a rule and puts the history under it. A rule on its own is not
// evidence — plenty of signatures sit under one — so it only counts when what
// follows reads like a quoted header.
func TestARuleCountsOnlyWhenAQuoteFollowsIt(t *testing.T) {
	quoteAfterRule := `<div>Confirmed, please ship Monday.</div><hr>` +
		longQuote(`<p>From: Ana Costa Sent: Monday To: Li Na</p>`)
	if _, quoted := SplitQuotedHistory(quoteAfterRule); quoted == "" {
		t.Fatal("a rule followed by a quoted header was not folded")
	}

	sigAfterRule := `<div>Confirmed, please ship Monday.</div><hr>` +
		`<p>Li Na · Sales · AAA Industry Inc</p>` +
		strings.Repeat(`<p>Tel +86 138 0000 0000 · aaaindustryinc.com</p>`, 12)
	if _, quoted := SplitQuotedHistory(sigAfterRule); quoted != "" {
		t.Fatalf("a signature under a rule was folded as history:\n%s", quoted)
	}
}

// ----------------------------------------------------------- when not to fold

// Folding is not free: it costs a click and it hides something. Below a
// certain size the quote is context rather than clutter.
func TestAShortQuoteIsLeftWhereItIs(t *testing.T) {
	body := `<div>Confirmed, please ship Monday.</div>` +
		`<blockquote><p>Can you ship Monday?</p></blockquote>`
	if fresh, quoted := SplitQuotedHistory(body); quoted != "" || fresh != body {
		t.Fatalf("a four-word quote was folded:\n%s", quoted)
	}
}

// A mail that is *only* a forward has no fresh part. Folding it would leave a
// page with a button on it, which reads as a broken mail.
func TestAMailThatIsNothingButAForwardIsLeftWhole(t *testing.T) {
	body := longQuote(`<p>---------- Forwarded message ----------</p>`)
	if _, quoted := SplitQuotedHistory(body); quoted != "" {
		t.Fatal("a mail with no fresh content was folded into nothing")
	}
	// Whitespace is not content either: Outlook writes an empty div above the
	// header block on a plain forward.
	if _, quoted := SplitQuotedHistory(`<div>&nbsp;</div>` + body); quoted != "" {
		t.Fatal("an empty div was treated as a reply worth showing above a fold")
	}
}

// A short reply is still a reply. The floor here used to be a character count,
// which meant "Confirmed, please ship Monday." folded and "好的，周一发货。"
// did not — the same sentence, one third the characters, and the language this
// product is actually used in. Both fold now, and so does "ok".
func TestAShortReplyStillFoldsWhateverLanguageItIsIn(t *testing.T) {
	for _, reply := range []string{
		`<div>Confirmed, please ship Monday.</div>`,
		`<div>好的，周一发货。</div>`,
		`<div>收到</div>`,
		`<div>ok</div>`,
	} {
		body := reply + `<div class="gmail_quote">` + longQuote(``) + `</div>`
		if _, quoted := SplitQuotedHistory(body); quoted == "" {
			t.Errorf("%s did not fold the history below it", reply)
		}
	}
}

// An unrecognised shape is left whole. A reader who has to click to find the
// sentence written to them is worse off than one who scrolls.
func TestAnUnrecognisedShapeIsLeftWhole(t *testing.T) {
	body := `<div>Here is the price list.</div>` +
		strings.Repeat(`<p>Item, quantity, unit price, total.</p>`, 20)
	fresh, quoted := SplitQuotedHistory(body)
	if quoted != "" {
		t.Fatalf("an ordinary long mail was folded:\n%s", quoted)
	}
	if fresh != body {
		t.Fatal("the body was altered when nothing was folded")
	}
}

// The words "original message" in the middle of a paragraph are not a client
// announcing a quote.
func TestThePhraseMustOpenTheBlockNotAppearInIt(t *testing.T) {
	body := `<div>Please resend the original message, I cannot find it.</div>` +
		strings.Repeat(`<p>We still need the packing list and the invoice.</p>`, 15)
	if _, quoted := SplitQuotedHistory(body); quoted != "" {
		t.Fatalf("a phrase inside a sentence triggered a fold:\n%s", quoted)
	}
}

// A blockquote three divs deep inside a marketing layout is part of that
// layout. Splitting there would cut the layout in half and render two broken
// documents.
func TestADeeplyNestedQuoteIsNotSplitOut(t *testing.T) {
	body := `<div class="wrapper"><table><tr><td><div>Hello</div>` +
		longQuote(`<blockquote>`) + `</blockquote></td></tr></table></div>`
	if _, quoted := SplitQuotedHistory(body); quoted != "" {
		t.Fatal("a nested quote was cut out of its container")
	}
}

// ----------------------------------------------------------- well-formedness

// The halves are rendered as two separate documents. Cutting the string at an
// index would leave an open tag above and a stray closing tag below, and each
// browser would repair them differently — which is why this parses.
func TestBothHalvesAreWellFormed(t *testing.T) {
	body := `<div class="outer"><p>Confirmed, please ship Monday.</p></div>` +
		`<div class="gmail_quote"><table><tr><td>` +
		strings.Repeat(`<p>Previous message text carried forward. </p>`, 12) +
		`</td></tr></table></div>`
	fresh, quoted := mustSplit(t, body)
	for name, half := range map[string]string{"fresh": fresh, "quoted": quoted} {
		if strings.Count(half, "<div") != strings.Count(half, "</div>") {
			t.Errorf("%s half has unbalanced divs:\n%s", name, half)
		}
		if strings.Count(half, "<table") != strings.Count(half, "</table>") {
			t.Errorf("%s half has unbalanced tables:\n%s", name, half)
		}
	}
}

// The sender's stylesheet goes to both halves. The quote is a document of its
// own, and without the CSS it opens into something that looks nothing like
// the mail it came out of.
func TestTheSendersStylesheetIsGivenToBothHalves(t *testing.T) {
	body := `<style>.q{color:#666}</style><div>Confirmed, please ship Monday.</div>` +
		`<div class="gmail_quote">` + longQuote(``) + `</div>`
	fresh, quoted := mustSplit(t, body)
	for name, half := range map[string]string{"fresh": fresh, "quoted": quoted} {
		if !strings.Contains(half, ".q{color:#666}") {
			t.Errorf("%s half lost the stylesheet:\n%s", name, half)
		}
	}
}

// Nothing may be lost across the split: every word that was in the body has
// to be in one half or the other.
func TestNoTextIsLostInTheSplit(t *testing.T) {
	body := `<div>Confirmed, please ship Monday.</div>` +
		`<div class="gmail_quote"><p>On Monday Ana wrote:</p>` +
		strings.Repeat(`<p>Previous message text carried forward. </p>`, 12) + `</div>`
	fresh, quoted := mustSplit(t, body)
	whole := collapseSpaces(textOf(body))
	rejoined := collapseSpaces(textOf(fresh) + " " + textOf(quoted))
	if len(rejoined) < len(whole) {
		t.Fatalf("text was lost:\nwhole (%d): %s\nrejoined (%d): %s",
			len(whole), whole, len(rejoined), rejoined)
	}
	for _, want := range []string{"ship Monday", "On Monday Ana wrote", "carried forward"} {
		if !strings.Contains(rejoined, want) {
			t.Errorf("%q went missing across the split", want)
		}
	}
}

// The real thing this exists for: a thread where the last message is the first
// fifteen stacked up. The fold has to leave only the newest turn.
func TestALongReplyChainFoldsToItsNewestTurn(t *testing.T) {
	var chain strings.Builder
	chain.WriteString(`<div>Latest: we can hold the price until Friday.</div>`)
	chain.WriteString(`<blockquote>`)
	for i := 15; i > 0; i-- {
		chain.WriteString(`<p>On an earlier day somebody wrote a paragraph about the order. </p>`)
	}
	chain.WriteString(`</blockquote>`)

	fresh, quoted := mustSplit(t, chain.String())
	freshLen := len([]rune(textOf(fresh)))
	quotedLen := len([]rune(textOf(quoted)))
	if freshLen > quotedLen/4 {
		t.Fatalf("the fold barely shrank anything: fresh %d chars, folded %d", freshLen, quotedLen)
	}
	if !strings.Contains(fresh, "hold the price until Friday") {
		t.Fatal("the newest turn did not survive above the fold")
	}
}

// 正文里出现「他写道：」这种句子不能当成引用的开头——那一行得带个尖括号
// 地址才算数。否则一封讲述别人说了什么的信会被拦腰折断。
func TestASentenceThatMerelySaysWroteIsNotAQuoteHeader(t *testing.T) {
	body := `<p>客户在电话里写道：这批货要提前。</p>` +
		`<p>他写道：下周一之前必须发出。</p>` +
		`<p>` + strings.Repeat("以上是今天沟通的全部内容。", 20) + `</p>`
	fresh, quoted := SplitQuotedHistory(body)
	if quoted != "" {
		t.Errorf("不该折：这几句都是正文\nfresh=%s\nquoted=%s", fresh, quoted)
	}
}
