package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/iam/internal/store"
)

// Bringing a whole company in at once.
//
// The one-at-a-time form is the right shape for hiring somebody; it is the
// wrong shape for the day a company arrives, when there are eighty people in
// a spreadsheet and every one of them needs a row before anybody can be
// invited. Eighty passes through a dialog is not a smaller version of that
// task, it is a different and much worse one.
//
// Two decisions shape everything here.
//
// The batch is all-or-nothing. A half-applied import is the worst outcome
// available: the person cannot tell which rows landed, and their instinct —
// paste it again — collides with the half that did. Refusing the whole block
// and saying exactly which lines are wrong leaves them with one obvious move.
//
// And the preview runs the identical code as the commit, differing only in
// whether the transaction is entered. A preview computed by a second, simpler
// validator is a preview that eventually lies, and it lies at the moment
// somebody has already decided to trust it.

// ImportRow is one line as the person pasted it, before anything is known
// about whether it means anything.
type ImportRow struct {
	Code        string
	Name        string
	Department  string // matched against a department's name or its code
	Position    string
	Email       string
	Phone       string
	ManagerCode string // the manager's 工号, which may be later in this batch
}

// ImportVerdict is what happened, or would happen, to one line. Line numbers
// are the pasted ones so the reason can be read next to the row that caused
// it.
type ImportVerdict struct {
	Line   int32
	Code   string
	Name   string
	OK     bool
	Reason string
}

type ImportResult struct {
	Verdicts []ImportVerdict
	Ready    int32
	Blocked  int32
	Imported int32
}

// ImportEmployees validates a pasted block and, unless this is a dry run and
// nothing is wrong with it, writes it.
func (s *Service) ImportEmployees(ctx context.Context, tenantID int64, rows []ImportRow, dryRun bool) (ImportResult, error) {
	if len(rows) == 0 {
		return ImportResult{}, apierr.Invalid("IAM_IMPORT_EMPTY", "没有可导入的数据")
	}
	// A ceiling, so a runaway paste cannot hold a transaction open across
	// thousands of inserts. Well above any real company's headcount, and the
	// message says to split rather than leaving somebody guessing.
	const maxRows = 500
	if len(rows) > maxRows {
		return ImportResult{}, apierr.Invalid("IAM_IMPORT_TOO_MANY",
			fmt.Sprintf("一次最多导入 %d 行，请分批粘贴", maxRows))
	}

	depts, err := s.q.ListDepartments(ctx, tenantID)
	if err != nil {
		return ImportResult{}, err
	}
	// Name or code, both lower-cased: a spreadsheet column headed 部门 holds
	// whichever of the two the person who made it had to hand, and refusing
	// one of them teaches nothing.
	byDept := make(map[string]int64, len(depts)*2)
	for _, d := range depts {
		byDept[strings.ToLower(d.Name)] = d.ID
		byDept[strings.ToLower(d.Code)] = d.ID
	}

	taken, err := s.q.ListEmployeeIdentity(ctx, tenantID)
	if err != nil {
		return ImportResult{}, err
	}
	usedCode := make(map[string]bool, len(taken))
	usedEmail := make(map[string]bool, len(taken))
	for _, t := range taken {
		usedCode[strings.ToLower(t.Code)] = true
		if t.Email != "" {
			usedEmail[t.Email] = true
		}
	}

	domains, err := s.q.ListTenantDomains(ctx, tenantID)
	if err != nil {
		return ImportResult{}, err
	}

	// Everything the decision needs is now in memory, and the decision itself
	// is separated from the four reads above so every refusal can be exercised
	// without a database.
	out, clean := validateImport(rows, byDept, usedCode, usedEmail, domains)

	if dryRun || out.Blocked > 0 {
		// Nothing is written when anything is wrong. See the note at the top:
		// the alternative leaves somebody unable to tell which half landed.
		return out, nil
	}

	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		ids := make(map[string]int64, len(clean))
		for _, r := range clean {
			emp, err := q.CreateEmployee(ctx, store.CreateEmployeeParams{
				TenantID: tenantID, Code: r.Code, Name: r.Name,
				DepartmentID: byDept[strings.ToLower(r.Department)],
				Position:     r.Position, Email: r.Email, Phone: r.Phone,
			})
			if err != nil {
				// Something the validation could not see — a concurrent import
				// of the same block, most likely. The transaction takes the
				// whole batch back with it.
				return translateUnique(err, "IAM_IMPORT_CONFLICT",
					fmt.Sprintf("工号「%s」写入时冲突，请重新预检", r.Code))
			}
			ids[strings.ToLower(r.Code)] = emp.ID
		}
		// Second pass, now that every row in the batch has an id.
		for _, r := range clean {
			if r.ManagerCode == "" {
				continue
			}
			managerID, inBatch := ids[strings.ToLower(r.ManagerCode)]
			if !inBatch {
				// Already in the company; look it up by code.
				existing, err := q.GetEmployeeByCode(ctx, store.GetEmployeeByCodeParams{
					TenantID: tenantID, Code: r.ManagerCode,
				})
				if err != nil {
					return err
				}
				managerID = existing.ID
			}
			if _, err := q.SetEmployeeManager(ctx, store.SetEmployeeManagerParams{
				TenantID: tenantID, ID: ids[strings.ToLower(r.Code)], ManagerID: managerID,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ImportResult{}, err
	}
	out.Imported = out.Ready
	s.log.Info("employees imported", "tenant_id", tenantID, "count", out.Imported)
	return out, nil
}

// trimRow normalises what came off a clipboard: spreadsheets carry trailing
// spaces, non-breaking spaces from web pages, and addresses in whatever case
// the person typed them.
func trimRow(r ImportRow) ImportRow {
	clean := func(s string) string {
		return strings.TrimSpace(strings.ReplaceAll(s, " ", " "))
	}
	return ImportRow{
		Code: clean(r.Code), Name: clean(r.Name), Department: clean(r.Department),
		Position: clean(r.Position), Phone: clean(r.Phone),
		Email:       strings.ToLower(clean(r.Email)),
		ManagerCode: clean(r.ManagerCode),
	}
}

// plausibleAddress is a shape check, not a validity check. Whether an address
// receives mail is settled by sending one — that is what the whole activation
// design rests on — so the only job here is to catch the typing accidents a
// person can fix by looking: no @, nothing before it, nothing after it, or a
// domain with no dot.
func plausibleAddress(email string) bool {
	at := strings.LastIndex(email, "@")
	if at < 1 || at == len(email)-1 {
		return false
	}
	if strings.ContainsAny(email, " \t,;") {
		return false
	}
	return strings.Contains(email[at+1:], ".")
}

// validateImport decides every row's fate, given what is already in the
// company. Separated from the reads that gather that context so the rules —
// which are the whole product here, because they are what somebody reads when
// their paste is refused — can be exercised without a database.
func validateImport(
	rows []ImportRow,
	byDept map[string]int64,
	usedCode, usedEmail map[string]bool,
	domains []string,
) (ImportResult, []ImportRow) {
	// Which line first claimed a code or an address, so a collision inside the
	// batch can name the other line instead of just saying "duplicate".
	batchCode := map[string]int32{}
	batchEmail := map[string]int32{}

	out := ImportResult{Verdicts: make([]ImportVerdict, len(rows))}
	clean := make([]ImportRow, len(rows))
	for i, raw := range rows {
		line := int32(i + 1)
		r := trimRow(raw)
		clean[i] = r
		v := ImportVerdict{Line: line, Code: r.Code, Name: r.Name, OK: true}

		switch {
		case r.Code == "":
			v.OK, v.Reason = false, "工号必填"
		case r.Name == "":
			v.OK, v.Reason = false, "姓名必填"
		case r.Department == "":
			v.OK, v.Reason = false, "部门必填"
		}
		if v.OK {
			if _, found := byDept[strings.ToLower(r.Department)]; !found {
				v.OK, v.Reason = false, fmt.Sprintf("部门「%s」不存在，请先建好部门", r.Department)
			}
		}
		if v.OK {
			key := strings.ToLower(r.Code)
			if first, dup := batchCode[key]; dup {
				v.OK, v.Reason = false, fmt.Sprintf("工号「%s」和第 %d 行重复", r.Code, first)
			} else if usedCode[key] {
				v.OK, v.Reason = false, fmt.Sprintf("工号「%s」系统里已经有了", r.Code)
			} else {
				batchCode[key] = line
			}
		}
		// The address is optional and stays optional. An employee with no
		// company mailbox is a normal thing — a warehouse hand who never opens
		// this system — and they simply show as 未邀请 forever, which is true.
		if v.OK && r.Email != "" {
			switch {
			case !plausibleAddress(r.Email):
				v.OK, v.Reason = false, fmt.Sprintf("邮箱「%s」格式不对", r.Email)
			case !domainAllowed(r.Email, domains):
				// Refused here rather than at invitation time, because an
				// address the company does not own can never be activated and
				// importing it only defers the discovery.
				v.OK, v.Reason = false, fmt.Sprintf("邮箱「%s」不是本公司域名（%s）",
					r.Email, strings.Join(domains, "、"))
			}
			if v.OK {
				if first, dup := batchEmail[r.Email]; dup {
					v.OK, v.Reason = false, fmt.Sprintf("邮箱「%s」和第 %d 行重复", r.Email, first)
				} else if usedEmail[r.Email] {
					v.OK, v.Reason = false, fmt.Sprintf("邮箱「%s」已经被占用了", r.Email)
				} else {
					batchEmail[r.Email] = line
				}
			}
		}
		out.Verdicts[i] = v
	}

	// The reporting line is resolved after every code in the batch is known,
	// so a manager may appear below their own reports in the spreadsheet —
	// which is how org charts are usually written down.
	for i := range clean {
		if !out.Verdicts[i].OK || clean[i].ManagerCode == "" {
			continue
		}
		key := strings.ToLower(clean[i].ManagerCode)
		if _, inBatch := batchCode[key]; !inBatch && !usedCode[key] {
			out.Verdicts[i].OK = false
			out.Verdicts[i].Reason = fmt.Sprintf("直属上级工号「%s」既不在本次导入里，系统里也没有", clean[i].ManagerCode)
		}
	}

	for _, v := range out.Verdicts {
		if v.OK {
			out.Ready++
		} else {
			out.Blocked++
		}
	}
	return out, clean
}
