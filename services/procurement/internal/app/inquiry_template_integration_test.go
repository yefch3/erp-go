package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 需要 make up 之后的本地库：go test 带上 PROCUREMENT_TEST_DSN 才会跑。
func templateTestService(t *testing.T) (*Service, context.Context, int64) {
	t.Helper()
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed inquiry template test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return New(pool, Deps{}), ctx, time.Now().UnixNano()
}

func TestInquiryTemplateSeedsSystemDefaultOnFirstUse(t *testing.T) {
	svc, ctx, tenantID := templateTestService(t)
	view, err := svc.GetDefaultInquiryTemplate(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Template.TemplateCode != SystemInquiryTemplateCode || view.Template.Version != 1 || !view.Template.IsDefault {
		t.Fatalf("unexpected seeded template: %#v", view.Template)
	}
	if len(view.Fields) != 22 {
		t.Fatalf("system layout should carry the 22 standard columns, got %d", len(view.Fields))
	}
	templates, err := svc.ListInquiryTemplates(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) != 2 {
		t.Fatalf("first use should seed default and detailed steel templates, got %d", len(templates))
	}
	var detailed *InquiryTemplateView
	for i := range templates {
		if templates[i].Template.TemplateCode == SteelDetailedInquiryTemplateCode {
			detailed = &templates[i]
		}
	}
	if detailed == nil || detailed.Template.IsDefault || len(detailed.Fields) != 26 {
		t.Fatalf("unexpected detailed steel template: %#v", detailed)
	}
}

func TestInquiryTemplateSaveIssuesNextVersionAndKeepsDefault(t *testing.T) {
	svc, ctx, tenantID := templateTestService(t)
	op := Operator{ID: 1, Name: "tester"}
	def, err := svc.GetDefaultInquiryTemplate(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	fields := make([]InquiryTemplateFieldInput, 0, len(def.Fields)+1)
	for _, field := range def.Fields {
		fields = append(fields, InquiryTemplateFieldInput{
			FieldKey: field.FieldKey, DisplayName: field.DisplayName, SortOrder: field.SortOrder,
			IsRequired: field.IsRequired, DefaultValue: field.DefaultValue, DataType: field.DataType,
		})
	}
	fields = append(fields, InquiryTemplateFieldInput{FieldKey: "custom.customer_part_no", DisplayName: "客户料号"})

	saved, err := svc.SaveInquiryTemplate(ctx, tenantID, def.Template.ID, InquiryTemplateInput{
		Name: "系统标准询盘模板", Description: "本地验收示例：包含公司客户料号", Fields: fields,
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Template.Version != def.Template.Version+1 || !saved.Template.IsDefault {
		t.Fatalf("save should issue the next version and keep default, got v%d default=%v",
			saved.Template.Version, saved.Template.IsDefault)
	}
	if len(saved.Fields) != 22 {
		t.Fatalf("custom column missing: %d fields", len(saved.Fields))
	}

	old, err := svc.GetInquiryTemplate(ctx, tenantID, def.Template.ID)
	if err != nil {
		t.Fatal(err)
	}
	if old.Template.Status != InquiryTemplateStatusSuperseded {
		t.Fatalf("previous version should be SUPERSEDED, got %s", old.Template.Status)
	}

	if err := svc.EnsureDefaultInquiryTemplate(ctx, tenantID); err != nil {
		t.Fatal(err)
	}
}

func TestInquiryTemplateRejectsDuplicateCodeAndDefaultDisable(t *testing.T) {
	svc, ctx, tenantID := templateTestService(t)
	op := Operator{ID: 1, Name: "tester"}
	if err := svc.EnsureDefaultInquiryTemplate(ctx, tenantID); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.CreateInquiryTemplate(ctx, tenantID, InquiryTemplateInput{
		TemplateCode: SystemInquiryTemplateCode, Name: "撞编码", Fields: validTemplateFields(),
	}, op); err == nil {
		t.Fatal("duplicate active code should be rejected")
	}

	company, err := svc.CreateInquiryTemplate(ctx, tenantID, InquiryTemplateInput{
		TemplateCode: "AAA_STANDARD", Name: "AAA 公司标准询盘", Fields: validTemplateFields(),
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	if company.Template.Version != 1 || company.Template.IsDefault {
		t.Fatalf("unexpected company template: %#v", company.Template)
	}

	def, err := svc.GetDefaultInquiryTemplate(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetInquiryTemplateStatus(ctx, tenantID, def.Template.ID, "DISABLED", op); err == nil {
		t.Fatal("disabling the default template should be rejected")
	}

	if _, err := svc.SetDefaultInquiryTemplate(ctx, tenantID, company.Template.ID, op); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetInquiryTemplateStatus(ctx, tenantID, def.Template.ID, "DISABLED", op); err != nil {
		t.Fatal(err)
	}
	current, err := svc.GetDefaultInquiryTemplate(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Template.TemplateCode != "AAA_STANDARD" {
		t.Fatalf("default should move to the company template, got %s", current.Template.TemplateCode)
	}
}
