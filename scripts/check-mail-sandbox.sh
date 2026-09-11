#!/bin/sh
# CI guard: a received mail's HTML may only be rendered inside MailBody.vue.
#
# It carries the sender's own stylesheet — kept on purpose, because that is
# most of how a mail looks like itself — and a stylesheet is only safe inside
# the sandboxed frame MailBody sets up. Injected into one of our own pages it
# is a stranger writing CSS for the ERP: `body{display:none}` hides the
# application, `position:fixed` covers the toolbar, and a selector naming a
# framework class restyles our controls.
#
# This guard exists because that is not hypothetical. The frame was added and
# two v-html sites in EmailsPage were missed, so opening a newsletter washed
# out the ERP's own chrome with the sender's dark theme. One place converted,
# two forgotten, and nothing said so.
#
# It happened a second time (2026-09-10) and this guard watched it go by: the
# reply quote box is filled with `el.innerHTML = quoted.value` from a script
# block, and the check below only knew about v-html. A Cloudflare newsletter's
# `p,div,h1,h2{color:#fff!important}` landed in the compose dialog and painted
# every div's text white — the From box looked empty and typing in the body
# produced nothing visible. So the second check exists: assigning to innerHTML
# anywhere in the frontend has to say out loud why it is safe.
set -eu

BAD=$(grep -rn 'v-html' frontend/src --include='*.vue' \
      | grep -viE 'MailBody\.vue' \
      | grep -iE 'v-html="[^"]*(body|bodyHtml|html)\b' \
      || true)

if [ -n "$BAD" ]; then
  echo "mail body rendered outside the sandbox:"
  echo "$BAD"
  echo
  echo "Use <MailBody :html=\"...\" />. A received mail carries the sender's"
  echo "stylesheet, which must not reach our own document."
  exit 1
fi

# The other door into the live document. Deliberately not a heuristic on what
# is being assigned — the first version of this bug read `el.innerHTML =
# quoted.value`, and any pattern narrow enough to spare the legitimate writers
# is one variable rename away from missing it again. So: every write is
# flagged, and the few real ones carry `mail-sandbox-ok:` and a reason.
HITS=$(mktemp)
UNMARKED=$(mktemp)
trap 'rm -f "$HITS" "$UNMARKED"' EXIT

grep -rn '\.innerHTML[[:space:]]*=[^=]' frontend/src \
     --include='*.vue' --include='*.ts' \
     | grep -v '/MailBody\.vue:' \
     > "$HITS" || true

# The reason goes in the comment above the write, not crammed onto it — one
# line is never enough room to explain why a mail cannot reach this spot.
# So look back a few lines for the marker.
while IFS= read -r hit; do
  [ -n "$hit" ] || continue
  file=${hit%%:*}
  rest=${hit#*:}
  line=${rest%%:*}
  from=$((line - 8))
  [ "$from" -lt 1 ] && from=1
  sed -n "${from},${line}p" "$file" | grep -q 'mail-sandbox-ok' || echo "$hit" >> "$UNMARKED"
done < "$HITS"

RAW=$(cat "$UNMARKED")
if [ -n "$RAW" ]; then
  echo "innerHTML written outside the sandbox:"
  echo "$RAW"
  echo
  echo "A received mail's HTML carries the sender's stylesheet and must go"
  echo "through <MailBody> (a sandboxed frame) or stripStylesheets()."
  echo "If this write cannot carry a mail, say so just above it:"
  echo "  // mail-sandbox-ok: <why this can never hold a received mail>"
  exit 1
fi

echo "mail sandbox check passed"
