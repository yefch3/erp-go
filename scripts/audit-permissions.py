#!/usr/bin/env python3
"""Fail CI when the permission catalogue and its consumers drift apart.

The ERP has three independent-looking layers that must name the same
capability: IAM migrations define it, the gateway enforces it, and the
frontend uses it to reveal navigation/actions.  A typo or a newly added code
in only one layer otherwise ships as a page or button that silently vanishes.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
CODE = r"[a-z][a-z0-9_-]*:[a-z][a-z0-9_-]*:[a-z][a-z0-9_-]*"


def text(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def permission_catalogue() -> set[str]:
    found: set[str] = set()
    for path in sorted((ROOT / "services/iam/db/migrations").glob("*.sql")):
        up = text(path).split("-- +goose Down", 1)[0]
        for statement in re.findall(
            r"INSERT\s+INTO\s+permissions\b.*?;", up, re.I | re.S
        ):
            found.update(re.findall(rf"['\"]({CODE})['\"]", statement))
        # Permission renames are catalogue changes too (notification -> mail).
        for new, old in re.findall(
            rf"UPDATE\s+permissions\s+SET\s+code\s*=\s*['\"]({CODE})['\"]\s+WHERE\s+code\s*=\s*['\"]({CODE})['\"]",
            up,
            re.I,
        ):
            found.discard(old)
            found.add(new)
        # Prefix renames such as notification:* -> mail:* keep the suffix.
        for new_prefix, old_prefix in re.findall(
            r"SET\s+code\s*=\s*['\"]([a-z][a-z0-9_-]*:)['\"]\s*\|\|.*?WHERE\s+code\s+LIKE\s+['\"]([a-z][a-z0-9_-]*:)%['\"]",
            up,
            re.I | re.S,
        ):
            renamed = {new_prefix + code[len(old_prefix):] for code in found if code.startswith(old_prefix)}
            found = {code for code in found if not code.startswith(old_prefix)}
            found.update(renamed)
        for statement in re.findall(
            r"DELETE\s+FROM\s+permissions\b.*?;", up, re.I | re.S
        ):
            for code in re.findall(rf"['\"]({CODE})['\"]", statement):
                found.discard(code)
    return found


def gateway_references() -> set[str]:
    found: set[str] = set()
    for path in (ROOT / "services/gateway/internal/httpapi").glob("*.go"):
        found.update(re.findall(rf's\.perm\("({CODE})"\)', text(path)))
    return found


def frontend_references() -> set[str]:
    found: set[str] = set()
    for path in (ROOT / "frontend/src").rglob("*"):
        if path.suffix not in {".ts", ".vue"}:
            continue
        source = text(path)
        found.update(
            re.findall(rf"auth\.can\(\s*['\"]({CODE})['\"]\s*\)", source)
        )
        found.update(
            re.findall(rf"permission\s*:\s*['\"]({CODE})['\"]", source)
        )
    return found


def preset_references() -> set[str]:
    source = text(ROOT / "services/iam/internal/app/presetroles.go")
    found: set[str] = set()
    for block in re.findall(r"Permissions:\s*\[\]string\s*\{(.*?)\}", source, re.S):
        found.update(re.findall(rf'"({CODE})"', block))
    return found


def dependency_references() -> set[str]:
    source = text(ROOT / "services/iam/internal/app/permission_dependencies.go")
    return set(re.findall(rf'"({CODE})"', source))


def dependency_actions() -> set[str]:
    source = text(ROOT / "services/iam/internal/app/permission_dependencies.go")
    return set(re.findall(rf'^\s*"({CODE})"\s*:', source, re.M))


def main() -> int:
    catalogue = permission_catalogue()
    consumers = {
        "gateway": gateway_references(),
        "frontend": frontend_references(),
        "preset roles": preset_references(),
        "dependency rules": dependency_references(),
    }
    errors: list[str] = []
    if not catalogue:
        errors.append("permission catalogue is empty; migration parser no longer matches")
    for name, references in consumers.items():
        unknown = sorted(references - catalogue)
        if unknown:
            errors.append(f"{name} references unknown permissions: {', '.join(unknown)}")

    # Every operation must deliberately say which readable surface leads to
    # it. These two are complete standalone workflows on the unguarded Todos
    # page; all other non-read capabilities need an explicit dependency.
    standalone_actions = {
        "approval:task:act",
        "mail:export:audit",
        "procurement:reimbursement:manage",
    }
    action_codes = {code for code in catalogue if not code.endswith(":read")}
    missing_dependencies = sorted(action_codes - dependency_actions() - standalone_actions)
    if missing_dependencies:
        errors.append(
            "operation permissions without an explicit page dependency: "
            + ", ".join(missing_dependencies)
        )

    # A catalogue entry may intentionally be service-only (approval task
    # ownership is checked inside approval, for example), so unused entries
    # are reported for review but do not fail the build.
    all_references = set().union(*consumers.values())
    unreferenced = sorted(catalogue - all_references)
    print(
        "permission audit: "
        f"catalogue={len(catalogue)} gateway={len(consumers['gateway'])} "
        f"frontend={len(consumers['frontend'])} presets={len(consumers['preset roles'])}"
    )
    if unreferenced:
        print("permission audit: service-only/unreferenced: " + ", ".join(unreferenced))
    if errors:
        for error in errors:
            print("permission audit: ERROR: " + error, file=sys.stderr)
        return 1
    print("permission audit: catalogue and consumers agree")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
