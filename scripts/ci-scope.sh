#!/bin/sh
# 按改动路径算 CI 要跑哪些东西。
#
# 一次 `make ci` 要五分多钟，其中大头是 13 个 Go 模块的测试和 lint、再加前端
# 的安装/类型检查/构建/测试。而最近二十次合并里，十一次只改了 frontend/，
# 两次只改了 services/mail/，没有一次碰过 pkg/ 或别的服务。一个只改前端的
# PR 等 13 个 Go 模块跑完，等的是和它无关的东西。
#
# 这个脚本只回答一个问题：**这批改动能影响到哪些模块。** 规则写死在下面，
# 宁可多跑、不可漏跑：认不出的路径归零（README、docs/），但任何会波及全局的
# 路径（pkg/、Makefile、脚本、生成的 proto 的公共部分）一律全跑。
#
# **只有 PR 用它。push 到 main 一律全跑**——PR 上跳过的那些，合进去之后在
# main 上补齐，main 红了几分钟内就知道。所以这里算错的代价是「PR 绿了、main
# 红了一次」，不是「坏代码悄悄上线」。
#
# 用法：
#   sh scripts/ci-scope.sh <base> <head>    按 git diff base...head 算
#   sh scripts/ci-scope.sh --files -         从 stdin 读改动文件清单（测试用）
#
# 输出到 stdout，每行一个 KEY=VALUE，可直接 >> $GITHUB_OUTPUT：
#   modules=all | none | <空格分隔的模块目录，如 "gen services/mail">
#   frontend=1 | 0
# 给人看的说明打到 stderr。
set -eu

if [ "${1:-}" = "--files" ]; then
    files=$(cat)
else
    base=${1:?base ref}
    head=${2:?head ref}
    # 三个点：相对两者的共同祖先，PR 上就是「这个分支改了什么」。
    files=$(git diff --name-only "$base...$head")
fi

everything=0
frontend=0
mods=""
protopkgs=""

add_mod() {
    case " $mods " in
        *" $1 "*) ;;
        *) mods="$mods $1" ;;
    esac
}

for f in $files; do
    case "$f" in
        # 全局：谁都依赖的代码、定义「CI 跑什么」的东西、以及 proto 工具链本身。
        pkg/*|go.work|go.work.sum|Makefile|scripts/*|.github/workflows/ci.yml|deploy/init-databases.sh|buf.yaml|buf.gen.yaml|buf.lock|gen/go.mod|gen/go.sum)
            everything=1 ;;
        # 某一个服务的 proto（源或生成物）：波及所有 import 了它的模块，下面再算。
        gen/go/erp/*/v1/*)
            protopkgs="$protopkgs $(echo "$f" | cut -d/ -f4)"; add_mod gen ;;
        proto/erp/*/v1/*)
            protopkgs="$protopkgs $(echo "$f" | cut -d/ -f3)"; add_mod gen ;;
        # gen/ 或 proto/ 里别的东西（buf 配置、模块文件）：认不清就全跑。
        gen/*|proto/*)
            everything=1 ;;
        services/*/*)
            add_mod "services/$(echo "$f" | cut -d/ -f2)" ;;
        frontend/*)
            frontend=1 ;;
        # 其余（docs/、deploy/ 的 compose、README）：不碰任何测试。
        # deploy/ 的守卫（check-compose-env）不在这个范围里，它永远跑。
        *) ;;
    esac
done

# 空 diff（比如一个没有改动的 PR，或者本地改动还没提交）：认不出就全跑。
if [ -z "$files" ]; then
    echo "ci-scope: no changed files found, running everything" >&2
    echo "modules=all"
    echo "frontend=1"
    exit 0
fi

if [ "$everything" = 1 ]; then
    echo "ci-scope: a global path changed, running everything" >&2
    echo "modules=all"
    echo "frontend=1"
    exit 0
fi

# 某个服务的 proto 变了 → 所有 import 了它的模块。common/v1 谁都 import，
# 自然会算出一大片；那是对的，不用特判。
for x in $protopkgs; do
    for f in $(grep -rl --include='*.go' "erp-go/gen/go/erp/$x/v1\"" pkg services 2>/dev/null); do
        case "$f" in
            pkg/*) add_mod pkg ;;
            services/*) add_mod "services/$(echo "$f" | cut -d/ -f2)" ;;
        esac
    done
    [ -d "services/$x" ] && add_mod "services/$x"
done

# 顺序固定：pkg、gen、然后各服务按名字——好让两次同样的改动给出同样的清单。
ordered=""
for m in pkg gen; do
    case " $mods " in *" $m "*) ordered="$ordered $m" ;; esac
done
for m in $(printf '%s\n' $mods | grep '^services/' | sort -u); do
    ordered="$ordered $m"
done
ordered=$(echo "$ordered" | sed 's/^ *//')

{
    echo "ci-scope: changed files:"
    printf '%s\n' $files | head -40 | sed 's/^/    /'
    n=$(printf '%s\n' $files | wc -l | tr -d ' ')
    [ "$n" -gt 40 ] && echo "    … and $((n - 40)) more"
    echo "ci-scope: Go modules to test and lint: ${ordered:-none}"
    echo "ci-scope: frontend: $([ "$frontend" = 1 ] && echo yes || echo skipped)"
} >&2

echo "modules=${ordered:-none}"
echo "frontend=$frontend"
