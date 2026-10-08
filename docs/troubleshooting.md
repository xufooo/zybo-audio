# Troubleshooting

## No sound at all

1. `curl localhost:8080/api/status` — is `playing` true? Is `volume` > 0?
2. Bypass on? (`"bypass": true` passes through; with no source it is silence.)
3. AirPlay: is the phone actually connected (shairport-sync), phone volume up?
4. ALSA: `amixer -c 0 sget Master` — the backend owns this; anything else
   writing it fights the backend.

## Quiet after switching cards / bypass

Different cards have different mastering (EQ cuts/boosts, limiter load) —
loudness differences between cards are expected, not drift. Direct (bypass)
is always louder than limited-processed: the limiter ceiling is −1 dBFS.
If processed sounds too quiet overall, lower the limiter load (threshold,
preamp) rather than pushing phone volume.

## Clicks when switching

Slot tables commit on frame boundaries (dual-bank); a click means a commit
landed mid-frame or a coefficient set changed under a running stage.
Report with the chain JSON.

## Occasional AirPlay pops

Do **not** reboot. Capture shairport-sync logs first (the evidence dies
with the process), then report.

## Raspberry-style Wi-Fi drops (USB rtl8xxxu)

The USB Wi-Fi does not always self-heal: rebind it
(`echo '1-1' > /sys/bus/usb/drivers/usb/{unbind,bind}` as root).

## PBP refused / "not supported"

PBP (Pure Bass Plus) needs the second FIR (`CAP1` bit7). This bitstream
does not have it — the refusal is correct behavior, not a bug.
