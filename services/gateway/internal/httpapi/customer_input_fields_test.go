package httpapi

import (
	"os"
	"regexp"
	"slices"
	"testing"

	"google.golang.org/protobuf/proto"

	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
)

// 客户详情页的「编辑联系人」「编辑地址」保存前只挑接口认的字段发
// （frontend/src/lib/customerForms.ts 里那两张表）。原因见 body_fields_test.go
// 文件头：网关按 proto 严格解析，表单是从列表行整个复制来的，行上的 id、
// status 一发就整单 400。
//
// 这里钉住那两张表和 proto 一一对应，两个方向都查：
//   - 表里多一个 proto 没有的字段 → 网关拒收，编辑又坏回去；
//   - proto 加了字段、表里没跟上 → 那个字段在页面上填了也永远存不进去，
//     而且一个字都不报。后者更坏，它看着像存上了。
func TestCustomerInputFieldListsMatchProto(t *testing.T) {
	src, err := os.ReadFile("../../../../frontend/src/lib/customerForms.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		list string
		msg  proto.Message
	}{
		{"contactInputFields", &mdv1.CustomerContactInput{}},
		{"addressInputFields", &mdv1.CustomerAddressInput{}},
	} {
		inTS := fieldListInTS(t, string(src), tc.list)
		var inProto []string
		fields := tc.msg.ProtoReflect().Descriptor().Fields()
		for i := 0; i < fields.Len(); i++ {
			inProto = append(inProto, fields.Get(i).JSONName())
		}
		for _, f := range inTS {
			if !slices.Contains(inProto, f) {
				t.Errorf("%s 里有 %q，但 proto 里没有——网关会拒收整个请求", tc.list, f)
			}
		}
		for _, f := range inProto {
			if !slices.Contains(inTS, f) {
				t.Errorf("proto 有 %q，但 %s 里没有——页面上填了也存不进去", f, tc.list)
			}
		}
	}
}

// fieldListInTS 从 `const <name> = [ '...', '...' ] as const` 里抠出字段名。
func fieldListInTS(t *testing.T, src, name string) []string {
	t.Helper()
	block := regexp.MustCompile(`const ` + name + ` = \[([^\]]*)\] as const`).FindStringSubmatch(src)
	if block == nil {
		t.Fatalf("customerForms.ts 里没找到 %s——是不是改了写法？", name)
	}
	var out []string
	for _, m := range regexp.MustCompile(`'([A-Za-z]+)'`).FindAllStringSubmatch(block[1], -1) {
		out = append(out, m[1])
	}
	if len(out) == 0 {
		t.Fatalf("%s 是空的", name)
	}
	return out
}
