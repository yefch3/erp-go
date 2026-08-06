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

echo "mail sandbox check passed"
