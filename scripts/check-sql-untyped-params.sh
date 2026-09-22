#!/bin/sh
# CI guard：jsonb_build_object 这一族函数里的占位符必须写明类型。
#
# 2026-09-22 的事故：改「手工应收」必报错，日志里是
#
#     ERROR: could not determine data type of parameter $4 (SQLSTATE 42P18)
#
# 来源是 services/export/internal/app/receivable.go 里这一句：
#
#     jsonb_build_object('customerName',$4,'contractNo',$5,'dueDate',$6)
#
# 这一族函数的参数类型声明是 `VARIADIC "any"`——**它不给 Postgres 任何类型
# 信息**。别的函数（比如 lower($1)）能反推出 $1 是 text，这里推不出来，于是
# 准备语句的那一刻就失败。同样的写法在采购那边的「手工应付」里也有一份，
# 一起修了。
#
# 为什么值得单开一条守卫，而不是补两条测试：
#
# · **编译器完全看不见。** 它是一个字符串，go build 一个字都不说。
# · **只在真库上才炸。** 不连库的单元测试跑得再多也照样绿。
# · 这两个函数当时**一行测试都没有**，而下一个写 jsonb_build_object 的人
#   多半也不会先写测试。守卫管的是"以后所有人"，测试只管它自己那一个。
#
# 别的 "any" 函数（concat、concat_ws、format）有同样的毛病，暂时没列进来：
# 仓库里现在没有那种写法，而把不存在的模式加进正则只会让这条规则更脆。真
# 撞上了再加。
set -eu

PATTERN='json_build_object|jsonb_build_object|json_build_array|jsonb_build_array'

fail=0
for f in $(grep -rl -E "$PATTERN" --include='*.go' services pkg 2>/dev/null); do
  # 把每一处 xxx_build_object( ... ) 的内容抠出来，逐个看里面的占位符。
  # -o 只吐出匹配到的那一段，所以同一行里有两处也各算各的。
  #
  # 用 [^()]* 而不是 .*：后者会从第一个 ( 一路吃到行尾最后一个 )，把
  # 后面别的函数调用也算进来，于是一个正常的 $1 被判成违规。
  hits=$(grep -o -E "($PATTERN)\([^()]*\)" "$f" \
         | grep -E '\$[0-9]+[^:]' || true)
  [ -z "$hits" ] || {
    echo "UNTYPED PARAM in $f"
    printf '%s\n' "$hits" | sed 's/^/    /'
    fail=1
  }
done

if [ "$fail" -ne 0 ]; then
  cat <<'MSG'

jsonb_build_object 这一族的参数类型是 "any"，Postgres 推不出占位符是什么，
准备语句时就会失败：could not determine data type of parameter $N（42P18）。

把类型写出来即可，值是 Go 的 string 就写 ::text：

    jsonb_build_object('name',$4,'no',$5)          ← 运行时报错
    jsonb_build_object('name',$4::text,'no',$5::text)  ← 对

这类错编译器看不见、不连库的测试也测不出来，只有真的执行到才炸——而执行到
的时候，用户正在点保存。
MSG
  exit 1
fi
echo "sql untyped param check passed"
