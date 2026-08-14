# sointu-homage

A demo-sized generative-audio homage to [vsariola/sointu](https://github.com/vsariola/sointu)
(the 4k-intro softsynth govertone embeds). Homage, not reimplementation — one
Go file that keeps sointu's *shape*:

- **instruments are stacks of unit opcodes** run by a tiny per-sample VM
  (`opEnv`, `opOsc`, `opMul`, `opLpf`, `opSend`, `opOut`); patches are compact
  opcode+param tables, like sointu's byte-encoded patches
- **a tracker-style sequencer** — 16-row patterns, 4 tracks
  (kick / bass / lead / hats), 4 rows per beat
- **everything derives from tiny data, deterministically** — patterns are
  *generated* from a seeded PRNG over A-minor pentatonic against an
  A–F–C–G root cycle, so the seed *is* the track

```
go run . -play                     # live, endless, via oto
go run . -wav out.wav -dur 32      # deterministic offline render
go run . -play -seed 7 -bpm 96     # a different track
```

Same seed → byte-identical WAV; different seed → different tune. Output is
44.1 kHz 16-bit stereo with a cross-fed dotted-eighth delay bus (lead sends to
it) and a tanh soft-clip master.

Also serves as a phase-A datapoint for the glplat audio spike
(`glplat-sdl3-audio-spike-spec-2026-08-14.md`): the
Oto lane — `oto/v3` + purego, no cgo, no runtime dylib — initialized and
streamed a live `io.Reader` on this Mac with zero friction (2026-08-14).
