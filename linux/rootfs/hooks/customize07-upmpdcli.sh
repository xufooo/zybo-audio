# SPDX-License-Identifier: GPL-2.0-only
#!/bin/sh
# ============================================================================
# customize07-upmpdcli.sh - DLNA renderer (upmpdcli) into the image (0.6 C1)
# ============================================================================
# upmpdcli is not in Debian main; it comes from the upstream repository
# (https://www.lesbonscomptes.com/upmpdcli, Raspbian/Debian trixie ARM).
# This hook adds the repo (key fingerprint verified below), installs upmpdcli,
# then disables the service: the backend owns source lifecycles and starts it
# on demand (selecting the DLNA source).
#
# Board-proven 2026-10-13: upmpdcli 1.9.17 armhf + config
#   friendlyname "ZYBO Audio", openhome=0, upnpav=1,
#   avfriendlyname "ZYBO Audio", upnpiface=<wifi iface>, mpdhost localhost.
# The .conf itself is board-managed for now (first-boot story at 1.0);
# this hook only guarantees the package + a stopped, disabled service.
#
# NOTE: unverifiable without a full image build (frozen until 1.0 by project
# rule). Syntax-checked (bash -n) and reviewed against the board transcript;
# first image build must watch this step.
# ============================================================================
set -e

TARGET="${1:?usage: customize07-upmpdcli.sh <rootfs dir>}"

KEYFP="F8E3347256922A8AE767605B7808CE96D38B9201"
KEYURL="https://www.lesbonscomptes.com/pages/lesbonscomptes.gpg"

echo "[hook] fetching upmpdcli repository key..."
curl -fsSL --max-time 60 "$KEYURL" -o /tmp/lesbonscomptes.gpg
GOTFP="$(gpg --show-keys --with-colons /tmp/lesbonscomptes.gpg 2>/dev/null | awk -F: '/^fpr:/ {print $10; exit}')"
if [ "$GOTFP" != "$KEYFP" ]; then
    echo "[hook] FATAL: repository key fingerprint mismatch (got $GOTFP)" >&2
    exit 1
fi
install -D -m 644 /tmp/lesbonscomptes.gpg "$TARGET/usr/share/keyrings/lesbonscomptes.gpg"
rm -f /tmp/lesbonscomptes.gpg

cat > "$TARGET/etc/apt/sources.list.d/upmpdcli-rtrixie.sources" <<'EOF'
Types: deb
URIs: https://www.lesbonscomptes.com/upmpdcli/downloads/raspbian/
Suites: trixie
Components: main
Signed-By: /usr/share/keyrings/lesbonscomptes.gpg
EOF

# Pin: only upmpdcli-family may come from here (their repo also carries mpd;
# the board MPD must never be replaced by it).
cat > "$TARGET/etc/apt/preferences.d/lesbons" <<'EOF'
Package: *
Pin: release o=lesbonscomptes
Pin-Priority: 100

Package: upmpdcli libnpupnp* libupnpp* sc2mpd
Pin: release o=lesbonscomptes
Pin-Priority: 500
EOF

echo "[hook] installing upmpdcli inside target..."
chroot "$TARGET" apt-get update -o Dir::Etc::sourcelist="sources.list.d/upmpdcli-rtrixie.sources" \
    -o Dir::Etc::sourceparts="-" -o APT::Get::List-Cleanup="0"
chroot "$TARGET" apt-get install -y --no-install-recommends upmpdcli
chroot "$TARGET" systemctl disable upmpdcli.service || true
echo "[hook] done: upmpdcli installed, service left disabled (backend-managed)"
