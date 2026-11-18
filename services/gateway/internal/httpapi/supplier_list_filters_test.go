package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// supplierListCapture 记下网关转给主数据服务的那一份供应商列表请求。
type supplierListCapture struct {
	mdv1.SupplierServiceClient
	received *mdv1.ListSuppliersRequest
}

func (c *supplierListCapture) ListSuppliers(_ context.Context, req *mdv1.ListSuppliersRequest, _ ...grpc.CallOption) (*mdv1.ListSuppliersResponse, error) {
	c.received = req
	return &mdv1.ListSuppliersResponse{}, nil
}

// 供应商列表上方的「国家/地区」下拉，选了以后页面发的是 ?country_code=XX。
//
// 网关以前只转发分页、关键字、状态、业务角色四样，country_code 在这一层被
// 丢掉：主数据服务收到的是空串，按「不筛」处理。于是顶上的家数跟着变了，
// 下面的行一条没少——而且全程没有任何报错。协议里有这个字段，主数据的 SQL
// 也认，缺的只是中间这一行（隔壁的客户列表一直是转了的）。
//
// 这里钉的是 SuppliersPage.vue 的 load() 实际发出的那一组参数：页面发几样，
// 网关就得原样转几样。
func TestListSuppliersForwardsEveryFilterTheListPageSends(t *testing.T) {
	cases := []struct {
		name  string
		query url.Values
		want  *mdv1.ListSuppliersRequest
	}{
		{
			name:  "只选国家",
			query: url.Values{"page": {"1"}, "page_size": {"100"}, "country_code": {"VN"}},
			want: &mdv1.ListSuppliersRequest{
				Page:        &commonv1.PageRequest{Page: 1, PageSize: 100},
				CountryCode: "VN",
			},
		},
		{
			name: "列表页的全部筛选条件一起用",
			query: url.Values{
				"page": {"2"}, "page_size": {"50"}, "keyword": {"钢管"},
				"status": {"ACTIVE"}, "country_code": {"VN"}, "business_type": {"FORWARDER"},
			},
			want: &mdv1.ListSuppliersRequest{
				Page:    &commonv1.PageRequest{Page: 2, PageSize: 50},
				Keyword: "钢管", Status: "ACTIVE", CountryCode: "VN", BusinessType: "FORWARDER",
			},
		},
		{
			// 选回「全部国家」时页面发的是空值，必须还是「不按国家筛」。
			name:  "不选国家",
			query: url.Values{"page": {"1"}, "page_size": {"100"}, "country_code": {""}, "business_type": {"CARRIER"}},
			want: &mdv1.ListSuppliersRequest{
				Page:         &commonv1.PageRequest{Page: 1, PageSize: 100},
				BusinessType: "CARRIER",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &supplierListCapture{}
			s := &Server{Suppliers: client}
			w := httptest.NewRecorder()

			s.listSuppliers(w, httptest.NewRequest(http.MethodGet, "/api/suppliers?"+tc.query.Encode(), nil))

			if w.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if client.received == nil {
				t.Fatal("列表请求没有转给主数据服务")
			}
			if !proto.Equal(client.received, tc.want) {
				t.Fatalf("转给主数据服务的筛选条件不对\n got: %v\nwant: %v", client.received, tc.want)
			}
		})
	}
}
