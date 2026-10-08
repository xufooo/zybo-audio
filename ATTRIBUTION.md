# Attribution

This release ships **no preset effect cards and no preset library**
(deliberate: user data and third-party tunings stay with their owners).
You create cards in the panel (save/copy/delete) or import them.

What remains attributed:

- Biquad math: RBJ Audio EQ Cookbook (public reference, re-implemented).
- Saturator / volume-ramp concepts: MIT (ideas only, re-implemented).
- Crossfeed default (700 Hz / 60): bs2b-style topology, JamesDSP install
  defaults as reference values (re-implemented).
- ViPER-style effect models (bass/clarity/exciter/surround): behavior
  re-implemented from public V4A-family sources; no upstream code is
  included. Parameters marked with their measured source in fixture/docs
  where relevant.
- Test fixtures under `tools/fixtures/` cite their upstream file and line
  for every borrowed number.
