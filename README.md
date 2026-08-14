# sointu-homage

A demo-sized generative-audio homage to [vsariola/sointu](https://github.com/vsariola/sointu)
(the 4k-intro softsynth govertone embeds). Homage, not reimplementation — one
Go file that keeps sointu's *shape*:

- **instruments are stacks of unit opcodes** run by a tiny per-sample VM
  (`opEnv`, `opOsc`, `opMul`, `opLpf`, `opSend`, `opOut`); patches are compact
  opcode+param tables, like sointu's byte-encoded patches
- **a tracker-style sequencer** — 16-row patterns, 5 tracks
  (drone / fifth / pad / bells / air)
- **everything derives from tiny data, deterministically**

Register: roguelike ambience. `DeriveParams(seed, depth)` maps a level seed
and dungeon depth to a music-parameter struct (tempo, root, scale, per-track
gains/densities) — deeper is darker, slower, thicker (measured 2026-08-14:
depth 5 puts ~77% of spectral power below 100 Hz vs ~19% at depth 0). The
kaiju-elements trick: taste lives in hand-tuned option tables, the seed just
picks. `SetParameters` stages a new map and applies it at the next pattern
boundary — the same transactional contract the glplat audio surface sketch
proposes for `audio/set-parameters`
(`glplat-audio-surface-sketch-2026-08-14.md`).

```
go run . -play                        # live, endless, via oto
go run . -play -depth 5               # deeper: darker, slower, thicker
go run . -wav out.wav -depth 2        # deterministic offline render
```

Same seed+depth → byte-identical WAV. Output is 44.1 kHz 16-bit stereo with a
cross-fed delay bus (bells send to it) and a tanh soft-clip master.

Also the phase-A datapoint for the glplat audio spike
(`glplat-sdl3-audio-spike-spec-2026-08-14.md`, phase A decided for Oto): the
Oto lane — `oto/v3` + purego, no cgo, no runtime dylib — initialized and
streamed a live `io.Reader` on this Mac with zero friction (2026-08-14).
