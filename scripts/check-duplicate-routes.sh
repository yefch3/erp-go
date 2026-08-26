#!/bin/sh
# CI guard：网关里同一个「方法 + 地址」不许注册两次。
#
# 这条守卫来自一次真实事故。GET /api/bank-transactions 被注册了两遍：
# 一遍给收款对账（转出口服务），一遍给银行流水（转采购服务），中间隔着
# 144 行，分属两个功能、两次提交。
#
# chi 对重复注册**既不报错也不警告，后注册的静默覆盖先注册的**。于是：
#
#   - 收款对账的列表打到了采购的服务上
#   - 两边返回的字段名还不一样（采购叫 items，出口叫 transactions）
#   - 页面拿不到自己要的字段，渲染成空表，并礼貌地显示「没有待处理的流水」
#   - 「登记流水」是 POST，没被覆盖，照样写进出口的库
#
# 结果是写和读指向两个不同的数据库，而全程没有一行错误日志、没有一个红色
# 的测试。它是被人一行行读路由表读出来的。
#
# 这种错不该靠人眼发现，所以有了这个脚本。
set -eu

FILE=services/gateway/internal/httpapi/server.go

[ -f "$FILE" ] || { echo "找不到 $FILE"; exit 1; }

# 从 .Get("/api/x", ...) 这样的调用里抠出「方法 地址」，排序后找重复。
# 行号一起带着，重复时要指出是哪几行。
# 全部用 awk：BSD 和 GNU 的 sed 正则方言不一样（\| 的「或」是 GNU 扩展），
# 在 macOS 上会静静地什么都不匹配——这个脚本第一版就栽在这上面，跑出来是
# 「通过」，其实一行都没读进去。
dupes=$(
  awk '
    match($0, /\.(Get|Post|Put|Delete|Patch|Head|Options)\("\/api[^"]*"/) {
      call = substr($0, RSTART + 1, RLENGTH - 1)      # Get("/api/x"
      split(call, part, "\\(\"")                       # part[1]=Get  part[2]=/api/x"
      method = part[1]
      path = substr(part[2], 1, length(part[2]) - 1)  # 去掉结尾的引号
      key = method " " path
      if (key in firstLine) {
        printf "  %-6s %-45s 第 %s 行 和 第 %s 行\n", method, path, firstLine[key], NR
        bad = 1
      } else {
        firstLine[key] = NR
      }
    }
    END { exit 0 }
  ' "$FILE"
)

if [ -n "$dupes" ]; then
  echo "同一个「方法 + 地址」注册了不止一次："
  echo "$dupes"
  echo ""
  echo "chi 不会报错，它会让后注册的那条静默覆盖前一条——前一条的处理函数"
  echo "和权限一起失效，而调用方看到的是一个格式对不上的回复或一个空列表。"
  echo "给其中一条换个地址。"
  exit 1
fi

echo "duplicate route check passed"
