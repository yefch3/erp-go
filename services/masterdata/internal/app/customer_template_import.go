package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

type templateCustomer struct {
	existing bool
	id       int64
	basic    CustomerImportRow
	contacts []templateContact
}
type templateContact struct {
	id                         int64
	name, email, phone, mobile string
}
type templatePlan struct {
	group *templateCustomer
	match int64
	skip  bool
}

func contactFromImport(r CustomerImportRow) templateContact {
	return templateContact{name: strings.TrimSpace(r.ContactName), email: strings.TrimSpace(r.ContactEmail), phone: strings.TrimSpace(r.ContactPhone), mobile: strings.TrimSpace(r.ContactMobile)}
}
func contactPhoneKey(v string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, v)
}
func sameImportContact(a, b templateContact) bool {
	if a.email != "" && b.email != "" && strings.EqualFold(a.email, b.email) {
		return true
	}
	if !strings.EqualFold(a.name, b.name) {
		return false
	}
	for _, x := range []string{a.phone, a.mobile} {
		for _, y := range []string{b.phone, b.mobile} {
			if contactPhoneKey(x) != "" && contactPhoneKey(x) == contactPhoneKey(y) {
				return true
			}
		}
	}
	return a.email == "" && a.phone == "" && a.mobile == "" && b.email == "" && b.phone == "" && b.mobile == ""
}
func mergeImportContact(a, b templateContact) templateContact {
	if b.name != "" {
		a.name = b.name
	}
	if b.email != "" {
		a.email = b.email
	}
	if b.phone != "" {
		a.phone = b.phone
	}
	if b.mobile != "" {
		a.mobile = b.mobile
	}
	return a
}
func importReason(err error) string {
	var e *apierr.Error
	if errors.As(err, &e) {
		return e.Msg
	}
	return err.Error()
}

// One transaction validates the whole batch and then writes it. A tenant lock
// serializes concurrent imports; customer rows lock existing records. A failed
// preview/commit never partially imports customers or contacts.
func (s *Service) ImportCustomerTemplate(ctx context.Context, tenantID int64, rows []CustomerImportRow, mappings []CustomerImportFieldMapping, dryRun bool, operatorID int64, operatorName string) ([]CustomerImportVerdict, int32, error) {
	if len(rows) == 0 || len(rows) > 5000 {
		return nil, 0, apierr.Invalid("MD_IMPORT_SIZE", "一次导入 1 至 5000 行")
	}
	if err := s.validateCustomerImportMappings(ctx, tenantID, mappings); err != nil {
		return nil, 0, err
	}
	verdicts := make([]CustomerImportVerdict, len(rows))
	var imported int32
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(72601,$1::int)`, tenantID); err != nil {
			return err
		}
		groups := map[string]*templateCustomer{}
		plans := make([]templatePlan, len(rows))
		blocked := false
		for i := range rows {
			r := &rows[i]
			rv := reflect.ValueOf(r).Elem()
			for j := 0; j < rv.NumField(); j++ {
				if rv.Field(j).Kind() == reflect.String {
					rv.Field(j).SetString(strings.TrimSpace(rv.Field(j).String()))
				}
			}
			r.Code = strings.ToUpper(r.Code)
			r.CreditGrade = strings.ToUpper(r.CreditGrade)
			r.CountryCode = strings.ToUpper(r.CountryCode)
			v := CustomerImportVerdict{Line: r.SourceLine, Code: r.Code, Name: r.Name, OK: true}
			if v.Line < 2 {
				v.Line = int32(i + 2)
			}
			fail := func(reason string) { v.OK = false; v.Reason = reason; blocked = true }
			in := CustomerInput{Name: r.Name, Currency: r.Currency, Website: r.Website, Timezone: r.Timezone, BusinessStatus: r.BusinessStatus, CreditStatus: "NORMAL"}
			in.normalizeProfile()
			if err := in.validate(); err != nil {
				fail(importReason(err))
			}
			if err := validateCustomerBasic(*r); err != nil {
				fail(importReason(err))
			}
			key := r.Code
			if key == "" {
				key = "name:" + strings.ToLower(r.Name)
			}
			g := groups[key]
			if g == nil && v.OK {
				g = &templateCustomer{basic: *r}
				groups[key] = g
				var id int64
				err := tx.QueryRow(ctx, `SELECT id FROM customers WHERE tenant_id=$1 AND (($2<>'' AND upper(code)=$2) OR ($2='' AND lower(name)=lower($3))) ORDER BY id LIMIT 1`, tenantID, r.Code, r.Name).Scan(&id)
				if errors.Is(err, pgx.ErrNoRows) && r.Code != "" {
					var exists bool
					if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM customers WHERE tenant_id=$1 AND lower(name)=lower($2))`, tenantID, r.Name).Scan(&exists); e != nil {
						return e
					}
					if exists {
						fail("客户名称已使用，请核对客户代码后导入")
					}
				}
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return err
				}
				if id > 0 {
					if err := lockCustomerForImport(ctx, tx, tenantID, id, customerAccessEmployee(ctx)); err != nil {
						fail("客户编码或名称不可用，请检查资料")
						delete(groups, key)
						g = nil
					} else if r.Code == "" {
						fail("客户名称已存在，请填写该客户代码后再选择更新或保留")
						delete(groups, key)
						g = nil
					} else {
						g.id = id
						g.existing = true
						rs, err := tx.Query(ctx, `SELECT id,name,email,phone,mobile FROM customer_contacts WHERE tenant_id=$1 AND customer_id=$2 AND status='ACTIVE' ORDER BY id FOR UPDATE`, tenantID, id)
						if err != nil {
							return err
						}
						for rs.Next() {
							var c templateContact
							if err := rs.Scan(&c.id, &c.name, &c.email, &c.phone, &c.mobile); err != nil {
								rs.Close()
								return err
							}
							g.contacts = append(g.contacts, c)
						}
						err = rs.Err()
						rs.Close()
						if err != nil {
							return err
						}
					}
				}
			}
			if g != nil && v.OK {
				v.ExistingCustomer = g.id > 0
				if g.id > 0 && r.CustomerAction != g.basic.CustomerAction {
					fail("同一客户各行请选择相同的客户资料处理方式")
				}
				if g.basic.Name != r.Name {
					fail("同一客户代码的客户名称不一致")
				}
				// Repeated rows may leave company cells blank, but conflicting nonblank
				// company fields must be fixed instead of silently using the final row.
				for _, field := range []string{"ShortName", "CountryRegion", "CountryCode", "CustomerType", "CreditGrade", "Source", "ArchiveCreator", "CompanyPhone", "FaxNumber", "CompanyEmail", "Address", "PostalCode"} {
					a := reflect.ValueOf(&g.basic).Elem().FieldByName(field)
					b := reflect.ValueOf(*r).FieldByName(field).String()
					if a.String() != "" && b != "" && a.String() != b {
						fail("同一客户多行的公司资料不一致，请统一后导入")
					}
					if a.String() == "" {
						a.SetString(b)
					}
				}
				if g.id > 0 && r.CustomerAction != "UPDATE" && r.CustomerAction != "KEEP" {
					fail("客户已存在，请选择更新非空资料或保留原资料")
				}
				c := contactFromImport(*r)
				if c.name != "" || c.email != "" || c.phone != "" || c.mobile != "" {
					in := CustomerContactInput{Name: c.name, Email: c.email, Phone: c.phone, Mobile: c.mobile}
					if err := in.normalizeAndValidate(); err != nil {
						fail(importReason(err))
					}
					var matches []int
					for n, old := range g.contacts {
						if sameImportContact(old, c) {
							matches = append(matches, n)
						}
					}
					v.DuplicateContact = len(matches) > 0
					if v.DuplicateContact && r.ContactAction != "ADD" && r.ContactAction != "UPDATE" && r.ContactAction != "SKIP" {
						fail("联系人重复，请选择更新、另增一人或跳过")
					}
					if r.ContactAction == "UPDATE" && len(matches) != 1 {
						fail("更新联系人需要唯一匹配，请检查邮箱或姓名和电话")
					}
					if r.ContactAction == "SKIP" {
						plans[i].skip = true
					} else if len(matches) == 1 && r.ContactAction == "UPDATE" {
						plans[i].match = g.contacts[matches[0]].id
						g.contacts[matches[0]] = mergeImportContact(g.contacts[matches[0]], c)
					} else {
						c.id = -int64(i + 1)
						g.contacts = append(g.contacts, c)
					}
				} else {
					plans[i].skip = true
				}
				if len(r.OwnerEmployeeIDs) == 0 && r.OwnerEmployeeID > 0 {
					r.OwnerEmployeeIDs = []int64{r.OwnerEmployeeID}
					r.OwnerNames = []string{r.OwnerName}
				}
				if r.OwnerName != "" && len(r.OwnerEmployeeIDs) == 0 && strings.EqualFold(r.OwnerName, operatorName) {
					r.OwnerEmployeeIDs = []int64{operatorID}
					r.OwnerNames = []string{operatorName}
				}
				if (r.OwnerName != "" && len(r.OwnerEmployeeIDs) == 0) || len(r.OwnerEmployeeIDs) != len(r.OwnerNames) {
					fail("分管人必须匹配本公司的唯一在职员工，多人请用分号分隔")
				}
				for n, id := range r.OwnerEmployeeIDs {
					if id <= 0 || n >= len(r.OwnerNames) || strings.TrimSpace(r.OwnerNames[n]) == "" {
						fail("分管人资料不正确")
					}
				}
				plans[i].group = g
			}
			verdicts[i] = v
		}
		if dryRun || blocked {
			return nil
		}
		q := s.q.WithTx(tx)
		fields, err := resolveImportFields(ctx, q, tenantID, operatorID, mappings)
		if err != nil {
			return err
		}
		written := map[*templateCustomer]bool{}
		contactIDs := map[int64]int64{}
		for i, r := range rows {
			p := plans[i]
			g := p.group
			isNew := g.id == 0
			if isNew {
				code := r.Code
				if code == "" {
					code, err = s.nextNumber(ctx, q, tenantID, "CUSTOMER")
					if err != nil {
						return err
					}
				}
				c, err := createImportedCustomer(ctx, q, tenantID, operatorID, code, g.basic)
				if err != nil {
					return fmt.Errorf("第%d行: %w", verdicts[i].Line, err)
				}
				g.id = c.ID
				if err = writeCustomerBasic(ctx, tx, tenantID, g.id, operatorID, g.basic, true); err != nil {
					return err
				}
				if err = addImportOwner(ctx, tx, tenantID, g.id, operatorID, operatorName, operatorID); err != nil {
					return err
				}
			} else if r.CustomerAction == "UPDATE" && !written[g] {
				if err = writeCustomerBasic(ctx, tx, tenantID, g.id, operatorID, g.basic, true); err != nil {
					return err
				}
			}
			if isNew || r.CustomerAction == "UPDATE" {
				written[g] = true
			}
			// Excel adds owners; it never removes an existing owner implicitly.
			for n, id := range r.OwnerEmployeeIDs {
				if err = addImportOwner(ctx, tx, tenantID, g.id, id, r.OwnerNames[n], operatorID); err != nil {
					return err
				}
			}
			if !p.skip {
				c := contactFromImport(r)
				match := p.match
				if match < 0 {
					match = contactIDs[match]
				}
				if match > 0 {
					_, err = tx.Exec(ctx, `UPDATE customer_contacts SET name=coalesce(nullif($4,''),name),email=coalesce(nullif($5,''),email),phone=coalesce(nullif($6,''),phone),mobile=coalesce(nullif($7,''),mobile),updated_by=$8,updated_at=now() WHERE tenant_id=$1 AND customer_id=$2 AND id=$3 AND status='ACTIVE'`, tenantID, g.id, match, c.name, c.email, c.phone, c.mobile, operatorID)
				} else {
					var id int64
					err = tx.QueryRow(ctx, `INSERT INTO customer_contacts(tenant_id,customer_id,name,email,phone,mobile,is_primary,created_by,updated_by)
      VALUES($1,$2,$3,$4,$5,$6,NOT EXISTS(SELECT 1 FROM customer_contacts WHERE tenant_id=$1 AND customer_id=$2 AND is_primary AND status='ACTIVE'),$7,$7) RETURNING id`, tenantID, g.id, c.name, c.email, c.phone, c.mobile, operatorID).Scan(&id)
					contactIDs[-int64(i+1)] = id
				}
				if err != nil {
					return err
				}
			}
			if !g.existing || r.CustomerAction == "UPDATE" {
				for key, value := range r.CustomFields {
					if f, ok := fields[key]; ok && strings.TrimSpace(value) != "" {
						if err = q.UpsertCustomerCustomFieldValue(ctx, store.UpsertCustomerCustomFieldValueParams{TenantID: tenantID, CustomerID: g.id, FieldID: f.ID, Value: strings.TrimSpace(value), OperatorID: operatorID}); err != nil {
							return err
						}
					}
				}
			}
			if err = recordCustomerChange(ctx, q, tenantID, g.id, "IMPORT", "BASIC", "导入客户资料及联系人", nil, r, operatorID, operatorName); err != nil {
				return err
			}
			imported++
		}
		return nil
	})
	return verdicts, imported, err
}

func addImportOwner(ctx context.Context, tx pgx.Tx, tenantID, customerID, employeeID int64, name string, operatorID int64) error {
	if employeeID <= 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO customer_owners(tenant_id,customer_id,employee_id,employee_name,responsibility_code,is_primary,created_by,updated_by)
 SELECT $1,$2,$3,$4,'SALES',NOT EXISTS(SELECT 1 FROM customer_owners WHERE tenant_id=$1 AND customer_id=$2 AND status='ACTIVE' AND is_primary),$5,$5
 WHERE NOT EXISTS(SELECT 1 FROM customer_owners WHERE tenant_id=$1 AND customer_id=$2 AND employee_id=$3 AND status='ACTIVE')`, tenantID, customerID, employeeID, name, operatorID)
	return err
}
