#!/bin/sh
# SPDX-License-Identifier: GPL-2.0-only

set -eu

TARGET="${1:?usage: customize05-wifi-powersave.sh <rootfs dir>}"

S="$TARGET/usr/local/sbin/wifi-powersave-off.sh"
U="$TARGET/etc/systemd/system/wifi-powersave-off.service"
W="$TARGET/etc/systemd/system/multi-user.target.wants"

mkdir -p "$(dirname "$S")" "$(dirname "$U")" "$W"

cat > "$S" <<'EOS'

IW=/usr/sbin/iw
[ -x "$IW" ] || IW=$(command -v iw 2>/dev/null || echo /usr/sbin/iw)

apply() {
    found=0
    for w in /sys/class/net/*/wireless; do
        [ -e "$w" ] || continue
        iface=$(basename "$(dirname "$w")")
        found=1
        "$IW" dev "$iface" set power_save off 2>/dev/null || return 1
    done
    [ "$found" = 1 ] || return 2   # no wireless interface yet
    return 0
}

n=0
while [ "$n" -lt 12 ]; do

    apply && break
    n=$((n + 1))
    sleep 3
done

for w in /sys/class/net/*/wireless; do
    [ -e "$w" ] || continue
    iface=$(basename "$(dirname "$w")")
    echo "wifi-powersave: $iface -> $("$IW" dev "$iface" get power_save 2>&1)"
done
exit 0
EOS
chmod 755 "$S"

cat > "$U" <<'EOU'
[Unit]
Description=Disable WiFi power save (keeps AirPlay uplink throughput usable)
Documentation=man:iw(8)

After=network.target systemd-networkd.service wpa_supplicant.service
Wants=network.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/local/sbin/wifi-powersave-off.sh

SuccessExitStatus=0 1 2

[Install]
WantedBy=multi-user.target
EOU
chmod 644 "$U"

ln -sf ../wifi-powersave-off.service "$W/wifi-powersave-off.service"

echo "customize05: installed $S + $U and linked it into multi-user.target.wants/" >&2
