package app

import (
	"strings"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
)

// validBusinessDate 校验一个从浏览器来的业务日期。
//
// 存在的理由：这些日期一路当字符串传到 SQL，写库那一句是
// `nullif($n::text, '')::date`。校验漏掉的话，一个打错的日子不会得到
// 「日期格式不对」，而是 PostgreSQL 的 22007 一路冒到网关——那边没有
// PgError → apierr 的映射，最后落进 500 INTERNAL。用户看到的是「系统
// 错误」，而实际上只是他把 2026-13-01 填进了月份。
//
// 空串合法：这几个字段都是可空的，「还没定」是正当状态。
//
// 只管格式，不管远近。过去的日期是正当的——补录一张上个月就该付的单，
// 或者把逾期已久的历史单补上到期日，都是真实操作。
func validBusinessDate(v, code, label string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return apierr.Invalid(code, label+"格式不对，应该是 2026-01-31 这样的年-月-日")
	}
	return nil
}
