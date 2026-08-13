package app

import (
	"context"
	"os"
	"testing"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestValidatePortInput(t *testing.T) {
	tests := []struct {
		name string
		in   PortInput
		bad  bool
	}{
		{name: "有效港口", in: PortInput{Port: Port{UNLOCODE: "cnsha", CountryCode: "cn", NameZH: "上海港", NameEN: "Shanghai", Timezone: "Asia/Shanghai", Aliases: []string{"上海", " 上海 "}}}},
		{name: "国家与代码不一致", in: PortInput{Port: Port{UNLOCODE: "USLAX", CountryCode: "CN", NameZH: "洛杉矶港", NameEN: "Los Angeles", Timezone: "America/Los_Angeles"}}, bad: true},
		{name: "无效时区", in: PortInput{Port: Port{UNLOCODE: "CNSHA", CountryCode: "CN", NameZH: "上海港", NameEN: "Shanghai", Timezone: "UTC+8"}}, bad: true},
		{name: "缺少中文名称", in: PortInput{Port: Port{UNLOCODE: "CNSHA", CountryCode: "CN", NameEN: "Shanghai", Timezone: "Asia/Shanghai"}}, bad: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePortInput(&tt.in)
			if (err != nil) != tt.bad {
				t.Fatalf("error=%v bad=%v", err, tt.bad)
			}
		})
	}
}

// TestPortLifecycle 使用真实 PostgreSQL 验证新增、搜索、编辑和软停用闭环。
func TestPortLifecycle(t *testing.T) {
	dsn := os.Getenv("MD_TEST_DSN")
	if dsn == "" {
		t.Skip("MD_TEST_DSN not set; skipping DB-backed test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool)
	created, err := svc.CreatePort(ctx, 1, PortInput{Port: Port{UNLOCODE: "ZZTST", CountryCode: "ZZ", NameZH: "测试港", NameEN: "Test Port", City: "Test City", Timezone: "UTC", Aliases: []string{"Old Test Port"}}, OperatorID: 1, OperatorName: "test"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM port_change_logs WHERE tenant_id=1 AND port_id=$1", created.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM ports WHERE tenant_id=1 AND id=$1", created.ID)
	}()
	rows, total, err := svc.ListPorts(ctx, 1, "Old Test Port", "ZZ", "ACTIVE", 1, 20)
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatalf("search total=%d rows=%d err=%v", total, len(rows), err)
	}
	created.City = "New City"
	updated, err := svc.UpdatePort(ctx, 1, PortInput{Port: created, OperatorID: 1, OperatorName: "test"})
	if err != nil || updated.City != "New City" {
		t.Fatalf("update: %#v %v", updated, err)
	}
	stopped, err := svc.SetPortStatus(ctx, 1, created.ID, "INACTIVE", updated.Version, 1, "test")
	if err != nil || stopped.Status != "INACTIVE" {
		t.Fatalf("deactivate: %#v %v", stopped, err)
	}
}

// TestPortImport 验证导入必须先预检，并且确认后整批写入；错误批次不会落库。
func TestPortImport(t *testing.T) {
	dsn := os.Getenv("MD_TEST_DSN")
	if dsn == "" {
		t.Skip("MD_TEST_DSN not set; skipping DB-backed test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM port_change_logs WHERE tenant_id=1 AND port_id IN (SELECT id FROM ports WHERE tenant_id=1 AND unlocode IN ('ZZI01','ZZI02'))")
		_, _ = pool.Exec(ctx, "DELETE FROM ports WHERE tenant_id=1 AND unlocode IN ('ZZI01','ZZI02')")
	}()
	svc := New(pool)
	rows := []PortImportRow{
		{RowNumber: 2, PortInput: PortInput{Port: Port{UNLOCODE: "ZZI01", CountryCode: "ZZ", NameZH: "导入港一", NameEN: "Import One", Timezone: "UTC"}}},
		{RowNumber: 3, PortInput: PortInput{Port: Port{UNLOCODE: "ZZI02", CountryCode: "ZZ", NameZH: "导入港二", NameEN: "Import Two", Timezone: "UTC"}}},
	}
	preview, err := svc.ImportPorts(ctx, 1, rows, false, 1, "test")
	if err != nil || preview.CreateCount != 2 || len(preview.Issues) != 0 {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}
	var before int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM ports WHERE tenant_id=1 AND unlocode IN ('ZZI01','ZZI02')").Scan(&before); err != nil || before != 0 {
		t.Fatalf("preview wrote rows: count=%d err=%v", before, err)
	}
	committed, err := svc.ImportPorts(ctx, 1, rows, true, 1, "test")
	if err != nil || committed.CreateCount != 2 {
		t.Fatalf("commit=%#v err=%v", committed, err)
	}
	bad := append(rows, PortImportRow{RowNumber: 4, PortInput: PortInput{Port: Port{UNLOCODE: "BAD", CountryCode: "ZZ", NameZH: "错误", NameEN: "Bad", Timezone: "UTC"}}})
	result, err := svc.ImportPorts(ctx, 1, bad, true, 1, "test")
	if err != nil || len(result.Issues) == 0 {
		t.Fatalf("invalid batch result=%#v err=%v", result, err)
	}
}
