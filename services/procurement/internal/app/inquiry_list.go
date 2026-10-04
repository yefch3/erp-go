package app

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/sgao19/erp-go/pkg/livefeed"
)

// inquiryRow is the sourcing_cases slice an inquiry body is assembled from.
type inquiryRow struct {
	id                        int64
	body                      []byte
	templateID                int64
	customerID, contactID     int64
	customerName, contactName string
}

// inquiryBody assembles what the workspace shows as an inquiry: the saved
// body, or a translation of the pre-workspace sourcing case when there is
// none, with the current customer and contact and the template applied.
// Shared by the detail read and the list so the two cannot drift apart.
func (s *Service) inquiryBody(ctx context.Context, tenant int64, r inquiryRow, templates map[int64]InquiryTemplateView) (InquiryBody, error) {
	var body InquiryBody
	if len(r.body) > 0 {
		if err := json.Unmarshal(r.body, &body); err != nil {
			return body, err
		}
	} else {
		old, err := s.GetSourcingCase(ctx, tenant, r.id)
		if err != nil {
			return body, err
		}
		if old.Head.SourceFileKey != "" {
			body.Attachments = []InquiryAttachment{{Key: old.Head.SourceFileKey, Name: old.Head.SourceFileName}}
		}
		body.Customer = old.Head.CustomerName
		body.CustomerID = strconv.FormatInt(old.Head.CustomerID, 10)
		body.Contact = old.Head.ContactName
		body.ContactID = strconv.FormatInt(old.Head.ContactID, 10)
		if len(old.Lines) > 0 {
			body.DestinationPort = old.Lines[0].Port
			body.Delivery = old.Lines[0].Delivery
			body.Incoterm = old.Lines[0].Incoterm
		}
		for _, l := range old.Lines {
			values := map[string]string{}
			_ = json.Unmarshal(l.CustomFields, &values)
			for key, value := range map[string]string{"material_standard": l.MaterialStandard, "grade": l.Grade, "thickness": l.Thickness, "width": l.Width, "length_or_form": l.LengthOrForm, "surface_requirement": l.SurfaceRequirement, "coating": l.Coating, "tolerance": l.Tolerance, "coil_weight": l.CoilWeight, "coil_id": l.CoilID, "payment_terms": l.PaymentTerms, "incoterm": l.Incoterm, "port": l.Port} {
				if value != "" {
					values[key] = value
				}
			}
			body.Products = append(body.Products, InquiryProduct{ID: strconv.FormatInt(l.ID, 10), Product: l.Product, Specification: strings.Join(compactStrings([]string{l.MaterialStandard, l.Grade, l.Thickness, l.Width, l.LengthOrForm, l.SurfaceRequirement}), " · "), Quantity: l.Quantity, Unit: l.QuantityUnit, Delivery: l.Delivery, Packaging: l.Packaging, Remark: l.Remarks, CustomFields: values})
		}
	}
	if r.customerID > 0 {
		body.CustomerID = strconv.FormatInt(r.customerID, 10)
		body.Customer = r.customerName
	}
	if r.contactID > 0 {
		body.ContactID = strconv.FormatInt(r.contactID, 10)
		body.Contact = r.contactName
	}
	if body.Template == nil && r.templateID > 0 {
		body.Template = &InquiryTemplateSnapshot{ID: strconv.FormatInt(r.templateID, 10)}
	}
	if err := s.prepareInquiryTemplateCached(ctx, tenant, &body, templates); err != nil {
		return body, err
	}
	if body.Products == nil {
		body.Products = []InquiryProduct{}
	}
	if body.Attachments == nil {
		body.Attachments = []InquiryAttachment{}
	}
	for i := range body.Products {
		if body.Products[i].CustomFields == nil {
			body.Products[i].CustomFields = map[string]string{}
		}
	}
	return body, nil
}

// inquiryTemplateView reads a template, or the tenant default for id 0,
// through the request's cache when there is one.
func (s *Service) inquiryTemplateView(ctx context.Context, tenant, id int64, cache map[int64]InquiryTemplateView) (InquiryTemplateView, error) {
	if view, ok := cache[id]; ok {
		return view, nil
	}
	var view InquiryTemplateView
	var err error
	if id <= 0 {
		view, err = s.GetDefaultInquiryTemplate(ctx, tenant)
	} else {
		view, err = s.GetInquiryTemplate(ctx, tenant, id)
	}
	if err == nil && cache != nil {
		cache[id] = view
	}
	return view, err
}

// inquiryPageRow is one list row while its page is being assembled.
type inquiryPageRow struct {
	view InquiryView
	row  inquiryRow
}

// inquiryCandidate is what deciding a row's place in the list needs: enough
// to know its state, nothing that costs a read of the body.
type inquiryCandidate struct {
	id                     int64
	status, handoff        string
	procurement, logistics int
}

// listInquiryWorkspace answers one page of the inquiry list.
//
// It used to read every matching inquiry in full — body, template, quotes,
// history, a customer lookup and up to four permission checks each — and
// only then count and page in Go. For fifty inquiries that is a few hundred
// internal calls per list, and every open inquiry page asked for the list
// every five seconds: under load the calls queued behind each other and one
// list went from 0.3 s to 3 s (load test 2026-10-03).
//
// Now one query decides which rows exist and what state each is in, the
// page is cut from that, and only the rows on the page are read in full.
// Facts that belong to the operator or to a customer are fetched once.
func (s *Service) listInquiryWorkspace(ctx context.Context, tenant int64, op Operator, in InquiryCommand) (InquiryResult, error) {
	candidates, err := s.inquiryListCandidates(ctx, tenant, op, in)
	if err != nil {
		return InquiryResult{}, err
	}
	if in.Page < 1 {
		in.Page = 1
	}
	if in.Size != 20 && in.Size != 50 && in.Size != 100 {
		in.Size = 20
	}
	out := InquiryResult{Items: []InquiryView{}}
	page := []int64{}
	for _, c := range candidates {
		state, listed := inquiryListState(in.View, c)
		if !listed || (in.State != "" && in.State != state) {
			continue
		}
		out.Total++
		if out.Total > (in.Page-1)*in.Size && len(page) < in.Size {
			page = append(page, c.id)
		}
	}
	out.Items, err = s.readInquiryPage(ctx, tenant, op, in.View, page)
	return out, err
}

// inquiryListCandidates is every row the operator may see in this view, in
// list order. Quote counts are read only where the view filters on them.
//
// The visibility filters here are the whole of what AuthorizeSourcingCase
// would check for a row: the owner range is the same Visibility, and the
// INTAKE_PENDING rule ("needs sales:inquiry:read") is the permission the
// SALES and QUOTATIONS views were already admitted on. So the list no longer
// re-authorizes row by row.
func (s *Service) inquiryListCandidates(ctx context.Context, tenant int64, op Operator, in InquiryCommand) ([]inquiryCandidate, error) {
	visible := Visibility{All: true}
	var err error
	if in.View == "SALES" || in.View == "QUOTATIONS" {
		visible, err = s.visibleSourcingTo(ctx, op)
		if err != nil {
			return nil, err
		}
	}
	customerAll := true
	var customerIDs []int64
	if (in.View == "SALES" || in.View == "QUOTATIONS") && s.customers != nil {
		customerIDs, customerAll, err = s.customers.VisibleIDs(ctx, op.ID)
		if err != nil {
			return nil, err
		}
	}
	countsQuotes := in.View == "PROCUREMENT" || in.View == "LOGISTICS"
	rows, err := s.pool.Query(ctx, `SELECT c.id,c.status,c.handoff_status,
		CASE WHEN $8 THEN `+inquiryQuoteCount("c", "PROCUREMENT")+` ELSE 0 END,
		CASE WHEN $8 THEN `+inquiryQuoteCount("c", "LOGISTICS")+` ELSE 0 END
		FROM sourcing_cases c
		WHERE c.tenant_id=$1 AND ($6 OR c.customer_id=0 OR c.customer_id=ANY($7::bigint[])) AND c.status<>'CANCELLED' AND c.deleted_at IS NULL
		AND ($5 OR (c.status<>'INTAKE_PENDING' AND c.handoff_status<>'SALES_WITHDRAWN')) AND ($2 OR c.owner_id=ANY($3::bigint[]))
		AND ($4='' OR c.case_no ILIKE '%'||$4||'%' OR c.display_inquiry_no ILIKE '%'||$4||'%' OR c.customer_name ILIKE '%'||$4||'%' OR c.inquiry_body::text ILIKE '%'||$4||'%')
		ORDER BY c.updated_at DESC,c.id DESC`,
		tenant, visible.All, visible.EmployeeIDs, strings.TrimSpace(in.Keyword), in.View == "SALES", customerAll, customerIDs, countsQuotes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := []inquiryCandidate{}
	for rows.Next() {
		var c inquiryCandidate
		if err = rows.Scan(&c.id, &c.status, &c.handoff, &c.procurement, &c.logistics); err != nil {
			return nil, err
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// inquiryQuoteCount is the number of submitted quotes of one kind on the
// inquiry's current revision — what readInquiry counts as it reads them.
func inquiryQuoteCount(alias, kind string) string {
	return `(SELECT count(*) FROM inquiry_quotes q WHERE q.tenant_id=` + alias + `.tenant_id AND q.case_id=` + alias + `.id AND q.inquiry_revision=` + alias + `.inquiry_revision AND q.submitted_at IS NOT NULL AND q.kind='` + kind + `')`
}

// inquiryListState is the state a view filters on. The bool is false for a
// row the view does not list at all.
func inquiryListState(view string, c inquiryCandidate) (string, bool) {
	state := inquiryState(c.status, c.handoff)
	switch view {
	case "QUOTATIONS":
		return state, state == "INQUIRING"
	case "PROCUREMENT":
		if c.procurement > 0 {
			return "QUOTED", true
		}
		return "WAITING", true
	case "LOGISTICS":
		if c.logistics > 0 {
			return "QUOTED", true
		}
		return "WAITING", true
	}
	return state, true
}

// readInquiryPage reads the rows on one list page in full, in page order.
// It produces what readInquiry would for each row, minus the quotes the list
// never shows, with the shared lookups done once for the whole page.
func (s *Service) readInquiryPage(ctx context.Context, tenant int64, op Operator, view string, page []int64) ([]InquiryView, error) {
	items := []InquiryView{}
	byID, err := s.loadInquiryPage(ctx, tenant, view, page)
	if err != nil || len(byID) == 0 {
		return items, err
	}
	if (view == "SALES" || view == "QUOTATIONS") && s.customers != nil {
		if err = s.refreshInquiryCustomers(ctx, tenant, page, byID); err != nil {
			return nil, err
		}
	}
	canEdit := view == "SALES" && s.inquiryAllowed(ctx, op, view, true) == nil
	canDelete := view == "SALES" && s.inquiryDeleteAllowed(ctx, op) == nil
	confirmed := map[int64]bool{}
	if canEdit {
		if confirmed, err = s.customerConfirmedInquiries(ctx, tenant, page); err != nil {
			return nil, err
		}
	}
	templates := map[int64]InquiryTemplateView{}
	for _, id := range page {
		l, ok := byID[id]
		if !ok {
			continue // gone from this view between the two reads
		}
		v := l.view
		v.CanEdit, v.CanDelete = canEdit, canDelete
		if view == "SALES" && !canEdit {
			v.ReadOnlyReason = "当前账号没有询盘编辑权限"
		}
		if canEdit && v.State == "INQUIRING" {
			v.CanWithdraw = !confirmed[id]
			if confirmed[id] {
				v.WithdrawReason = "客户已确认，不能直接撤回，请走后续业务变更流程"
			}
		}
		v.Legacy = len(l.row.body) == 0
		if v.Body, err = s.inquiryBody(ctx, tenant, l.row, templates); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, nil
}

// loadInquiryPage reads the page rows as they are now. Between the list
// query and this one an inquiry can be withdrawn or deleted; outside SALES a
// row that is no longer INQUIRING is dropped here, as readInquiry would
// refuse it, so buyers and forwarders never see a withdrawn inquiry.
func (s *Service) loadInquiryPage(ctx context.Context, tenant int64, view string, ids []int64) (map[int64]*inquiryPageRow, error) {
	byID := map[int64]*inquiryPageRow{}
	if len(ids) == 0 {
		return byID, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT c.id,c.id::text,c.case_no,c.display_inquiry_no,c.owner_id,c.owner_name,c.status,c.handoff_status,c.inquiry_revision,coalesce(c.inquiry_submitted_at::text,''),c.inquiry_body,c.source_mail_id,c.inquiry_template_id,c.customer_id,c.customer_name,c.contact_id,c.contact_name,
		`+inquiryQuoteCount("c", "PROCUREMENT")+`,`+inquiryQuoteCount("c", "LOGISTICS")+`
		FROM sourcing_cases c WHERE c.tenant_id=$1 AND c.id=ANY($2::bigint[]) AND c.status<>'CANCELLED' AND c.deleted_at IS NULL`, tenant, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var l inquiryPageRow
		var owner, source int64
		var status, handoff string
		if err = rows.Scan(&l.row.id, &l.view.ID, &l.view.Number, &l.view.DisplayInquiryNo, &owner, &l.view.Owner, &status, &handoff, &l.view.Revision, &l.view.SubmittedAt, &l.row.body, &source, &l.row.templateID, &l.row.customerID, &l.row.customerName, &l.row.contactID, &l.row.contactName, &l.view.ProcurementCount, &l.view.LogisticsCount); err != nil {
			return nil, err
		}
		l.view.OwnerID = strconv.FormatInt(owner, 10)
		l.view.SourceMailID = strconv.FormatInt(source, 10)
		l.view.State = inquiryState(status, handoff)
		if view != "SALES" && l.view.State != "INQUIRING" {
			continue
		}
		byID[l.row.id] = &l
	}
	return byID, rows.Err()
}

// refreshInquiryCustomers gives each row the customer's current short name,
// as the detail view does, with one masterdata call for the whole page.
// Legacy rows that saved only a name are linked to the customer first.
func (s *Service) refreshInquiryCustomers(ctx context.Context, tenant int64, page []int64, byID map[int64]*inquiryPageRow) error {
	ids := []int64{}
	for _, id := range page {
		l, ok := byID[id]
		if !ok {
			continue
		}
		if l.row.customerID == 0 && strings.TrimSpace(l.row.customerName) != "" {
			id, name, err := s.linkInquiryCustomerByName(ctx, tenant, l.row.id, l.row.customerName)
			if err != nil {
				return err
			}
			if id > 0 {
				l.row.customerID, l.row.customerName = id, name
			}
		}
		if l.row.customerID > 0 {
			ids = append(ids, l.row.customerID)
		}
	}
	names, err := s.currentCustomerNames(ctx, ids)
	if err != nil {
		return err
	}
	for _, id := range page {
		if l, ok := byID[id]; ok && l.row.customerID > 0 {
			if name := strings.TrimSpace(names[l.row.customerID]); name != "" {
				// 列表和详情使用当前客户主档简称。数据库中的历史快照不改写。
				l.row.customerName = name
			}
		}
	}
	return nil
}

// customerConfirmedInquiries says which of these inquiries a customer has
// already confirmed, which is what stops a salesperson withdrawing them.
func (s *Service) customerConfirmedInquiries(ctx context.Context, tenant int64, ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT case_id FROM sourcing_customer_selections WHERE tenant_id=$1 AND case_id=ANY($2::bigint[]) AND status='CUSTOMER_CONFIRMED'`, tenant, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// inquiryChanged tells every open inquiry page in the tenant to re-read. The
// pages used to poll the whole list every five seconds whether anything had
// changed or not; now they wait for this.
//
// No subject: the hint reaches everyone in the tenant, including people who
// may not see the inquiry, and "inquiry 1234 changed just now" is itself
// information. The pages re-read their own permission-checked view anyway.
var inquiryChanged = livefeed.Event{Type: livefeed.InquiryChanged}

// nudgeInquiry is for changes that move inquiries without touching purchase
// requirements. Writes that move both go through nudge(…, inquiryChanged).
// Always after the commit: a hint about a change that then rolled back would
// send browsers to read something that never happened.
func (s *Service) nudgeInquiry(ctx context.Context, tenant int64) {
	if s.live == nil {
		return
	}
	s.live.ToTenant(ctx, tenant, inquiryChanged)
}
