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

The lead (bell) slot is a six-instrument library — bell, chime
(inharmonic partials), clang (ring-mod), kalimba, reed, and a
Karplus-Strong pluck — mood-weighted and picked by the seed, so two
seeds differ in instrument, not just notes. Override with `-lead name`
/ `?lead=name`.

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

## The let-go lane (`lghost/` + `lg/ambience.lg`)

The point of the whole spike was to prove out the lg↔Go audio boundary, and
this is it: `lghost` embeds let-go, registers the audio surface sketch's
proposed natives, and hands the engine to an lg script.

```
go run ./lghost -e '(do (ambience/score-mood! :dungeon 7 5 0.8) (audio/music-start))'
go run ./lghost -e '(do (ambience/score! 7 4 0.6) (audio/render-wav "out.wav" 20))'
```

The split follows the sketch: **lg is the control language, Go is the DSP
engine.** `lg/ambience.lg` owns the `(seed, depth, tension) -> parameters`
derivation and the hand-tuned option tables — the layer where a game's taste
lives, evaluated once per pattern. Everything below the map stays in Go,
because the unit VM runs at 44.1 kHz and lg has no business there; keeping it
out also keeps the boxed variadic call ABI (advisory #722) off the audio path.

Natives, exactly the sketch's surface plus one:

| native | |
|---|---|
| `audio/music-start`, `audio/music-stop` | playback |
| `audio/set-parameters` | one map, merged over current state, applied at the next pattern boundary |
| `audio/trigger` | one-shot sfx: a trigger map or vector of them (from `sfx/for-events`), fire-and-forget |
| `audio/render-wav` | **spike-only** — how determinism gets checked without a sound card |

The surface is write-only: no native reads audio state back, so console
noodling can't taint replay determinism. Registering it needed **no changes to
let-go** — `vm.NewNamespace`, `vm.MustBox` and `rt.RegisterNS` are all
exported, which is what makes this self-contained.

`lghost/parity_test.go` renders 144 (mood × seed × depth × tension) cases both
ways and compares parameters field-by-field plus a hash of the PCM. Two things
it caught, both worth knowing:

- **lg's `*` is overflow-checked** and raises on wraparound, so the seed-mixing
  multiplies need `unchecked-multiply`. lg also has no unsigned integer type,
  and the rng takes an *unsigned* remainder — `(mod s m)` on a negative int64
  silently picks the wrong option from a table. `ambience/umod` does it by
  splitting the state into its top 63 bits and its low bit.
- **Go was fusing `x*y + z` into FMA on arm64**, putting derived gains one ULP
  off the stepwise result — which lg, with separate multiply and add opcodes,
  cannot reproduce. That made the Go derivation architecture-dependent: the
  same seed rendered differently on arm64 and amd64. The derivation now writes
  those products through an explicit `float64(…)` conversion, which the Go spec
  forbids fusing across. This changed some renders by one ULP versus the
  `poc/undertone-go-2026-08-15` tag; it is a fix, not a regression. The
  per-sample VM and `buildPatterns` still contain unfused products and so are
  still arm64/amd64-divergent — cross-architecture determinism is not claimed.

## One-shot sfx (`engine/sfx.go` + `lg/sfx.lg`)

The procedural one-shot lane the decoupling assessment called Tier 3, scoped
to the three sounds that cover most of a roguelike turn: **footstep**,
**damage hit**, **rune pickup**. Procedural, not samples: near-zero payload,
free per-event variation, and the same synth VM as the score so one-shots
share its timbre space.

The split mirrors the music lane. `lg/sfx.lg` is the vocabulary — a pure map
from event descriptors (`{:kind :combat-hit :dist 3 :dir 0.5}`, i.e. *what
happened*) to engine trigger maps, with the spatial model (hyperbolic distance
attenuation, soft-edged pan) applied there. `engine/sfx.go` is the mechanics:
an 8-voice one-shot pool beside the sequencer (which never sees it — `step()`
is bounded by `nTracks`), per-voice pan applied outside the patch,
sample-accurate trigger delay, and a per-trigger `humanize` jitter of envelope
decays and filter cutoffs so a footstep every turn doesn't machine-gun.

Two design points worth keeping:

- **The pickup chime is pitched from the floor's own scale** via the same
  derivation the music uses, so it is in key with the score by construction —
  the trick a sample library can't do.
- **`audio/trigger` is fire-and-forget and write-only**, like the rest of the
  surface: with no synth running the triggers drop silently, and nothing
  upstream can learn whether a sound played. Descriptors name events, not
  sounds, so procedural-vs-samples stays swappable behind this seam.

`go run ./lghost -demo` walks the scripted arc with the one-shots riding the
same per-turn boundary as the parameters (`-dry` prints the trace, including
a per-turn cue tag, without sound). `engine/sfx_test.go` pins the lane's
contract — a gain-0 one-shot leaves the music render byte-identical, triggers
are deterministic per seed, voices die (the env sustain-stage trap), and the
lg-thread-vs-audio-callback locking survives `-race`; `lghost/sfx_lg_test.go`
pins the vocabulary (gain hierarchy, attenuation, pan direction, in-scale
pickup, salt determinism).

Also the phase-A datapoint for the glplat audio spike
(`glplat-sdl3-audio-spike-spec-2026-08-14.md`, phase A decided for Oto): the
Oto lane — `oto/v3` + purego, no cgo, no runtime dylib — initialized and
streamed a live `io.Reader` on this Mac with zero friction (2026-08-14).

## Browser (Go wasm + oto-js)

```
GOOS=js GOARCH=wasm go build -o web/main.wasm .
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" web/
python3 serve.py 8899        # LAN + no-cache dev server (any static server works: no SAB)
open http://localhost:8899/?depth=3                # click to start (autoplay policy)
```

Same engine, no ebiten runner — verified headless 2026-08-14 (AudioContext
running at 44.1 kHz, clock advancing, checked by a headless browser probe).
This retires the spike-spec caveat that Oto's wasm lane was only proven
inside ebiten web games.
