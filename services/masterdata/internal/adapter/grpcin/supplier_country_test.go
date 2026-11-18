package grpcin

import (
	"context"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/masterdata/internal/app"
	"google.golang.org/grpc"
)

// 供应商的国家码是定长两位的列：没填的存进去会被数据库补成两个空格。
//
// 列表页上方「国家/地区」下拉里「没填国家」的那一组因此一直是坏的：
//   - 分组接口把两个空格原样交出去，页面当它是个国家码，名字显示成空白的「（N）」；
//   - 选中它，筛选条件在服务里被去掉空格变成空串，而空串的意思是「不筛」——
//     顶上写着「· N」，下面还是全部供应商。
//
// 客户那边早就有约定：交出去的国家码去掉空格（空串 = 没填），想只看没填国家的
// 就传 __UNCLASSIFIED__。这里钉住供应商也守同一个约定，并且「只能看到自己
// 负责的供应商」这条规则在新的筛法下照样成立。
func TestSupplierCountryFilterCoversSuppliersWithoutACountry(t *testing.T) {
	dsn := os.Getenv("MD_TEST_DSN")
	if dsn == "" {
		t.Skip("MD_TEST_DSN not set")
	}
	pool, err := pgdb.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	// 清理是后进先出的：先登记关池子、后登记删数据，删数据时池子才还开着。
	t.Cleanup(pool.Close)
	h := New(app.New(pool))
	tenant := time.Now().UnixNano() / 1000
	t.Cleanup(func() {
		for _, table := range []string{"supplier_change_logs", "supplier_contacts", "supplier_owners", "suppliers"} {
			if _, err := pool.Exec(context.Background(), "DELETE FROM "+table+" WHERE tenant_id=$1", tenant); err != nil {
				t.Errorf("清理 %s 失败：%v", table, err)
			}
		}
	})

	// 和 TestSupplierAccessLifecycleIntegration 一样走真的访问拦截器：99 号是
	// 最高权限（看全部），其余的人只看得到自己负责的。
	const admin, buyer, otherBuyer = int64(99), int64(1), int64(2)
	call := func(actor int64, method string, req any) any {
		t.Helper()
		ctx := grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: tenant, EmployeeID: actor, Name: "Employee"})
		resp, err := h.SupplierAccess(supplierIAM{})(ctx, req, &grpc.UnaryServerInfo{FullMethod: "/erp.masterdata.v1.SupplierService/" + method}, func(ctx context.Context, req any) (any, error) {
			values := reflect.ValueOf(h).MethodByName(method).Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(req)})
			if !values[1].IsNil() {
				return nil, values[1].Interface().(error)
			}
			return values[0].Interface(), nil
		})
		if err != nil {
			t.Fatalf("%s as %d: %v", method, actor, err)
		}
		return resp
	}
	// 创建人自动成为负责人，所以「谁建的」就是「谁看得到」。
	create := func(actor int64, code, name, country string) int64 {
		t.Helper()
		return call(actor, "CreateSupplier", &mdv1.CreateSupplierRequest{
			Code: code, NameZh: name, CountryCode: country, BusinessTypes: []string{"GENERAL"},
		}).(*mdv1.CreateSupplierResponse).Supplier.Id
	}
	vietnam := create(buyer, "CTRY-VN", "越南钢管厂", "vn")
	china := create(otherBuyer, "CTRY-CN", "中国钢板厂", "CN")
	unset := create(buyer, "CTRY-NONE-1", "没填国家的厂", "")
	unsetOfOther := create(otherBuyer, "CTRY-NONE-2", "别人负责、也没填国家的厂", "")

	list := func(actor int64, country string) *mdv1.ListSuppliersResponse {
		t.Helper()
		return call(actor, "ListSuppliers", &mdv1.ListSuppliersRequest{
			CountryCode: country, Page: &commonv1.PageRequest{Page: 1, PageSize: 50},
		}).(*mdv1.ListSuppliersResponse)
	}
	ids := func(resp *mdv1.ListSuppliersResponse) []int64 {
		out := make([]int64, 0, len(resp.GetSuppliers()))
		for _, s := range resp.GetSuppliers() {
			out = append(out, s.GetId())
		}
		sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
		return out
	}
	sorted := func(v ...int64) []int64 {
		sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
		return v
	}

	t.Run("专用取值只列没填国家的供应商", func(t *testing.T) {
		resp := list(admin, "__UNCLASSIFIED__")
		if got, want := ids(resp), sorted(unset, unsetOfOther); !reflect.DeepEqual(got, want) {
			t.Fatalf("没填国家这一组筛出来的是 %v，应为 %v", got, want)
		}
		if resp.GetMeta().GetTotal() != 2 {
			t.Fatalf("总数 = %d，应为 2", resp.GetMeta().GetTotal())
		}
		for _, s := range resp.GetSuppliers() {
			if s.GetCountryCode() != "" {
				t.Fatalf("没填国家的供应商交出去的国家码是 %q，应为空串", s.GetCountryCode())
			}
		}
	})

	t.Run("只能看到自己负责的这条规则照样成立", func(t *testing.T) {
		if got, want := ids(list(buyer, "__UNCLASSIFIED__")), []int64{unset}; !reflect.DeepEqual(got, want) {
			t.Fatalf("采购看到的没填国家的供应商是 %v，应只有自己负责的 %v", got, want)
		}
	})

	t.Run("选具体国家和全部国家的行为不变", func(t *testing.T) {
		for _, country := range []string{"VN", "vn", " vn "} {
			if got, want := ids(list(admin, country)), []int64{vietnam}; !reflect.DeepEqual(got, want) {
				t.Fatalf("country_code=%q 筛出来的是 %v，应为 %v", country, got, want)
			}
		}
		if got, want := ids(list(admin, "")), sorted(vietnam, china, unset, unsetOfOther); !reflect.DeepEqual(got, want) {
			t.Fatalf("不选国家时列出的是 %v，应为全部 %v", got, want)
		}
		// 两个空格以前就等于「不筛」（旧页面选「没填国家」那一组时发的就是它），保持不变。
		if got, want := ids(list(admin, "  ")), sorted(vietnam, china, unset, unsetOfOther); !reflect.DeepEqual(got, want) {
			t.Fatalf("country_code 为空白时列出的是 %v，应为全部 %v", got, want)
		}
	})

	t.Run("单个供应商交出去的国家码也是空串", func(t *testing.T) {
		got := call(admin, "GetSupplier", &mdv1.GetSupplierRequest{Id: unset}).(*mdv1.GetSupplierResponse).Supplier
		if got.GetCountryCode() != "" {
			t.Fatalf("国家码 = %q，应为空串", got.GetCountryCode())
		}
	})

	groups := func(actor int64) map[string]int64 {
		t.Helper()
		resp := call(actor, "ListSupplierCountries", &mdv1.ListSupplierCountriesRequest{}).(*mdv1.ListSupplierCountriesResponse)
		out := map[string]int64{}
		for _, g := range resp.GetCountries() {
			if _, dup := out[g.GetCountryCode()]; dup {
				t.Fatalf("国家 %q 出现了两组", g.GetCountryCode())
			}
			out[g.GetCountryCode()] = g.GetCount()
		}
		return out
	}

	t.Run("分组里没填国家的那一组国家码是空串", func(t *testing.T) {
		if got, want := groups(admin), map[string]int64{"": 2, "CN": 1, "VN": 1}; !reflect.DeepEqual(got, want) {
			t.Fatalf("最高权限看到的分组是 %v，应为 %v", got, want)
		}
		if got, want := groups(buyer), map[string]int64{"": 1, "VN": 1}; !reflect.DeepEqual(got, want) {
			t.Fatalf("采购看到的分组是 %v，应为 %v", got, want)
		}
	})
}
