package app

import (
	"strings"
	"testing"
)

// The company as it already stands: two departments, one employee, one owned
// mail domain. Everything a paste is judged against.
func company() (map[string]int64, map[string]bool, map[string]bool, []string) {
	byDept := map[string]int64{"销售部": 10, "sales": 10, "总部": 1, "hq": 1}
	usedCode := map[string]bool{"e001": true}
	usedEmail := map[string]bool{"laowang@aaaindustryinc.com": true}
	return byDept, usedCode, usedEmail, []string{"aaaindustryinc.com"}
}

func check(t *testing.T, rows []ImportRow) ImportResult {
	t.Helper()
	byDept, usedCode, usedEmail, domains := company()
	out, _ := validateImport(rows, byDept, usedCode, usedEmail, domains)
	return out
}

func row(code, name, dept, email string) ImportRow {
	return ImportRow{Code: code, Name: name, Department: dept, Email: email}
}

func TestACleanBlockPassesWhole(t *testing.T) {
	out := check(t, []ImportRow{
		row("E100", "张三", "销售部", "zhangsan@aaaindustryinc.com"),
		row("E101", "李四", "总部", "lisi@aaaindustryinc.com"),
	})
	if out.Ready != 2 || out.Blocked != 0 {
		t.Fatalf("ready=%d blocked=%d, want 2/0: %+v", out.Ready, out.Blocked, out.Verdicts)
	}
}

// Every refusal names the line that caused it and says something the person
// can act on. These messages are the product: they are what somebody reads
// when 80 rows come back rejected, and "invalid row" would send them looking
// through a spreadsheet by hand.
func TestEveryRefusalSaysWhichRowAndWhy(t *testing.T) {
	cases := []struct {
		name string
		rows []ImportRow
		want string
	}{
		{"没工号", []ImportRow{row("", "张三", "销售部", "")}, "工号必填"},
		{"没姓名", []ImportRow{row("E100", "", "销售部", "")}, "姓名必填"},
		{"没部门", []ImportRow{row("E100", "张三", "", "")}, "部门必填"},
		{"部门不存在", []ImportRow{row("E100", "张三", "研发部", "")}, "部门「研发部」不存在"},
		{"工号系统里有了", []ImportRow{row("E001", "张三", "销售部", "")}, "工号「E001」系统里已经有了"},
		{"邮箱被占用", []ImportRow{row("E100", "张三", "销售部", "laowang@aaaindustryinc.com")}, "已经被占用"},
		{"邮箱格式不对", []ImportRow{row("E100", "张三", "销售部", "zhangsan.aaaindustryinc.com")}, "格式不对"},
		{"外部域名", []ImportRow{row("E100", "张三", "销售部", "zhangsan@gmail.com")}, "不是本公司域名"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := check(t, c.rows)
			if out.Blocked != 1 {
				t.Fatalf("blocked=%d, want 1", out.Blocked)
			}
			if !strings.Contains(out.Verdicts[0].Reason, c.want) {
				t.Fatalf("reason %q does not contain %q", out.Verdicts[0].Reason, c.want)
			}
			if out.Verdicts[0].Line != 1 {
				t.Fatalf("line is %d, want 1", out.Verdicts[0].Line)
			}
		})
	}
}

// A collision inside the paste names the other line. "Duplicate" alone sends
// somebody scanning eighty rows for its twin; "和第 3 行重复" does not.
func TestADuplicateInsideTheBatchNamesTheOtherLine(t *testing.T) {
	out := check(t, []ImportRow{
		row("E100", "张三", "销售部", "a@aaaindustryinc.com"),
		row("E101", "李四", "销售部", "b@aaaindustryinc.com"),
		row("E100", "王五", "销售部", "c@aaaindustryinc.com"),
	})
	if out.Blocked != 1 {
		t.Fatalf("blocked=%d, want 1", out.Blocked)
	}
	if got := out.Verdicts[2].Reason; !strings.Contains(got, "第 1 行") {
		t.Fatalf("reason %q does not point at the first claimant", got)
	}
}

func TestADuplicateAddressInsideTheBatchAlsoNamesTheOtherLine(t *testing.T) {
	out := check(t, []ImportRow{
		row("E100", "张三", "销售部", "same@aaaindustryinc.com"),
		row("E101", "李四", "销售部", "SAME@aaaindustryinc.com"),
	})
	if out.Blocked != 1 {
		t.Fatalf("blocked=%d, want 1: %+v", out.Blocked, out.Verdicts)
	}
	if got := out.Verdicts[1].Reason; !strings.Contains(got, "第 1 行") {
		t.Fatalf("reason %q does not point at the first claimant", got)
	}
}

// An employee with no company mailbox is normal — a warehouse hand who never
// opens this system. Requiring one would either block the import or invite
// somebody to invent addresses, and an invented address is exactly what the
// whole activation design exists to refuse.
func TestSomebodyWithNoMailboxImportsFine(t *testing.T) {
	out := check(t, []ImportRow{row("E100", "仓库老陈", "总部", "")})
	if out.Ready != 1 {
		t.Fatalf("an employee with no address was refused: %+v", out.Verdicts)
	}
}

// The 部门 column of a real spreadsheet holds whichever of name or code the
// person who made it had to hand. Refusing one of them teaches nothing.
func TestADepartmentMatchesByNameOrByCode(t *testing.T) {
	out := check(t, []ImportRow{
		row("E100", "张三", "销售部", ""),
		row("E101", "李四", "SALES", ""),
	})
	if out.Ready != 2 {
		t.Fatalf("ready=%d, want 2: %+v", out.Ready, out.Verdicts)
	}
}

// Clipboard content carries trailing spaces, non-breaking spaces from web
// pages, and addresses in whatever case somebody typed. None of that is a
// mistake the person can see, so none of it may be a refusal.
func TestWhatAClipboardAddsIsCleanedRatherThanRefused(t *testing.T) {
	out := check(t, []ImportRow{{
		Code: "  E100 ", Name: " 张三", Department: "销售部 ",
		Email: " ZhangSan@AAAindustryinc.com ",
	}})
	if out.Ready != 1 {
		t.Fatalf("a row was refused over whitespace and case: %+v", out.Verdicts)
	}
}

// Org charts are written top-down and pasted in whatever order, so a manager
// may appear below their own reports. Resolving the reporting line only after
// every code in the batch is known is what makes that work.
func TestAManagerMayAppearBelowTheirOwnReports(t *testing.T) {
	out := check(t, []ImportRow{
		{Code: "E100", Name: "张三", Department: "销售部", ManagerCode: "E200"},
		{Code: "E200", Name: "销售总监", Department: "销售部"},
	})
	if out.Ready != 2 {
		t.Fatalf("ready=%d, want 2: %+v", out.Ready, out.Verdicts)
	}
}

func TestAManagerWhoExistsNowhereIsRefused(t *testing.T) {
	out := check(t, []ImportRow{
		{Code: "E100", Name: "张三", Department: "销售部", ManagerCode: "E999"},
	})
	if out.Blocked != 1 || !strings.Contains(out.Verdicts[0].Reason, "E999") {
		t.Fatalf("a dangling manager was accepted: %+v", out.Verdicts)
	}
}

// Somebody already in the company can be a manager for imported rows without
// appearing in the paste.
func TestAnExistingEmployeeCanBeTheManager(t *testing.T) {
	out := check(t, []ImportRow{
		{Code: "E100", Name: "张三", Department: "销售部", ManagerCode: "E001"},
	})
	if out.Ready != 1 {
		t.Fatalf("an existing manager was refused: %+v", out.Verdicts)
	}
}

// One bad row stops the whole batch. A half-applied import is the worst
// outcome available: the person cannot tell which rows landed, and their
// instinct — paste it again — collides with the half that did.
func TestOneBadRowBlocksTheWholeBatch(t *testing.T) {
	out := check(t, []ImportRow{
		row("E100", "张三", "销售部", ""),
		row("E101", "李四", "不存在的部门", ""),
		row("E102", "王五", "销售部", ""),
	})
	if out.Ready != 2 || out.Blocked != 1 {
		t.Fatalf("ready=%d blocked=%d, want 2/1", out.Ready, out.Blocked)
	}
	// Ready counts what *would* import; Imported is what did, and the service
	// leaves it at zero whenever anything is blocked.
	if out.Imported != 0 {
		t.Fatalf("imported=%d before anything was written", out.Imported)
	}
}

// A shape check, not a validity check. Whether an address receives mail is
// settled by sending one — that is what the whole activation design rests on.
// The only job here is the typing accidents somebody can fix by looking.
func TestTheAddressCheckCatchesTypingAccidentsAndNothingElse(t *testing.T) {
	bad := []string{"", "zhangsan", "@company.com", "zhangsan@", "a b@company.com",
		"a@company", "a@b,c.com"}
	for _, e := range bad {
		if plausibleAddress(e) {
			t.Fatalf("%q passed the shape check", e)
		}
	}
	good := []string{"a@b.com", "zhang.san+alias@sub.company.com.cn", "a_b-c@company.co"}
	for _, e := range good {
		if !plausibleAddress(e) {
			t.Fatalf("%q was refused by the shape check", e)
		}
	}
}
