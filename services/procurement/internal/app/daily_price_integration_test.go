package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

type dailyAllowAll struct{}

func (dailyAllowAll) Allowed(context.Context, int64, string) (bool, error) { return true, nil }

type dailyTestScope struct{}

func (dailyTestScope) VisibleEmployees(_ context.Context, id int64, module string) (Visibility, error) {
	if module != "daily_base_price" {
		return Visibility{EmployeeIDs: []int64{id}}, nil
	}
	if id == 99 {
		return Visibility{All: true}, nil
	}
	return Visibility{EmployeeIDs: []int64{id}}, nil
}

func TestDailyPriceStartsWithEditableSheetTemplate(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM daily_price_dimensions WHERE tenant_id=$1`, tenant) }()
	svc := New(pool, Deps{Permissions: dailyAllowAll{}})
	for attempt := 0; attempt < 2; attempt++ {
		result, err := svc.DailyPrice(ctx, tenant, Operator{ID: 1}, DailyPriceCommand{Action: "config"})
		if err != nil {
			t.Fatal(err)
		}
		rows := result.(map[string]any)["dimensions"].([]DailyDimension)
		counts := map[string]int{}
		notes := map[string]string{}
		for _, row := range rows {
			counts[row.Kind]++
			if row.Kind == "SUPPLIER" {
				notes[row.Name] = row.Note
			}
		}
		if counts["PRODUCT"] != 4 || counts["SUPPLIER"] != 6 || counts["SPREAD"] != 6 || len(rows) != 16 {
			t.Fatalf("template dimensions on attempt %d: %+v", attempt, counts)
		}
		if notes["东钢"] != "出厂价" || notes["神龙"] != "鲅鱼圈" || notes["澳森"] != "天津港" {
			t.Fatalf("supplier quote notes on attempt %d: %+v", attempt, notes)
		}
		if attempt == 0 {
			for _, row := range rows {
				if row.Kind == "SUPPLIER" && row.Name == "东钢" {
					_, err = svc.DailyPrice(ctx, tenant, Operator{ID: 1}, DailyPriceCommand{Action: "configure", ID: row.ID, Kind: row.Kind, Active: &row.Active, SortOrder: row.SortOrder, Note: "天津港"})
					if err != nil {
						t.Fatal(err)
					}
					_, err = svc.DailyPrice(ctx, tenant, Operator{ID: 1}, DailyPriceCommand{Action: "configure", ID: row.ID, Kind: row.Kind, Active: &row.Active, SortOrder: row.SortOrder, Note: "出厂价"})
					if err != nil {
						t.Fatal(err)
					}
					break
				}
			}
		}
	}
	var priceCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM daily_base_prices WHERE tenant_id=$1`, tenant).Scan(&priceCount); err != nil || priceCount != 0 {
		t.Fatalf("template must not create market prices: count=%d err=%v", priceCount, err)
	}
	var unusedID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM daily_price_dimensions WHERE tenant_id=$1 AND kind='PRODUCT' AND name='冷卷'`, tenant).Scan(&unusedID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DailyPrice(ctx, tenant, Operator{ID: 1}, DailyPriceCommand{Action: "deleteDimension", ID: unusedID}); err != nil {
		t.Fatalf("delete unused dimension: %v", err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM daily_price_dimensions WHERE tenant_id=$1`, tenant).Scan(&remaining); err != nil || remaining != 15 {
		t.Fatalf("dimension deletion: remaining=%d err=%v", remaining, err)
	}
}

func TestDailyPriceTenantOwnerAndUpsert(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_base_prices WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM daily_basis_spreads WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM daily_price_dimensions WHERE tenant_id=$1`, tenant)
	}()
	var product, supplier, spread int64
	for _, v := range []struct {
		kind, name string
		id         *int64
	}{{"PRODUCT", "Coil", &product}, {"SUPPLIER", "Mill", &supplier}, {"SPREAD", "Hot coil", &spread}} {
		if err = pool.QueryRow(ctx, `INSERT INTO daily_price_dimensions(tenant_id,kind,name) VALUES($1,$2,$3) RETURNING id`, tenant, v.kind, v.name).Scan(v.id); err != nil {
			t.Fatal(err)
		}
	}
	svc := New(pool, Deps{Permissions: dailyAllowAll{}, Scopes: dailyTestScope{}})
	date := "2026-09-23"
	price := DailyPriceInput{ProductID: product, SupplierID: supplier, Price: "3265", Remark: "Factory gate"}
	_, err = svc.DailyPrice(ctx, tenant, Operator{ID: 1, Name: "Buyer A"}, DailyPriceCommand{Action: "savePrices", Date: date, Prices: []DailyPriceInput{price}})
	if err != nil {
		t.Fatal(err)
	}
	day, err := svc.DailyPrice(ctx, tenant, Operator{ID: 1}, DailyPriceCommand{Action: "day", Date: date})
	if err != nil {
		t.Fatal(err)
	}
	first := day.(map[string]any)["prices"].([]DailyPriceRecord)
	if len(first) != 1 || first[0].Price != "3265.0000" || first[0].CreatedBy != 1 {
		t.Fatalf("first save: %+v", first)
	}
	if _, err = svc.DailyPrice(ctx, tenant, Operator{ID: 1}, DailyPriceCommand{Action: "deleteDimension", ID: product}); err == nil {
		t.Fatal("dimension with historical price was deleted")
	}
	price.Price = "3270"
	if _, err = svc.DailyPrice(ctx, tenant, Operator{ID: 2}, DailyPriceCommand{Action: "savePrices", Date: date, Prices: []DailyPriceInput{price}}); err == nil {
		t.Fatal("other buyer overwrote price")
	}
	other, err := svc.DailyPrice(ctx, tenant+1, Operator{ID: 2}, DailyPriceCommand{Action: "day", Date: date})
	if err != nil {
		t.Fatal(err)
	}
	if len(other.(map[string]any)["prices"].([]DailyPriceRecord)) != 0 {
		t.Fatal("cross-tenant price leaked")
	}
	_, err = svc.DailyPrice(ctx, tenant, Operator{ID: 99, Name: "Manager"}, DailyPriceCommand{Action: "savePrices", Date: date, Prices: []DailyPriceInput{price}})
	if err != nil {
		t.Fatal(err)
	}
	day, err = svc.DailyPrice(ctx, tenant, Operator{ID: 99}, DailyPriceCommand{Action: "day", Date: date})
	if err != nil {
		t.Fatal(err)
	}
	updated := day.(map[string]any)["prices"].([]DailyPriceRecord)
	if len(updated) != 1 || updated[0].ID != first[0].ID || updated[0].Price != "3270.0000" || updated[0].CreatedBy != 1 || updated[0].UpdatedBy != 99 {
		t.Fatalf("upsert: %+v", updated)
	}
	_, err = svc.DailyPrice(ctx, tenant, Operator{ID: 1}, DailyPriceCommand{Action: "saveSpreads", Date: date, Spreads: []DailySpreadInput{{ProductID: spread, Spot: "3250", Futures: "0"}}})
	if err != nil {
		t.Fatal(err)
	}
	day, err = svc.DailyPrice(ctx, tenant, Operator{ID: 1}, DailyPriceCommand{Action: "day", Date: date})
	if err != nil {
		t.Fatal(err)
	}
	spreads := day.(map[string]any)["spreads"].([]DailySpreadRecord)
	if len(spreads) != 1 || spreads[0].BasisPercent != nil {
		t.Fatalf("zero futures: %+v", spreads)
	}
}
