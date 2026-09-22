package app

import (
	"context"
	"errors"
	"net/mail"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

type MailCustomerMatch struct {
	ID                 int64
	Code, Name, Status string
	ExactName          bool
	Contacts           []store.CustomerContact
}
type MailMatches struct{ Customers, Suppliers []MailCustomerMatch }
type mailQuery interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (s *Service) MatchMailCustomer(ctx context.Context, tenant int64, name, email string) (MailMatches, error) {
	return matchMailCustomer(ctx, s.pool, s.q, tenant, name, email)
}
func matchMailCustomer(ctx context.Context, db mailQuery, q *store.Queries, tenant int64, name, email string) (MailMatches, error) {
	name = strings.TrimSpace(name)
	email = strings.ToLower(strings.TrimSpace(email))
	out := MailMatches{}
	if name == "" && email == "" {
		return out, nil
	}
	rows, err := db.Query(ctx, `SELECT c.id,c.code,c.name,c.status,
 $2<>'' AND (lower(btrim(c.name))=lower($2) OR lower(btrim(c.short_name))=lower($2) OR lower(btrim(c.english_name))=lower($2)) AS exact_name
 FROM customers c WHERE c.tenant_id=$1
 AND ($4::bigint=0 OR EXISTS(SELECT 1 FROM customer_owners o WHERE o.tenant_id=c.tenant_id AND o.customer_id=c.id AND o.employee_id=$4 AND o.status='ACTIVE' AND (o.start_date IS NULL OR o.start_date<=CURRENT_DATE) AND (o.end_date IS NULL OR o.end_date>=CURRENT_DATE)))
 AND (($2<>'' AND (strpos(lower(c.name),lower($2))>0 OR strpos(lower(c.short_name),lower($2))>0 OR strpos(lower(c.english_name),lower($2))>0))
 OR ($3<>'' AND EXISTS(SELECT 1 FROM customer_contacts cc WHERE cc.tenant_id=c.tenant_id AND cc.customer_id=c.id AND (lower(btrim(cc.email))=$3 OR $3=ANY(cc.additional_emails)))))
 ORDER BY EXISTS(SELECT 1 FROM customer_contacts cc WHERE cc.tenant_id=c.tenant_id AND cc.customer_id=c.id AND cc.status='ACTIVE' AND (lower(btrim(cc.email))=$3 OR $3=ANY(cc.additional_emails))) DESC, exact_name DESC,c.id LIMIT 30`, tenant, name, email, customerAccessEmployee(ctx))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var m MailCustomerMatch
		if err = rows.Scan(&m.ID, &m.Code, &m.Name, &m.Status, &m.ExactName); err != nil {
			break
		}
		out.Customers = append(out.Customers, m)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return out, err
	}
	for i := range out.Customers {
		out.Customers[i].Contacts, err = q.ListCustomerContactsDetailed(ctx, store.ListCustomerContactsDetailedParams{TenantID: tenant, CustomerID: out.Customers[i].ID, Status: "ALL"})
		if err != nil {
			return out, err
		}
	}
	// -1 means the caller cannot read suppliers. Never widen customer access
	// into supplier access merely because both are shown in the same dialog.
	if supplierAccessEmployee(ctx) < 0 {
		return out, nil
	}
	rows, err = db.Query(ctx, `SELECT s.id,s.code,coalesce(nullif(s.name_zh,''),nullif(s.name_en,''),s.name),s.status,
 $2<>'' AND (lower(btrim(s.name))=lower($2) OR lower(btrim(s.name_zh))=lower($2) OR lower(btrim(s.name_en))=lower($2))
 FROM suppliers s WHERE s.tenant_id=$1
 AND ($4::bigint=0 OR EXISTS(SELECT 1 FROM supplier_owners o WHERE o.tenant_id=s.tenant_id AND o.supplier_id=s.id AND o.employee_id=$4 AND o.status='ACTIVE' AND (o.start_date IS NULL OR o.start_date<=CURRENT_DATE) AND (o.end_date IS NULL OR o.end_date>=CURRENT_DATE)))
 AND (($2<>'' AND (lower(btrim(s.name))=lower($2) OR lower(btrim(s.name_zh))=lower($2) OR lower(btrim(s.name_en))=lower($2)))
 OR ($3<>'' AND (lower(btrim(s.contact_email))=$3 OR EXISTS(SELECT 1 FROM supplier_contacts sc WHERE sc.tenant_id=s.tenant_id AND sc.supplier_id=s.id AND sc.status='ACTIVE' AND lower(btrim(sc.email))=$3))))
 ORDER BY s.id LIMIT 30`, tenant, name, email, supplierAccessEmployee(ctx))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var m MailCustomerMatch
		if err = rows.Scan(&m.ID, &m.Code, &m.Name, &m.Status, &m.ExactName); err != nil {
			return out, err
		}
		out.Suppliers = append(out.Suppliers, m)
	}
	return out, rows.Err()
}

type SaveMailCustomerInput struct {
	InboundID, CustomerID, ContactID, OperatorID                                           int64
	Action, CompanyName, ContactName, Email, Phone, Website, Address, Remark, OperatorName string
	ConfirmSameName, ConfirmSupplier                                                       bool
}
type MailCustomerLink struct {
	Customer store.Customer
	Contact  store.CustomerContact
	Email    string
}

func contactHasEmail(c store.CustomerContact, email string) bool {
	if strings.EqualFold(strings.TrimSpace(c.Email), email) {
		return true
	}
	for _, v := range c.AdditionalEmails {
		if strings.EqualFold(v, email) {
			return true
		}
	}
	return false
}
func (s *Service) GetMailCustomerLink(ctx context.Context, tenant, inbound int64) (MailCustomerLink, error) {
	var out MailCustomerLink
	var customer, contact int64
	err := s.pool.QueryRow(ctx, `SELECT customer_id,contact_id,email FROM customer_mail_links WHERE tenant_id=$1 AND inbound_id=$2`, tenant, inbound).Scan(&customer, &contact, &out.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err = s.AuthorizeCustomer(ctx, tenant, customer); err != nil {
		return out, err
	}
	out.Customer, err = s.q.GetCustomer(ctx, store.GetCustomerParams{TenantID: tenant, ID: customer})
	if err != nil {
		return out, err
	}
	out.Contact, err = s.q.GetCustomerContact(ctx, store.GetCustomerContactParams{TenantID: tenant, CustomerID: customer, ID: contact})
	return out, err
}
func (s *Service) SaveMailCustomer(ctx context.Context, tenant int64, in SaveMailCustomerInput) (MailCustomerLink, error) {
	out := MailCustomerLink{}
	in.CompanyName = strings.TrimSpace(in.CompanyName)
	in.ContactName = strings.TrimSpace(in.ContactName)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if tenant <= 0 || in.OperatorID <= 0 || in.InboundID <= 0 {
		return out, apierr.Invalid("MD_MAIL_SOURCE_REQUIRED", "请选择来源邮件")
	}
	addr, err := mail.ParseAddress(in.Email)
	if err != nil || addr.Address != in.Email {
		return out, apierr.Invalid("MD_CONTACT_EMAIL_INVALID", "请填写有效的联系人邮箱")
	}
	if in.Action != "CREATE" && in.Action != "ADD" && in.Action != "USE" && in.Action != "SUPPLEMENT" {
		return out, apierr.Invalid("MD_MAIL_ACTION_INVALID", "请选择本次处理方式")
	}
	if in.Action == "CREATE" {
		if in.CompanyName == "" || in.CustomerID != 0 || in.ContactID != 0 {
			return out, apierr.Invalid("MD_CUSTOMER_NAME_REQUIRED", "新建客户必须填写公司名称")
		}
	} else if in.CustomerID <= 0 {
		return out, apierr.Invalid("MD_CUSTOMER_REQUIRED", "请选择所属公司")
	}
	if utf8.RuneCountInString(in.CompanyName) > 200 || utf8.RuneCountInString(in.ContactName) > 100 || len(in.Email) > 200 || utf8.RuneCountInString(in.Address) > 500 || len(in.Website) > 300 || utf8.RuneCountInString(in.Remark) > 2000 || len(in.Phone) > 50 {
		return out, apierr.Invalid("MD_MAIL_INPUT_LENGTH", "资料长度超过限制")
	}
	if in.Phone != "" && !customerPhonePattern.MatchString(in.Phone) {
		return out, apierr.Invalid("MD_CONTACT_PHONE_INVALID", "电话格式不正确")
	}
	if in.Website != "" {
		u, e := url.ParseRequestURI(in.Website)
		if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
			return out, apierr.Invalid("MD_WEBSITE_INVALID", "网站地址需要以 http:// 或 https:// 开头")
		}
	}
	if in.CustomerID > 0 {
		if err = s.AuthorizeCustomer(ctx, tenant, in.CustomerID); err != nil {
			return out, err
		}
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		// Serialize this tenant's registration decisions, including new companies.
		if _, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('mail-customer:'||$1::bigint::text,0))`, tenant); e != nil {
			return e
		}
		q := s.q.WithTx(tx)
		var linkedCustomer, linkedContact int64
		var linkedEmail string
		e := tx.QueryRow(ctx, `SELECT customer_id,contact_id,email FROM customer_mail_links WHERE tenant_id=$1 AND inbound_id=$2`, tenant, in.InboundID).Scan(&linkedCustomer, &linkedContact, &linkedEmail)
		if e == nil {
			if linkedEmail != in.Email || (in.CustomerID > 0 && linkedCustomer != in.CustomerID) || (in.ContactID > 0 && linkedContact != in.ContactID) {
				return apierr.Conflict("MD_MAIL_ALREADY_LINKED", "该邮件已经关联其他资料，请刷新核对")
			}
			if e = s.AuthorizeCustomer(ctx, tenant, linkedCustomer); e != nil {
				return e
			}
			out.Customer, e = q.GetCustomer(ctx, store.GetCustomerParams{TenantID: tenant, ID: linkedCustomer})
			if e != nil {
				return e
			}
			out.Contact, e = q.GetCustomerContact(ctx, store.GetCustomerContactParams{TenantID: tenant, CustomerID: linkedCustomer, ID: linkedContact})
			out.Email = linkedEmail
			if out.Customer.Status != "ACTIVE" || out.Contact.Status != "ACTIVE" {
				return apierr.Conflict("MD_MAIL_INACTIVE", "关联资料已停用，请到基础数据核对或恢复")
			}
			return e
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		// Unowned matches must not be exposed, but must still prevent a new
		// duplicate identity. Return no customer identifiers or names.
		if in.Action != "USE" {
			var exists bool
			e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM customers c WHERE c.tenant_id=$1 AND c.status='ACTIVE' AND c.id<>$2
           AND (($4::boolean AND $5<>'' AND (lower(btrim(c.name))=lower($5) OR lower(btrim(c.short_name))=lower($5) OR lower(btrim(c.english_name))=lower($5)))
           OR EXISTS(SELECT 1 FROM customer_contacts ct WHERE ct.tenant_id=c.tenant_id AND ct.customer_id=c.id AND ct.status='ACTIVE' AND (lower(btrim(ct.email))=$3 OR $3=ANY(ct.additional_emails)))))`, tenant, in.CustomerID, in.Email, in.Action == "CREATE", in.CompanyName).Scan(&exists)
			if e != nil {
				return e
			}
			if exists {
				return apierr.Conflict("MD_MAIL_IDENTITY_EXISTS", "已有客户资料匹配，请重新查询；如无查看权限，请联系负责人核对")
			}
		}
		matches, e := matchMailCustomer(ctx, tx, q, tenant, in.CompanyName, in.Email)
		if e != nil {
			return e
		}
		if in.Action == "ADD" || in.Action == "SUPPLEMENT" {
			for _, c := range matches.Customers {
				if c.ID == in.CustomerID || c.Status != "ACTIVE" {
					continue
				}
				for _, ct := range c.Contacts {
					if ct.Status == "ACTIVE" && contactHasEmail(ct, in.Email) {
						return apierr.Conflict("MD_MAIL_OTHER_COMPANY", "该邮箱已属于其他公司，请核对并使用已有归属")
					}
				}
			}
		}
		if in.Action == "CREATE" {
			for _, c := range matches.Customers {
				if c.Status != "ACTIVE" {
					continue
				}
				if c.ExactName {
					return apierr.Conflict("MD_MAIL_COMPANY_EXISTS", "已有同名公司，请重新查询并选择公司")
				}
				for _, ct := range c.Contacts {
					if ct.Status == "ACTIVE" && contactHasEmail(ct, in.Email) {
						return apierr.Conflict("MD_MAIL_EMAIL_EXISTS", "该邮箱已有联系人，请重新查询并选择已有归属")
					}
				}
			}
			if !in.ConfirmSupplier {
				for _, v := range matches.Suppliers {
					if v.Status == "ACTIVE" {
						return apierr.Conflict("MD_MAIL_SUPPLIER_CONFIRM", "已找到供应商资料，请确认是否同时建立客户身份")
					}
				}
			}
			code, e := s.nextNumber(ctx, q, tenant, "CUSTOMER")
			if e != nil {
				return e
			}
			out.Customer, e = q.CreateCustomer(ctx, store.CreateCustomerParams{TenantID: tenant, Code: code, Name: in.CompanyName, Currency: "USD", CreditCurrency: "USD", CreditStatus: "NORMAL", BusinessStatus: "PROSPECT", Source: "EMAIL", Website: in.Website, Address: in.Address, Remark: in.Remark, OperatorID: in.OperatorID, Tags: []string{}})
			if e != nil {
				return e
			}
			in.CustomerID = out.Customer.ID
			_, e = q.CreateCustomerOwner(ctx, store.CreateCustomerOwnerParams{TenantID: tenant, CustomerID: in.CustomerID, EmployeeID: in.OperatorID, EmployeeName: in.OperatorName, ResponsibilityCode: "SALES", IsPrimary: true, OperatorID: in.OperatorID})
			if e != nil {
				return e
			}
		} else {
			// Lock the company so concurrent deactivation cannot race this save.
			var status string
			if e = tx.QueryRow(ctx, `SELECT status FROM customers WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, in.CustomerID).Scan(&status); e != nil {
				return e
			}
			if status != "ACTIVE" {
				return apierr.Conflict("MD_MAIL_INACTIVE", "该公司已停用，请重新查询")
			}
			out.Customer, e = q.GetCustomer(ctx, store.GetCustomerParams{TenantID: tenant, ID: in.CustomerID})
			if e != nil {
				return e
			}
		}
		contacts, e := q.ListCustomerContactsDetailed(ctx, store.ListCustomerContactsDetailedParams{TenantID: tenant, CustomerID: in.CustomerID, Status: "ALL"})
		if e != nil {
			return e
		}
		if in.Action == "USE" || in.Action == "SUPPLEMENT" {
			var status string
			if e = tx.QueryRow(ctx, `SELECT status FROM customer_contacts WHERE tenant_id=$1 AND customer_id=$2 AND id=$3 FOR UPDATE`, tenant, in.CustomerID, in.ContactID).Scan(&status); e != nil {
				return e
			}
			if status != "ACTIVE" {
				return apierr.Conflict("MD_MAIL_INACTIVE", "该联系人已停用，请重新查询")
			}
			out.Contact, e = q.GetCustomerContact(ctx, store.GetCustomerContactParams{TenantID: tenant, CustomerID: in.CustomerID, ID: in.ContactID})
			if e != nil {
				return e
			}
			if in.Action == "USE" && !contactHasEmail(out.Contact, in.Email) {
				return apierr.Conflict("MD_MAIL_CONTACT_CHANGED", "联系人邮箱已变化，请重新查询")
			}
			if in.Action == "SUPPLEMENT" && !contactHasEmail(out.Contact, in.Email) {
				for _, ct := range contacts {
					if ct.Status == "ACTIVE" && ct.ID != in.ContactID && contactHasEmail(ct, in.Email) {
						return apierr.Conflict("MD_MAIL_EMAIL_EXISTS", "该邮箱已在该公司的其他联系人下")
					}
				}
				_, e = tx.Exec(ctx, `UPDATE customer_contacts SET additional_emails=array_append(additional_emails,$4),updated_by=$5,updated_at=now() WHERE tenant_id=$1 AND customer_id=$2 AND id=$3`, tenant, in.CustomerID, in.ContactID, in.Email, in.OperatorID)
				if e != nil {
					return e
				}
				before := out.Contact
				out.Contact, e = q.GetCustomerContact(ctx, store.GetCustomerContactParams{TenantID: tenant, CustomerID: in.CustomerID, ID: in.ContactID})
				if e != nil {
					return e
				}
				if e = recordCustomerChange(ctx, q, tenant, in.CustomerID, "UPDATE", "CONTACT", "从邮件补充联系人邮箱", before, out.Contact, in.OperatorID, in.OperatorName); e != nil {
					return e
				}
			}
		} else {
			// A second request for this company/email reuses the contact. Same-name
			// different-email requires an explicit decision, never an automatic merge.
			for _, ct := range contacts {
				if ct.Status != "ACTIVE" {
					continue
				}
				if contactHasEmail(ct, in.Email) {
					out.Contact = ct
					break
				}
			}
			if out.Contact.ID == 0 {
				for _, ct := range contacts {
					if ct.Status == "ACTIVE" && in.ContactName != "" && sameText(ct.Name, in.ContactName) && !in.ConfirmSameName {
						return apierr.Conflict("MD_MAIL_SAME_NAME", "该公司已有同名联系人，请确认补充邮箱或另建联系人")
					}
				}
				out.Contact, e = q.CreateCustomerContact(ctx, store.CreateCustomerContactParams{TenantID: tenant, CustomerID: in.CustomerID, Name: in.ContactName, Email: in.Email, Phone: in.Phone, Remark: in.Remark, IsPrimary: in.Action == "CREATE", EmailPermission: "ALLOWED", EmailCategories: []string{}, OperatorID: in.OperatorID})
				if e != nil {
					return e
				}
				if e = recordCustomerChange(ctx, q, tenant, in.CustomerID, "CREATE", "CONTACT", "从邮件添加联系人", nil, out.Contact, in.OperatorID, in.OperatorName); e != nil {
					return e
				}
			}
		}
		out.Email = in.Email
		_, e = tx.Exec(ctx, `INSERT INTO customer_mail_links(tenant_id,inbound_id,customer_id,contact_id,email,created_by) VALUES($1,$2,$3,$4,$5,$6)`, tenant, in.InboundID, in.CustomerID, out.Contact.ID, in.Email, in.OperatorID)
		return e
	})
	return out, err
}
