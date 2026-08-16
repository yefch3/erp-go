package app

import (
	"reflect"
	"testing"
)

// TestSameText 验证重复预检会忽略首尾空格和英文大小写。
func TestSameText(t *testing.T) {
	tests := []struct {
		name        string
		left, right string
		want        bool
	}{
		{name: "忽略空格和大小写", left: "Acme Trading", right: " acme trading ", want: true},
		{name: "空输入不匹配", left: "Acme Trading", right: "", want: false},
		{name: "不同内容不匹配", left: "Acme Trading", right: "Acme Factory", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameText(tt.left, tt.right); got != tt.want {
				t.Fatalf("sameText(%q, %q)=%v, want %v", tt.left, tt.right, got, tt.want)
			}
		})
	}
}

// TestNameMatches 验证精确名称和相似名称会返回不同的提示类型。
func TestNameMatches(t *testing.T) {
	tests := []struct {
		name             string
		candidate, input string
		want             []string
	}{
		{name: "精确名称", candidate: "华东贸易", input: "华东贸易", want: []string{"NAME"}},
		{name: "相似名称", candidate: "华东贸易有限公司", input: "华东贸易", want: []string{"SIMILAR_NAME"}},
		{name: "未输入名称", candidate: "华东贸易", input: "", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nameMatches(tt.candidate, tt.input); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("nameMatches(%q, %q)=%v, want %v", tt.candidate, tt.input, got, tt.want)
			}
		})
	}
}
