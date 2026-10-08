# Operations

## Two volumes (read this first)

There are two volume knobs, and they do different jobs:

- **Panel volume** (`/api/volume`, ALSA Master): the loudness you hear.
  This is the only volume the backend ever writes.
- **Phone volume** (AirPlay stream volume, 30 dB software range): how much
  signal the phone sends. **Keep the phone at maximum** and control loudness
  from the panel only. A phone at half volume costs ~15 dB before the board
  can do anything about it.

## Bypass

`POST /api/dsp/bypass {"bypass": true}` passes audio straight through
(effects off). It persists across restarts (whatever it was, it stays).
Hardware boots bypassed (safe default: zero coefficients can never mute);
the service restores your setting about a minute after power-on, so the
first minute sounds louder (direct) — that is the handover, not drift.

## Backup / restore

- `GET /api/dsp/backup` downloads everything (types, state, presets, IR, DDC).
- `POST /api/dsp/restore` with that file: validates everything first
  (a bad file changes nothing), then writes and re-applies.
- The panel settings area has Backup/Restore buttons (restore asks first:
  it overwrites custom cards, presets and chain settings).
- Bulk storage lives on the rootfs (`/var/lib/zybo-audio/`) — reflashing
  wipes it. Only an exported backup survives. Export after any change
  you care about.

## Restart behavior

Everything persists: chain, volume, bypass, limiter, IR/DDC selections
(IR/DDC files are re-read by name). `Restart=always` on the service.

## What the engine reports (capabilities, not guesses)

Software never assumes what the bitstream can do — it reads `CAP0..CAP4`
(magic `0x5A44`). A chain the hardware cannot run is refused with a 400
(machine-readable `code`/`what` fields), never half-applied. See
`zybo-dsp/CONTRACT.md`.
