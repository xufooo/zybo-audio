# SPDX-License-Identifier: GPL-2.0-only
#!/bin/sh
# ============================================================================
# customize06-mounts.sh - NAS mount pieces into the image (0.6 B4a)
# ============================================================================
# Installs:
#   /usr/lib/systemd/system/mnt-nas.{mount,automount}  CIFS on-demand mount
#   /mnt/nas                                            mount point (empty dir)
#   /var/lib/mpd/music/nas -> /mnt/nas                  MPD library symlink
#   /etc/nas.credentials.example                        credential template
#                                                       (real credentials are
#                                                        written by first-boot
#                                                        setup, never imaged)
#
# The mount itself is NOT enabled (mounting without credentials only slows
# things down); the automount is (access without credentials fails cleanly).
# MPD follow_outside_symlinks lives in the mpd.conf of customize01-base.sh.
# NOTE: like customize02-app.sh, no chroot, only install/cp/ln - dry-runnable.
# ============================================================================
set -e

TARGET="${1:?usage: customize06-mounts.sh <rootfs dir>}"
FILES="${ZYBO_FILES:-}"

if [ -z "$FILES" ] || [ ! -f "$FILES/systemd/mnt-nas.mount" ]; then
    echo "[hook] WARN: mount units not staged -- skipping NAS mounts"
    exit 0
fi

install -D -m 644 "$FILES/systemd/mnt-nas.mount"     "$TARGET/usr/lib/systemd/system/mnt-nas.mount"
install -D -m 644 "$FILES/systemd/mnt-nas.automount" "$TARGET/usr/lib/systemd/system/mnt-nas.automount"
install -D -m 644 "$FILES/systemd/rclone-cloud.service" "$TARGET/usr/lib/systemd/system/rclone-cloud.service"
install -d "$TARGET/mnt/nas"
install -d "$TARGET/mnt/cloud"
mkdir -p "$TARGET/etc/rclone"
chmod 700 "$TARGET/etc/rclone"

WANT="$TARGET/etc/systemd/system/multi-user.target.wants"
mkdir -p "$WANT"
ln -sf "/usr/lib/systemd/system/mnt-nas.automount" "$WANT/mnt-nas.automount"
echo "[hook] enabled mnt-nas.automount (mount on first access)"

# Credential template: the real /etc/nas.credentials is written by first-boot
# setup (0600); the image carries only this sample.
cat > "$TARGET/etc/nas.credentials.example" <<'EOF'
# Copy to /etc/nas.credentials with mode 0600, then: systemctl restart mnt-nas.automount
username=guest-or-nas-user
# ...plus the secret key: see CREDENTIALS in mount.cifs(8)
EOF
chmod 644 "$TARGET/etc/nas.credentials.example"

# MPD library symlinks (dangling until mounted; MPD update skips them).
mkdir -p "$TARGET/var/lib/mpd/music"
ln -sf "/mnt/nas" "$TARGET/var/lib/mpd/music/nas"
ln -sf "/mnt/cloud" "$TARGET/var/lib/mpd/music/cloud"
echo "[hook] done: NAS mount units + MPD symlinks"
