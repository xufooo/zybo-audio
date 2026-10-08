#!/bin/sh
# SPDX-License-Identifier: GPL-2.0-only

set -e

TARGET="${1:?usage: customize04-volume.sh <rootfs dir>}"

DROPIN="$TARGET/etc/systemd/system/bluealsa-aplay.service.d"
mkdir -p "$DROPIN"
cat > "$DROPIN/10-volume.conf" <<'EOF'

[Service]
ExecStart=
ExecStart=/usr/bin/bluealsa-aplay -S --volume=software
EOF

fail() { echo "[hook] ERROR: volume separation invariant broken: $1" >&2; exit 1; }

SH="$TARGET/etc/shairport-sync.conf"
[ -f "$SH" ] || fail "missing $SH"
if grep -qE '^[[:space:]]*mixer_control_name[[:space:]]*=' "$SH"; then
    fail 'shairport-sync drives the codec mixer: the sender would overwrite the board volume and re-write it on every poll (pops)'
fi
grep -qE '^[[:space:]]*volume_max_db[[:space:]]*=[[:space:]]*0(\.0)?[[:space:]]*;' "$SH" \
    || fail 'shairport-sync general.volume_max_db is not 0.0'
grep -qE '^[[:space:]]*volume_range_db[[:space:]]*=[[:space:]]*60[[:space:]]*;' "$SH" \
    || fail 'shairport-sync general.volume_range_db is not 60'

grep -qE '^[[:space:]]*ignore_volume_control[[:space:]]*=[[:space:]]*"yes"[[:space:]]*;' "$SH" \
    || fail 'shairport-sync general.ignore_volume_control is not "yes" (AirPlay would be ~64 dB quieter than the other sources)'

MPD="$TARGET/etc/mpd.conf"
[ -f "$MPD" ] || fail "missing $MPD"
bad="$(grep -cE '^[[:space:]]*mixer_type[[:space:]]+"software"' "$MPD" || true)"
[ "$bad" -eq 0 ] || fail "mpd.conf still has mixer_type \"software\" ($bad occurrence(s)); mpd would keep its own volume"

grep -q -- '--volume=software' "$DROPIN/10-volume.conf" \
    || fail 'bluealsa-aplay does not use --volume=software: with --volume=mixer (or =auto on a native-mode PCM) bluealsa-aplay would operate the ALSA Master control, so the phone would drive the board volume'

echo "[hook] volumes are separate: board = ALSA Master (WebUI only), sources = their own"
echo "  shairport-sync : software volume (mixer_control_name absent)"
echo "  shairport-sync : $(grep -hE '^[[:space:]]*volume_max_db' "$SH" | sed 's/^[[:space:]]*//')"
echo "  shairport-sync : $(grep -hE '^[[:space:]]*volume_range_db' "$SH" | sed 's/^[[:space:]]*//')"
echo "  mpd            : $(grep -hE '^[[:space:]]*mixer_type' "$MPD" | head -1 | sed 's/^[[:space:]]*//')"
echo "  bluealsa-aplay : $(grep -h '^ExecStart=' "$DROPIN/10-volume.conf")"
