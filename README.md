# undertone

A tiny generative-ambience engine — under(ground) + tone — and the glplat
audio spike's engine seed. Architecturally a demo-sized homage to
[vsariola/sointu](https://github.com/vsariola/sointu), the 4k-intro softsynth
govertone embeds: no sointu code here, just its shape, in one Go file:

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
go run . -play -depth 5 -tension 1    # danger: faster, pulse in, harmony sours
go run . -play -mood overworld -depth 3   # bright mirror (-depth -3 is sugar)
go run . -wav out.wav -depth 2        # deterministic offline render
```

Two moods, each a patch palette + a derivation over (level, tension):
`dungeon` (minor ladder, saw drone, fifth) and `overworld` (mixolydian →
major → lydian by elevation, sine bed, major third, lifted denser bells,
breeze instead of rumble). Tension is mood-independent — overworld
tension is brightness collapsing into the same tritone/minor-ninth dread.

Tension (0..1) is the second game-state axis: depth says *where you are*,
tension says *what's happening*. It raises tempo, fades in a low pulse
track, slides the second drone from fifth to tritone to minor ninth, and
thins the calm layers. `tension 0` is byte-identical to the pre-tension
output. On the web build: `?tension=0..1`.

Same seed+depth → byte-identical WAV. Output is 44.1 kHz 16-bit stereo with a
cross-fed delay bus (bells send to it) and a tanh soft-clip master.

Also the phase-A datapoint for the glplat audio spike
(`glplat-sdl3-audio-spike-spec-2026-08-14.md`, phase A decided for Oto): the
Oto lane — `oto/v3` + purego, no cgo, no runtime dylib — initialized and
streamed a live `io.Reader` on this Mac with zero friction (2026-08-14).

## Browser (Go wasm + oto-js)

```
GOOS=js GOARCH=wasm go build -o web/main.wasm .
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" web/
python3 -m http.server 8899 -d web   # any static server works: no SAB
open http://localhost:8899/?depth=3                # click to start (autoplay policy)
```

Same engine, no ebiten runner — verified headless 2026-08-14 (AudioContext
running at 44.1 kHz, clock advancing, checked by a headless browser probe).
This retires the spike-spec caveat that Oto's wasm lane was only proven
inside ebiten web games.
