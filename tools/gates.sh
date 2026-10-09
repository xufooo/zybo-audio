#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-2.0-only
set -euo pipefail
cd "$(dirname "$0")/.."
fail=0
note() { printf '%s\n' "$*"; }
bad() { fail=1; note "FAIL: $*"; }

if grep -rIlP '[\x{4e00}-\x{9fff}\x{3040}-\x{30ff}]' . --exclude-dir=.git --exclude-dir=node_modules --exclude=go.sum --exclude=package-lock.json >/tmp/zybo-cjk.txt 2>/dev/null; then
  bad "non-English text in $(wc -l </tmp/zybo-cjk.txt) file(s):"
  sed 's/^/  /' /tmp/zybo-cjk.txt
else
  note "ok: no CJK text"
fi

missing=0
while IFS= read -r f; do
  head -6 "$f" | grep -q 'SPDX-License-Identifier: GPL-2.0-only' || { note "  no SPDX header: $f"; missing=1; }
done < <(git ls-files '*.go' '*.sh' '*.py' '*.tcl' '*.xdc' '*.dts' '*.fragment' '*.service' | grep -v '^tools/')
[ "$missing" -eq 0 ] && note "ok: SPDX headers"
[ "$missing" -eq 1 ] && fail=1

if git ls-files | grep -qiE 'loongfx|cyfan|BY-SA|by-sa'; then
  bad "copyright-blocked preset content present (LoongFX/CC BY-SA)"
else
  note "ok: no blocked preset content"
fi

if git ls-files | grep -qiE '\.(irs|vdc)$'; then
  bad "user files committed (*.irs/*.vdc are user data, never enter git)"
else
  note "ok: no user IR/VDC files"
fi

if git grep -n -I -E "/home/ooo|~/Works|192\.168\.|BEGIN .*PRIVATE|api[_-]?key|password|ssid" -- . ':!pins.env' ':!tools/gates.sh' >/dev/null; then
  bad "local paths or secrets present:"
  git grep -n -I -E "/home/ooo|~/Works|192\.168\.|BEGIN .*PRIVATE|api[_-]?key|password|ssid" -- . ':!pins.env' ':!tools/gates.sh' | head -5
else
  note "ok: no local paths/secrets"
fi

if git ls-files | grep -qiE '\.(bit|xsa|bin|img|ext4|tar\.zst)$'; then
  bad "build output is committed (.bit/.xsa/.bin/.img)"
else
  note "ok: no build output committed"
fi

while IFS= read -r f; do bash -n "$f" || bad "shell syntax: $f"; done < <(git ls-files '*.sh' | grep -v '^tools/')
note "ok: shell syntax"

while IFS= read -r f; do python3 -m py_compile "$f" || bad "python syntax: $f"; done < <(git ls-files '*.py' | grep -v '^tools/')
rm -rf __pycache__ tools/__pycache__
note "ok: python syntax"

tagok=1
for tag in v0.3.0 v0.4.0; do
  git rev-parse -q --verify "refs/tags/$tag" >/dev/null || { bad "missing tag $tag"; tagok=0; }
done
[ "$tagok" -eq 1 ] && note "ok: tags present"

exit "$fail"
