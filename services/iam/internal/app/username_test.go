package app

import "testing"

func TestValidateUsername(t *testing.T) {
	// 像邮箱的也行：登录名是任意字符串，系统不关心它长什么样。
	ok := []string{"zhangsan", "zhang.san", "zhang_san-01", "张三", "ZhangSan", "ab", "zhangsan@xxx.com", "sales/01"}
	for _, u := range ok {
		if err := validateUsername(u); err != nil {
			t.Errorf("%q should be a valid login name: %v", u, err)
		}
	}
	bad := map[string]string{
		"a":                      "太短",
		"zhang san":              "有空格",
		"zhang\tsan":             "有控制字符",
		string(make([]rune, 65)): "太长",
	}
	for u, why := range bad {
		if err := validateUsername(u); err == nil {
			t.Errorf("%q should be rejected (%s)", u, why)
		}
	}
}
