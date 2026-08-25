package app

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// Every bank portal exports its own dialect. These cases are the dialects
// the parser promises to read; anything else must fail with a row number
// the clerk can find in Excel, never silently drop.
func TestParseBankStatementCSV(t *testing.T) {
	cases := []struct {
		name       string
		csv        string
		defaultCcy string
		wantRows   int
		wantErrs   int
		check      func(t *testing.T, rows []BankStatementRow, errs []BankRowError)
	}{
		{
			name: "standard Chinese header with 借贷 column",
			csv: "交易日期,借贷,交易金额,币种,对方户名,银行流水号,摘要\n" +
				"2026-08-20,借,10000.00,USD,鞍山钢厂,TXN001,货款\n" +
				"2026-08-21,贷,5000.00,USD,海外客户,TXN002,回款\n",
			wantRows: 2,
			check: func(t *testing.T, rows []BankStatementRow, _ []BankRowError) {
				if rows[0].Direction != "DEBIT" || rows[1].Direction != "CREDIT" {
					t.Fatalf("借→DEBIT 贷→CREDIT: %+v", rows)
				}
				if rows[0].Amount.String() != "10000" || rows[0].BankRef != "TXN001" {
					t.Fatalf("row values: %+v", rows[0])
				}
			},
		},
		{
			name: "English header, signed amounts, no direction column",
			csv: "Date,Amount,Currency,Payee,Reference\n" +
				"2026/08/20,-3000.50,USD,Mill Co,REF-1\n" +
				"2026/08/21,+1200.00,USD,Customer,REF-2\n",
			wantRows: 2,
			check: func(t *testing.T, rows []BankStatementRow, _ []BankRowError) {
				if rows[0].Direction != "DEBIT" || rows[0].Amount.String() != "3000.5" {
					t.Fatalf("negative means money left: %+v", rows[0])
				}
				if rows[1].Direction != "CREDIT" {
					t.Fatalf("positive means money arrived: %+v", rows[1])
				}
			},
		},
		{
			name: "no currency column, default supplied; thousands separators; datetime; compact date",
			csv: "日期,金额,方向,流水号\n" +
				"2026-08-20 14:33:05,\"1,234.56\",支出,A1\n" +
				"20260821,￥890,收入,A2\n",
			defaultCcy: "usd",
			wantRows:   2,
			check: func(t *testing.T, rows []BankStatementRow, _ []BankRowError) {
				if rows[0].Currency != "USD" || rows[0].TxnDate != "2026-08-20" || rows[0].Amount.String() != "1234.56" {
					t.Fatalf("normalization: %+v", rows[0])
				}
				if rows[1].TxnDate != "2026-08-21" || rows[1].Amount.String() != "890" {
					t.Fatalf("compact date and yen sign: %+v", rows[1])
				}
			},
		},
		{
			name: "bad rows report their Excel row number and spare the good ones",
			csv: "日期,金额,币种,流水号\n" +
				"2026-08-20,100,USD,B1\n" +
				"某年某月,100,USD,B2\n" +
				"2026-08-22,abc,USD,B3\n" +
				"2026-08-23,100,USD,\n",
			wantRows: 1,
			wantErrs: 3,
			check: func(t *testing.T, _ []BankStatementRow, errs []BankRowError) {
				if errs[0].RowNo != 3 || errs[1].RowNo != 4 || errs[2].RowNo != 5 {
					t.Fatalf("row numbers must match what Excel shows: %+v", errs)
				}
			},
		},
		{
			name: "missing currency with no default is a row error, not a guess",
			csv: "日期,金额,流水号\n" +
				"2026-08-20,100,C1\n",
			wantRows: 0,
			wantErrs: 1,
		},
		{
			name: "preamble lines before the real header are skipped",
			csv: "招商银行交易明细\n账号: 1234\n\n" +
				"记账日期,发生额,币别,凭证号\n" +
				"2026-08-20,-500,USD,D1\n",
			wantRows: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, errs, err := parseBankStatementCSV([]byte(tc.csv), tc.defaultCcy)
			if err != nil {
				t.Fatalf("file-level error: %v", err)
			}
			if len(rows) != tc.wantRows || len(errs) != tc.wantErrs {
				t.Fatalf("rows=%d errs=%d, want %d/%d — errs: %+v", len(rows), len(errs), tc.wantRows, tc.wantErrs, errs)
			}
			if tc.check != nil {
				tc.check(t, rows, errs)
			}
		})
	}
}

// Chinese bank portals export GBK more often than not.
func TestParseBankStatementGBK(t *testing.T) {
	utf8CSV := "交易日期,金额,币种,对方户名,流水号\n2026-08-20,-100,CNY,鞍山钢铁,G1\n"
	gbk, err := io.ReadAll(transform.NewReader(strings.NewReader(utf8CSV), simplifiedchinese.GBK.NewEncoder()))
	if err != nil {
		t.Fatal(err)
	}
	rows, errs, perr := parseBankStatementCSV(gbk, "")
	if perr != nil || len(errs) != 0 || len(rows) != 1 {
		t.Fatalf("GBK file should parse: %v %v %d", perr, errs, len(rows))
	}
	if rows[0].Counterparty != "鞍山钢铁" {
		t.Fatalf("GBK text should survive the decode, got %q", rows[0].Counterparty)
	}
}

// A file with no recognizable header must fail loudly at the file level.
func TestParseBankStatementNoHeader(t *testing.T) {
	if _, _, err := parseBankStatementCSV([]byte("a,b,c\n1,2,3\n"), ""); err == nil ||
		!strings.Contains(err.Error(), "BANK_CSV_HEADER") {
		t.Fatalf("headerless file must be refused, got %v", err)
	}
}

// BOM must not hide the first header cell.
func TestParseBankStatementBOM(t *testing.T) {
	csv := append([]byte{0xEF, 0xBB, 0xBF}, []byte("日期,金额,币种,流水号\n2026-08-20,-1,USD,E1\n")...)
	rows, _, err := parseBankStatementCSV(csv, "")
	if err != nil || len(rows) != 1 {
		t.Fatalf("BOM file should parse: %v %d", err, len(rows))
	}
	_ = bytes.MinRead
}
