package app

import "testing"

func TestValidateUsername(t *testing.T) {
	ok := []string{"zhangsan", "zhang.san", "zhang_san-01", "张三", "ZhangSan", "ab"}
	for _, u := range ok {
		if err := validateUsername(u); err != nil {
			t.Errorf("%q should be a valid username: %v", u, err)
		}
	}
	bad := map[string]string{
		"a":                "太短",
		"zhang san":        "有空格",
		"zhang@san":        "有 @——会同时走邮箱和用户名两条路",
		"zhang/san":        "有斜杠",
		"zhang\tsan":       "有控制字符",
		string(make([]rune, 65)): "太长",
	}
	for u, why := range bad {
		if err := validateUsername(u); err == nil {
			t.Errorf("%q should be rejected (%s)", u, why)
		}
	}
}
