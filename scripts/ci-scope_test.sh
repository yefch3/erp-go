#!/bin/sh
# scripts/ci-scope.sh 的测试。每一条钉一种改动该跑什么。
#
# 这个脚本决定 PR 上**不跑**哪些测试，所以它自己错了不会有任何测试变红——
# 只会有一批测试悄悄不跑。所以它的规则在这里逐条写死，而且它跑在 `make ci`
# 里，在 main 上每次都全量跑一遍。
set -eu

fail=0
check() {
    name=$1; want_modules=$2; want_frontend=$3; shift 3
    out=$(printf '%s\n' "$@" | sh scripts/ci-scope.sh --files - 2>/dev/null)
    got_modules=$(printf '%s\n' "$out" | sed -n 's/^modules=//p')
    got_frontend=$(printf '%s\n' "$out" | sed -n 's/^frontend=//p')
    if [ "$got_modules" != "$want_modules" ] || [ "$got_frontend" != "$want_frontend" ]; then
        echo "FAIL: $name"
        echo "      want modules=[$want_modules] frontend=$want_frontend"
        echo "      got  modules=[$got_modules] frontend=$got_frontend"
        fail=1
    fi
}
# 只断言「包含」——common/v1 谁都 import，具体是哪一堆会随代码变。
check_contains() {
    name=$1; want=$2; shift 2
    out=$(printf '%s\n' "$@" | sh scripts/ci-scope.sh --files - 2>/dev/null)
    got_modules=$(printf '%s\n' "$out" | sed -n 's/^modules=//p')
    case " $got_modules " in
        *" $want "*) ;;
        *) echo "FAIL: $name — modules=[$got_modules] should contain $want"; fail=1 ;;
    esac
}

# 最常见的一种：只改前端。一个 Go 模块都不该跑。
check "frontend only" "none" 1 \
    frontend/src/pages/EmailsPage.vue frontend/src/locales/zh.ts

# 只改一个服务：只跑它。
check "one service" "services/mail" 0 \
    services/mail/internal/app/draft.go services/mail/db/queries/mailbox.sql

# 迁移也算那个服务的（它的迁移测试在那个模块里）。
check "one service's migration" "services/mail" 0 \
    services/mail/db/migrations/00064_inbound_answered.sql

# 前端 + 一个服务：两边都跑，别的服务不跑。
check "frontend and one service" "services/mail" 1 \
    frontend/src/components/MailList.vue services/mail/internal/app/inboxread.go

# 一个服务的 proto 变了：gen、这个服务、以及所有 import 了它的模块（网关一定在）。
check "one service's proto" "gen services/gateway services/mail" 0 \
    proto/erp/mail/v1/email.proto gen/go/erp/mail/v1/email.pb.go

# 谁都依赖的：全跑。
check "pkg" "all" 1 pkg/apierr/errors.go
check "Makefile" "all" 1 Makefile
check "a guard script" "all" 1 scripts/check-mail-sandbox.sh
check "the ci workflow itself" "all" 1 .github/workflows/ci.yml
check "gen module files" "all" 1 gen/go.sum

# 认不出的：什么测试都不跑。守卫脚本不在这个范围里，它们永远跑。
check "docs only" "none" 0 docs/requirements/x.md README.md
check "deploy compose" "none" 0 deploy/docker-compose.prod.yml

# 别的 workflow 不影响 ci。
check "build workflow" "none" 0 .github/workflows/build.yml

# 空 diff：认不出就全跑。
check "empty diff" "all" 1 ""

# common/v1 谁都 import：至少 gen 和 mail 得在里面。
check_contains "common proto reaches mail" "services/mail" proto/erp/common/v1/common.proto
check_contains "common proto reaches gen" "gen" proto/erp/common/v1/common.proto

if [ "$fail" = 1 ]; then
    echo "ci-scope tests failed" >&2
    exit 1
fi
echo "ci-scope tests passed"
