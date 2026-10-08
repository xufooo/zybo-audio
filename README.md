# zybo-audio — software side of the ZYBO audio player

Turns a Digilent ZYBO Rev B into a network audio player with FPGA-accelerated
DSP: parametric EQ, convolution (IR), DDC headphone correction, dynamics and
a true-peak limiter, driven from a one-screen web panel in two layers
(effect cards + capabilities).

- Board: Digilent ZYBO Rev B · This repo is **software only**.
  The bitstream comes from the companion repo `zybo-dsp`, pinned by hash
  (see `boot/README.md` and `pins.env`).
- License: GPL-2.0-only (`LICENSE`). Nothing in this repo is vendored:
  upstreams are fetched at build time, pinned in `pins.env`.
- **Sources ship without comments.** Backend comments were stripped, not
  translated; usage is documented here, register-level facts in
  `zybo-dsp/CONTRACT.md`. (The generated one-file panel keeps its English
  comments; it is build output, not source.)
- The panel ships with **no built-in effect cards** — you create your own
  (save/copy/delete) or import one. Device profiles (Headphones / Small
  Speakers / Amplifier) are included as starting points.

## Layout

```
backend/        Go service (HTTP API + ALSA + /dev/mem driver), no comments
webui/          one-file panel (English, no preset cards)
linux/          kernel fragment + device tree + rootfs hooks + mixer state
tools/          gates, panel smoke test, webui-check deps
boot/           BOOT.BIN (built artifact + hashes + how it was assembled)
docs/           operations, troubleshooting, plan
pins.env        every upstream pinned: kernel, U-Boot, Debian, tools, bitstream
```

## Build the backend (native or ARM)

```bash
cd backend
go build -o zybo-audio-web .                        # native
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 \
  go build -trimpath -o zybo-audio-web .            # ZYBO (static, no build paths inside)
go test ./...                                       # self-checking, no hardware needed
```

`go vet` and `gofmt -l` (empty) are part of CI.

## Test the panel (no board needed)

```bash
npm ci --prefix tools/webui-check
NODE_PATH=tools/webui-check/node_modules node tools/webui_smoke.js webui/index.html 0
```

`0` = no built-in cards expected (release panel ships empty).

## From these sources to a bootable card

1. Backend binary (above) + panel + `linux/systemd/zybo-audio-web.service`.
2. `BOOT.BIN` — see `boot/README.md` (FSBL + bitstream + U-Boot, hashes in
   `boot/SHA256SUMS`).
3. Root filesystem — Debian via `linux/rootfs/hooks/` (mmdebstrap-style),
   or any armhf rootfs with ALSA + the files above.

## Versions

- `v0.3.0` — matches the `v0.3.0` bitstream: no second FIR
  (`CAP1` bit7 = 0, PBP cleanly refused), no preset cards.
