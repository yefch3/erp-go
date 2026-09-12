#!/bin/sh
# CI guard: 按需引入之后，源码里用到的每一个 el-* 组件都要真的进构建产物。
#
# Element Plus 从「整包注册」改成「按用到的引入」（vite.config 里的 Components
# 插件）之后，组件是靠名字被解析出来的。解析不到不会报错，也不会让构建失败
# ——它只是渲染不出那个组件，而且**只在打开那一页时才看得见**。四十个页面里
# 有几页是月底才有人开的。
#
# 所以这里做一次静态核对：源码里出现过的每个 <el-xxx>，构建产物里必须找得到
# 对应的 ElXxx。挡的不是「某个组件名写错了」，是「那个插件哪天不工作了」——
# 升级、误删配置、换构建工具，后果都是一整页的组件集体消失。
#
# 跑在 frontend-ci 里，紧跟 npm run build 之后（它需要 dist/）。
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
dist="$root/frontend/dist/assets"

[ -d "$dist" ] || { echo "先 npm run build：$dist 不存在" >&2; exit 1; }

missing=""
count=0
for c in $(grep -rhoE "<el-[a-z-]+" --include='*.vue' "$root/frontend/src" | sed 's/<el-//' | sort -u); do
    count=$((count + 1))
    # el-table-column → ElTableColumn
    pascal="El$(echo "$c" | awk -F- '{for (i = 1; i <= NF; i++) printf toupper(substr($i, 1, 1)) substr($i, 2)}')"
    grep -ql "$pascal" "$dist"/*.js 2>/dev/null || missing="$missing $c"
done

if [ -n "$missing" ]; then
    echo "这些组件在源码里用着，构建产物里却找不到：$missing" >&2
    echo "多半是 vite.config 里那个 Components 插件没在工作——那一页上的组件会集体渲染不出来。" >&2
    exit 1
fi
echo "frontend component check passed ($count components)"
