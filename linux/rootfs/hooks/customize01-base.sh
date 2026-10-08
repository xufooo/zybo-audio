#!/bin/sh
# SPDX-License-Identifier: GPL-2.0-only

set -e

TARGET="${1:?usage: mmdebstrap hook requires the chroot directory as \$1}"

mkdir -p "$TARGET/etc"
cat > "$TARGET/etc/fstab" <<'EOF'

/dev/mmcblk0p2   /              ext4       defaults,noatime      0      1
proc             /proc          proc       defaults              0      0
sysfs            /sys           sysfs      defaults              0      0
devtmpfs         /dev           devtmpfs   mode=0755,nosuid      0      0
tmpfs            /tmp           tmpfs      defaults,nosuid,nodev 0      0
EOF

echo zybo-audio > "$TARGET/etc/hostname"
cat > "$TARGET/etc/hosts" <<'EOF'
127.0.0.1   localhost
127.0.1.1   zybo-audio
::1         localhost ip6-localhost ip6-loopback
EOF

install -d "$TARGET/etc/systemd/network"

cat > "$TARGET/etc/systemd/network/10-eth0.link" <<'EOF'
[Match]
Type=ether

[Link]
Name=eth0
EOF

cat > "$TARGET/etc/systemd/network/20-wired.network" <<'EOF'
[Match]
Name=eth0 en* eth*

[Network]
DHCP=yes
IPv6AcceptRA=yes
EOF

cat > "$TARGET/etc/systemd/network/30-wireless.network" <<'EOF'
[Match]
Name=wl*

[Network]
DHCP=yes
IPv6AcceptRA=yes
EOF

install -d "$TARGET/etc/wpa_supplicant"
cat > "$TARGET/etc/wpa_supplicant/wpa_supplicant.conf" <<'EOF'

ctrl_interface=DIR=/run/wpa_supplicant GROUP=netdev
update_config=1
country=CN
EOF
chmod 600 "$TARGET/etc/wpa_supplicant/wpa_supplicant.conf"

install -d "$TARGET/etc/systemd/system/wpa_supplicant@.service.d"
cat > "$TARGET/etc/systemd/system/wpa_supplicant@.service.d/10-generic-conf.conf" <<'EOF'
[Service]
ExecStart=
ExecStart=/sbin/wpa_supplicant -c/etc/wpa_supplicant/wpa_supplicant.conf -i%I
EOF

cat > "$TARGET/etc/udev/rules.d/70-wifi-autoconf.rules" <<'EOF'

SUBSYSTEM=="net", ACTION=="add",  KERNEL=="wl*", TAG+="systemd", ENV{SYSTEMD_WANTS}+="wpa_supplicant@%k.service"

SUBSYSTEM=="net", ACTION=="move", KERNEL=="wl*", TAG+="systemd", ENV{SYSTEMD_WANTS}+="wpa_supplicant@%k.service"
EOF

ln -sf /usr/share/zoneinfo/Asia/Shanghai "$TARGET/etc/localtime"
echo "Asia/Shanghai" > "$TARGET/etc/timezone"

echo 'LANG=C.UTF-8' > "$TARGET/etc/locale.conf"

install -d "$TARGET/etc/systemd/journald.conf.d"
cat > "$TARGET/etc/systemd/journald.conf.d/10-zybo.conf" <<'EOF'
[Journal]
Storage=persistent
SystemMaxUse=128M
RuntimeMaxUse=32M
EOF

cat > "$TARGET/etc/issue" <<'EOF'
ZYBO Audio DSP (Debian armhf) \n \l
EOF
cat > "$TARGET/etc/motd" <<'EOF'
 ZYBO Rev B audio player - Debian armhf

  sound card  aplay -l             -> card 0: ZyboSoundCard (48k / S32_LE)
  volume      alsamixer -c 0       restored at boot from /var/lib/alsa/asound.state
  playback    mpc add/play/status  (music library in /var/lib/mpd/music)
  streaming   shairport-sync is running; pick zybo-audio from AirPlay
  debug       journalctl -u mpd -f
EOF

cat > "$TARGET/etc/resolv.conf" <<'EOF'
nameserver 223.5.5.5
nameserver 1.1.1.1
EOF

cat > "$TARGET/etc/asound.conf" <<'EOF'
defaults.pcm.rate_converter "samplerate"

pcm.!default {
    type plug
    slave {
        pcm "hw:0,0"
        rate 48000
        format S32_LE
        channels 2
    }
}

ctl.!default {
    type hw
    card 0
}
EOF

if [ -n "${ZYBO_FILES:-}" ] && [ -f "$ZYBO_FILES/asound.state" ]; then
    install -D -m 644 "$ZYBO_FILES/asound.state" "$TARGET/var/lib/alsa/asound.state"
    echo "[hook] installed asound.state (initial Master volume)"
else
    echo "[hook] WARN: ZYBO_FILES/asound.state not provided; boot volume will use the driver default"
fi

ROOT_PW="${ZYBO_ROOT_PW:-zybo}"
chroot "$TARGET" chpasswd <<EOF
root:${ROOT_PW}
EOF
if [ "${ZYBO_ROOT_PW:-}" = "" ]; then
    echo "[hook] WARN: root login uses the default 'zybo' (override with ZYBO_ROOT_PW)"
fi

rm -f "$TARGET"/etc/ssh/ssh_host_*
install -d "$TARGET/etc/systemd/system/ssh.service.d"
cat > "$TARGET/etc/systemd/system/ssh.service.d/10-generate-host-keys.conf" <<'EOF'

[Service]
ExecStartPre=/usr/bin/ssh-keygen -A
EOF
echo "[hook] SSH host keys will be generated on first boot (not included in the image)"

cat > "$TARGET/etc/mpd.conf" <<'EOF'
music_directory     "/var/lib/mpd/music"
playlist_directory  "/var/lib/mpd/playlists"
db_file             "/var/lib/mpd/tag_cache"
log_file            "syslog"
pid_file            "/run/mpd/pid"
state_file          "/var/lib/mpd/state"
sticker_file        "/var/lib/mpd/sticker.sql"

user                "mpd"
bind_to_address     "any"
port                "6600"

mixer_type          "none"
volume_normalization "no"

audio_output {
    type        "alsa"
    name        "ZYBO (SSM2603 @48k)"
    device      "default"
    mixer_type  "none"
}
EOF
chroot "$TARGET" install -d -o mpd -g audio /var/lib/mpd/music /var/lib/mpd/playlists
chroot "$TARGET" usermod -aG audio mpd || true

WANT="$TARGET/etc/systemd/system/multi-user.target.wants"
mkdir -p "$WANT"
for u in ssh.service \
         systemd-networkd.service \
         systemd-timesyncd.service \
         avahi-daemon.service \
         shairport-sync.service \
         bluetooth.service \
         mpd.socket; do
    if [ -e "$TARGET/usr/lib/systemd/system/$u" ]; then
        ln -sf "/usr/lib/systemd/system/$u" "$WANT/$u"
        echo "[hook] enabled $u"
    else
        echo "[hook] WARN: unit not found, skipped: $u"
    fi
done
chroot "$TARGET" systemctl set-default multi-user.target 2>/dev/null || true

echo "[hook] base configuration applied to $TARGET"
