package app

import (
	"bytes"
	"encoding/csv"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"github.com/sgao19/erp-go/pkg/apierr"
)

// Parsing the bank's CSV. There is no standard: every bank portal exports
// its own column names, its own date format, its own idea of how to say
// "money left". This parser recognizes the common Chinese and English
// spellings of each and reports everything else as a per-row error with the
// row number — finance fixes the file in Excel and uploads again, and the
// bank_ref dedupe makes the second upload harmless.

// BankStatementRow is one parsed statement line.
type BankStatementRow struct {
	RowNo        int
	TxnDate      string // YYYY-MM-DD
	Direction    string // DEBIT | CREDIT
	Amount       decimal.Decimal
	Currency     string
	Counterparty string
	BankRef      string
	Remark       string
}

// BankRowError names the row and the reason; the row stays out, the rest of
// the file still imports.
type BankRowError struct {
	RowNo  int
	Reason string
}

// Column aliases, lowercase. The first header cell matching an alias claims
// the column.
var bankHeaderAliases = map[string][]string{
	"date":         {"日期", "交易日期", "记账日期", "交易时间", "date", "txn date", "transaction date", "value date"},
	"amount":       {"金额", "交易金额", "发生额", "amount", "交易额"},
	"currency":     {"币种", "货币", "币别", "currency", "ccy"},
	"direction":    {"方向", "借贷", "借贷标志", "收支", "收支类型", "direction", "dr/cr", "type"},
	"counterparty": {"对方户名", "对方名称", "对方账户名", "收款人", "收/付款人", "counterparty", "payee", "beneficiary"},
	"ref":          {"流水号", "交易流水号", "银行流水号", "凭证号", "交易参考号", "reference", "ref", "ref no", "transaction id", "txn id"},
	"remark":       {"备注", "摘要", "用途", "附言", "remark", "memo", "narrative", "description"},
}

var bankDebitWords = map[string]bool{
	"借": true, "付": true, "支出": true, "出账": true, "付款": true,
	"d": true, "dr": true, "debit": true, "out": true,
}
var bankCreditWords = map[string]bool{
	"贷": true, "收": true, "收入": true, "入账": true, "收款": true,
	"c": true, "cr": true, "credit": true, "in": true,
}

// parseBankStatementCSV turns the uploaded bytes into rows plus per-row
// errors. A file-level error (unreadable, no header, no usable columns)
// aborts; row-level problems never do.
func parseBankStatementCSV(data []byte, defaultCurrency string) ([]BankStatementRow, []BankRowError, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	// Chinese bank portals export GBK more often than not; a file that is
	// not valid UTF-8 gets one decode attempt before we give up.
	if !utf8.Valid(data) {
		decoded, err := io.ReadAll(transform.NewReader(bytes.NewReader(data), simplifiedchinese.GBK.NewDecoder()))
		if err != nil {
			return nil, nil, apierr.Invalid("BANK_CSV_ENCODING", "文件既不是 UTF-8 也不是 GBK 编码")
		}
		data = decoded
	}

	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true

	records, err := r.ReadAll()
	if err != nil {
		return nil, nil, apierr.Invalid("BANK_CSV_MALFORMED", "CSV 无法解析："+err.Error())
	}

	// The header is the first row with a recognizable date and ref column.
	cols := map[string]int{}
	headerAt := -1
	for i, rec := range records {
		candidate := bankHeaderColumns(rec)
		if candidate["date"] >= 0 && candidate["ref"] >= 0 && candidate["amount"] >= 0 {
			for k, v := range candidate {
				if v >= 0 {
					cols[k] = v
				}
			}
			headerAt = i
			break
		}
	}
	if headerAt < 0 {
		return nil, nil, apierr.Invalid("BANK_CSV_HEADER",
			"没找到表头——至少需要能认出日期、金额和流水号三列（支持中英文常见写法）")
	}

	defaultCurrency = strings.ToUpper(strings.TrimSpace(defaultCurrency))
	var rows []BankStatementRow
	var rowErrs []BankRowError
	for i := headerAt + 1; i < len(records); i++ {
		rec := records[i]
		rowNo := i + 1 // 1-based, header included — matches what Excel shows
		if isBlankRecord(rec) {
			continue
		}
		cell := func(name string) string {
			idx, ok := cols[name]
			if !ok || idx >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[idx])
		}

		row := BankStatementRow{RowNo: rowNo,
			Counterparty: cell("counterparty"), BankRef: cell("ref"), Remark: cell("remark")}
		if row.BankRef == "" {
			rowErrs = append(rowErrs, BankRowError{rowNo, "缺少银行流水号"})
			continue
		}
		date, ok := normalizeBankDate(cell("date"))
		if !ok {
			rowErrs = append(rowErrs, BankRowError{rowNo, "日期无法识别：" + cell("date")})
			continue
		}
		row.TxnDate = date

		amount, ok := normalizeBankAmount(cell("amount"))
		if !ok || amount.IsZero() {
			rowErrs = append(rowErrs, BankRowError{rowNo, "金额无法识别：" + cell("amount")})
			continue
		}
		// Direction: the column when present, the sign when not. A bank
		// statement's negative number means money left the account.
		switch dir := strings.ToLower(cell("direction")); {
		case bankDebitWords[dir]:
			row.Direction = "DEBIT"
		case bankCreditWords[dir]:
			row.Direction = "CREDIT"
		case dir != "":
			rowErrs = append(rowErrs, BankRowError{rowNo, "借贷方向无法识别：" + cell("direction")})
			continue
		case amount.IsNegative():
			row.Direction = "DEBIT"
		default:
			row.Direction = "CREDIT"
		}
		row.Amount = amount.Abs()

		row.Currency = strings.ToUpper(cell("currency"))
		if row.Currency == "" {
			row.Currency = defaultCurrency
		}
		if row.Currency == "" || len(row.Currency) > 8 {
			rowErrs = append(rowErrs, BankRowError{rowNo, "缺少币种（文件没有币种列时请在导入时指定）"})
			continue
		}
		rows = append(rows, row)
	}
	return rows, rowErrs, nil
}

func bankHeaderColumns(rec []string) map[string]int {
	out := map[string]int{}
	for k := range bankHeaderAliases {
		out[k] = -1
	}
	for i, cell := range rec {
		cell = strings.ToLower(strings.TrimSpace(cell))
		if cell == "" {
			continue
		}
		for key, aliases := range bankHeaderAliases {
			if out[key] >= 0 {
				continue
			}
			for _, a := range aliases {
				if cell == a {
					out[key] = i
					break
				}
			}
		}
	}
	return out
}

func isBlankRecord(rec []string) bool {
	for _, c := range rec {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// normalizeBankDate accepts the formats bank portals actually emit:
// 2026-08-22, 2026/08/22, 2026.08.22, 20260822, each optionally followed by
// a time of day.
func normalizeBankDate(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if i := strings.IndexAny(v, " T"); i > 0 {
		v = v[:i]
	}
	v = strings.NewReplacer("/", "-", ".", "-").Replace(v)
	if len(v) == 8 && !strings.Contains(v, "-") {
		v = v[:4] + "-" + v[4:6] + "-" + v[6:]
	}
	parts := strings.Split(v, "-")
	if len(parts) != 3 || len(parts[0]) != 4 {
		return "", false
	}
	if len(parts[1]) == 1 {
		parts[1] = "0" + parts[1]
	}
	if len(parts[2]) == 1 {
		parts[2] = "0" + parts[2]
	}
	v = strings.Join(parts, "-")
	for _, c := range v {
		if c != '-' && (c < '0' || c > '9') {
			return "", false
		}
	}
	return v, true
}

// normalizeBankAmount strips the decorations spreadsheets add — thousands
// separators, currency signs, whitespace — and keeps the sign.
func normalizeBankAmount(v string) (decimal.Decimal, bool) {
	v = strings.NewReplacer(",", "", "，", "", "￥", "", "¥", "", "$", "", " ", "").Replace(strings.TrimSpace(v))
	if v == "" {
		return decimal.Zero, false
	}
	d, err := decimal.NewFromString(v)
	if err != nil {
		return decimal.Zero, false
	}
	return d, true
}
