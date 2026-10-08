#!/bin/sh
# SPDX-License-Identifier: GPL-2.0-only

set -e

TARGET="${1:?usage: customize02-app.sh <rootfs dir>}"
FILES="${ZYBO_FILES:-}"

if [ -z "$FILES" ] || [ ! -f "$FILES/zybo-audio-web" ]; then
    echo "[hook] WARN: ZYBO_FILES/zybo-audio-web not provided -- the image will have no web panel"
    echo "[hook]       stage it first with tools/stage_release.sh in the source tree"
    exit 0
fi

VER="$(cat "$FILES/VERSION" 2>/dev/null || echo unknown)"
echo "[hook] installing ZYBO Audio panel v$VER"

install -D -m 755 "$FILES/zybo-audio-web"            "$TARGET/usr/local/bin/zybo-audio-web"
install -D -m 644 "$FILES/webui/index.html"          "$TARGET/var/www/zybo-audio/index.html"
if [ -f "$FILES/webui/favicon.svg" ]; then
    install -D -m 644 "$FILES/webui/favicon.svg"     "$TARGET/var/www/zybo-audio/assets/favicon.svg"
fi

install -D -m 644 "$FILES/zybo-audio-web.service"    "$TARGET/usr/lib/systemd/system/zybo-audio-web.service"

WANT="$TARGET/etc/systemd/system/multi-user.target.wants"
mkdir -p "$WANT"
ln -sf "/usr/lib/systemd/system/zybo-audio-web.service" "$WANT/zybo-audio-web.service"
echo "[hook] enabled zybo-audio-web.service"

DOC="$TARGET/usr/share/doc/zybo-audio"
install -D -m 644 "$FILES/VERSION"                   "$DOC/VERSION"
if [ -f "$FILES/THIRD-PARTY.md" ]; then
    install -D -m 644 "$FILES/THIRD-PARTY.md"        "$DOC/THIRD-PARTY.md"
fi
if [ -d "$FILES/licenses" ]; then
    install -d "$DOC/licenses"
    cp -a "$FILES/licenses/." "$DOC/licenses/"
    echo "[hook] installed third-party license texts with the image ($(ls "$FILES/licenses" | wc -l) files)"
fi

echo "[hook] done: zybo-audio-web v$VER + WebUI + licenses"
