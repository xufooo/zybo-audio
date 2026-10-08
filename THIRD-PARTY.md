# Third-party components (not shipped in this repo)

Everything below is fetched or provided by its owner at build time.
Nothing in this list is vendored here.

| Component | Owner / license | How it enters the build |
|---|---|---|
| linux-xlnx kernel | Xilinx, GPL-2.0-only | fetched at `LINUX_XLNX_TAG` (see `pins.env`); our only kernel inputs are `linux/config.fragment` + `linux/zybo-audio.dts` |
| U-Boot `v2024.01` | DENX, GPL-2.0+ | upstream tag, stock binary |
| Debian trixie packages | respective owners (see per-package `/usr/share/doc/*/copyright` in the image) | mmdebstrap at build time; image ships non-free firmware blobs (see NOTICE below) |
| shairport-sync 5.5.2 + nqptp 1.2.8 | Mike Brady et al., MIT (shairport) | cross-built by CI |
| Go toolchain + `gorilla/websocket` | Go Authors (BSD-3) / Gorilla (BSD-3) | fetched by `go build` |
| `zybo-dsp` bitstream inputs | see `zybo-dsp/THIRD-PARTY.md` | consumed as `.bit`/`.xsa` pinned by hash (see `boot/`) |
| FSBL `md5.c` (inside BOOT.BIN) | Eric Young (SSLeay-style, attribution + advertising clause) | compiled from Vivado embeddedsw; attribution kept in image docs |

NOTICE (non-free firmware): the rootfs ships `firmware-realtek/-mediatek/-atheros`
binary blobs (redistribution permitted, no reverse engineering). They are
required for the board's USB Wi-Fi; a fully-free image would drop Wi-Fi.
