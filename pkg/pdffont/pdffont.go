// Package pdffont carries the one CJK font every generated PDF uses.
//
// It exists because there were two of it. The same 17 MB Noto Sans SC sat in
// services/export and services/procurement, byte for byte identical, each with
// its own copy of the two lines below. Two copies of a binary asset do not stay
// identical: one gets replaced, the other does not, and the day that matters is
// the day a customer asks why the 采购订单 and the 报价单 for the same deal do
// not look alike. Nothing in a build would have caught it.
//
// The font is embedded rather than read from disk on purpose. A missing font
// file is a runtime failure in a container somebody deploys on a Friday; an
// embedded one either compiles or does not.
package pdffont

import (
	_ "embed"

	"codeberg.org/go-pdf/fpdf"
)

// Name is the family name to pass to SetFont. Exported so callers do not
// repeat the string: if it ever changes, a literal somewhere else would keep
// compiling and fail at run time with a font fpdf has never heard of.
const Name = "NotoSansSC"

//go:embed NotoSansSC-VF.ttf
var data []byte

// Register makes Name available on this document in both weights.
//
// Regular and bold come from the same bytes because this is a variable font —
// there is no separate bold file to ship. Registering only the regular weight
// compiles, renders, and silently gives you no bold anywhere, which is the kind
// of thing that reaches a customer before it reaches a developer.
func Register(pdf *fpdf.Fpdf) {
	pdf.AddUTF8FontFromBytes(Name, "", data)
	pdf.AddUTF8FontFromBytes(Name, "B", data)
}
