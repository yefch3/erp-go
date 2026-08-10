package app

import "testing"

func TestValidateEmployeeEmail(t *testing.T) {
	for _, email := range []string{"", "employee@example.com", "name.surname+erp@example.cn"} {
		if err := validateEmployeeEmail(email); err != nil {
			t.Fatalf("合法邮箱 %q 被拒绝: %v", email, err)
		}
	}
	for _, email := range []string{"invalid", "Name <employee@example.com>", "a@"} {
		if err := validateEmployeeEmail(email); err == nil {
			t.Fatalf("非法邮箱 %q 未被拒绝", email)
		}
	}
}

func TestParseOptionalDate(t *testing.T) {
	blank, err := parseOptionalDate("", "入职日期")
	if err != nil || blank.Valid {
		t.Fatalf("空日期应当被当作未填写: value=%+v err=%v", blank, err)
	}
	date, err := parseOptionalDate("2026-08-10", "入职日期")
	if err != nil || !date.Valid || date.Time.Format("2006-01-02") != "2026-08-10" {
		t.Fatalf("合法日期解析错误: value=%+v err=%v", date, err)
	}
	if _, err := parseOptionalDate("08/10/2026", "入职日期"); err == nil {
		t.Fatal("非 YYYY-MM-DD 日期未被拒绝")
	}
}

func TestValidateEmployeePhone(t *testing.T) {
	for _, phone := range []string{"", "+1 (212) 555-0100", "021-55556666"} {
		if err := validateEmployeePhone(phone); err != nil {
			t.Fatalf("合法电话 %q 被拒绝: %v", phone, err)
		}
	}
	for _, phone := range []string{"123", "call-me-now", "1234567890123456789012345678901"} {
		if err := validateEmployeePhone(phone); err == nil {
			t.Fatalf("非法电话 %q 未被拒绝", phone)
		}
	}
}
