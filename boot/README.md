# boot/ — how the bootable image is assembled

This directory holds a **built** `BOOT.BIN`, not sources. Every input is
pinned in `../pins.env` so anyone can reproduce it.

## What BOOT.BIN is

```
BOOT.BIN = FSBL + zybo_audio.bit + U-Boot
```

| Part | Origin | License note |
|---|---|---|
| FSBL | Vivado 2024.1 `embeddedsw` (`lib/sw_apps/zynq_fsbl`), compiled with the `.xsa` below | Xilinx MIT (+ `md5.c` Eric Young attribution, see THIRD-PARTY.md) |
| `system.bit` | `zybo-dsp` tag `v0.3.0`, built with Vivado 2024.1 (`scripts/build_retry.sh`), hash in `SHA256SUMS` | GPL-2.0 (own RTL) + Xilinx EULA (Xilinx IP, runs only on Xilinx silicon) |
| U-Boot | upstream `v2024.01` stock binary for ZYBO | GPL-2.0+ (sources: upstream tag) |

## Reproduce it

```bash
# 1. bitstream (needs Vivado 2024.1, batch mode)
git clone <zybo-dsp> && cd zybo-dsp && git checkout v0.3.0
bash scripts/fetch_ip.sh
bash scripts/build_retry.sh 3 -tclargs -force
# → build/fpga/zybo_audio/zybo_audio.{bit,xsa}

# 2. FSBL (needs the .xsa above)
# 3. pack it (bootgen ships with Vivado):
bootgen -image boot.bif -o BOOT.BIN
# boot.bif:
#   the_ROM_image: { [bootloader]fsbl.elf, system.bit, u-boot.elf }
```

Compare your output against `SHA256SUMS`. Bit-identical output is **not**
expected across Vivado runs (timestamps); what must match is the
functionality gate: boot it, read `CAP0..CAP4` (see `zybo-dsp/CONTRACT.md`),
`CAP1` bit7 must read 0.

## If the hashes don't match

1. Check `pins.env` — a moved upstream pin is the usual cause.
2. Rebuild the bitstream and compare synthesis utilization
   (reference: 14901 LUT / 8032 FF / 50 BRAM / 47 DSP for the audio IP).
3. Never ship a BOOT.BIN whose CAPs you haven't read back.
