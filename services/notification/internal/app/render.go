package app

import (
	"regexp"
	"sort"
	"strings"
)

// Variables use {{ }} delimiters rather than bare words. A bare token would
// be substituted wherever it appeared, including inside the prose — a mail
// that happens to discuss "contact_name" would have the sentence rewritten.
var variablePattern = regexp.MustCompile(`\{\{\s*([a-z_][a-z0-9_]*)\s*\}\}`)

// Recipient carries everything a variable can resolve against.
// Tags matter here beyond style: these values are stored as JSONB in a
// draft and read back, so the key names are part of the stored format.
type Recipient struct {
	ContactID    int64  `json:"contactId,omitempty"`
	Name         string `json:"name,omitempty"`
	Email        string `json:"email"`
	CustomerID   int64  `json:"customerId,omitempty"`
	CustomerName string `json:"customerName,omitempty"`
	// Anything else the caller wants addressable, e.g. contract_no.
	Extra map[string]string `json:"extra,omitempty"`
}

// Sender is the person the mail goes out as.
type Sender struct {
	ID    int64
	Name  string
	Title string
	Email string
	Phone string
}

// values flattens a recipient and sender into the substitution table. Keys
// here are the whole vocabulary a template may use.
func values(r Recipient, s Sender) map[string]string {
	v := map[string]string{
		"contact_name":       r.Name,
		"contact_first_name": firstName(r.Name),
		"company_name":       r.CustomerName,
		"my_name":            s.Name,
		"my_title":           s.Title,
		"my_email":           s.Email,
		"my_phone":           s.Phone,
	}
	for k, val := range r.Extra {
		v[k] = val
	}
	return v
}

// firstName takes the personal name out of "John Smith" so a template can
// greet somebody the way their own colleagues would. Latin scripts put it
// first; CJK names are short and idiomatically used whole, so they are left
// alone rather than sliced in the wrong place.
func firstName(full string) string {
	full = strings.TrimSpace(full)
	if full == "" {
		return ""
	}
	if strings.ContainsAny(full, " \t") && isLatin(full) {
		return strings.Fields(full)[0]
	}
	return full
}

func isLatin(s string) bool {
	for _, r := range s {
		if r > 0x24F && r != ' ' && r != '\t' && r != '-' && r != '.' && r != '\'' {
			return false
		}
	}
	return true
}

// Render substitutes every variable, or reports the ones it could not.
//
// A template that cannot be fully resolved is never sent. "Dear ," reaching a
// customer is worse than the mail not going at all: it is visibly generated,
// it is visibly broken, and it cannot be taken back. The caller routes these
// to a person instead.
func Render(tpl string, r Recipient, s Sender) (string, []string) {
	return RenderAs(tpl, r, s, FormatText)
}

// RenderAs is Render with the output format known.
//
// The format matters because of escaping: a customer legitimately called
// "Smith & Sons <Trading>" would break the surrounding markup if dropped
// into HTML raw, and a hostile value could do worse. Escaping happens on the
// substituted value only — never on the template, which is meant to contain
// markup.
func RenderAs(tpl string, r Recipient, s Sender, format string) (string, []string) {
	table := values(r, s)
	asHTML := format == FormatHTML
	missing := map[string]bool{}
	out := variablePattern.ReplaceAllStringFunc(tpl, func(match string) string {
		key := variablePattern.FindStringSubmatch(match)[1]
		val, known := table[key]
		if !known || strings.TrimSpace(val) == "" {
			missing[key] = true
			return match
		}
		if asHTML {
			return escapeForHTML(val)
		}
		return val
	})
	if len(missing) == 0 {
		return out, nil
	}
	keys := make([]string, 0, len(missing))
	for k := range missing {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return out, keys
}

// VariablesIn lists the variables a template uses, for the editor.
func VariablesIn(tpl string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range variablePattern.FindAllStringSubmatch(tpl, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// KnownVariables is what the editor offers as insertable tags, so nobody has
// to remember the spelling.
func KnownVariables() []string {
	return []string{
		"contact_name", "contact_first_name", "company_name",
		"my_name", "my_title", "my_email", "my_phone",
	}
}
