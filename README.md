# let-go-undertone

A small generative-ambience engine for games, with a binding for
[let-go](https://github.com/nooga/let-go). It is a demo-sized homage to
[vsariola/sointu](https://github.com/vsariola/sointu): there is no sointu code
here, only its shape. Instruments are stacks of unit opcodes run by a tiny
per-sample VM, a tracker-style sequencer drives them, and the whole track is
derived from a seed.

The module has three parts:

| Package | What it is |
|---|---|
| `engine` | The synth VM, the sequencer, a one-shot sfx voice pool, and the game-state → parameters derivation. Plain Go; imports nothing from let-go. |
| `letgo` | The let-go binding: an `audio` namespace that drives the engine from lg code, plus `letgo/lg/ambience.lg`, the same derivation written in lg. |
| `cmd/undertone` | A CLI that plays live, renders WAVs, and draws a four-band VU meter. |

`cmd/lghost` is a minimal lg runner with the binding linked in, for trying the
namespace without lgx.

## The engine

Music parameters come from game state:

- **mood**: `dungeon` (minor ladder, saw drone, fifth) or `overworld`
  (mixolydian → major → lydian with elevation, sine bed, lifted bells)
- **level**: depth darkens, slows and thickens; elevation brightens
- **tension** (0..1): raises tempo, fades in a low pulse, slides the second
  drone from fifth to tritone to minor ninth, and thins the calm layers
- **seed**: picks from hand-tuned option tables, including the lead
  instrument (bell, chime, clang, kalimba, reed, or a Karplus-Strong pluck)

`Synth.SetParameters` stages a new parameter set and applies it at the next
pattern boundary, so a change mid-playback is a seamless transition. Output is
44.1 kHz 16-bit stereo through a cross-fed delay bus and a tanh soft clip.

One-shots run beside the sequencer in an 8-voice pool that the sequencer never
sees, so a trigger cannot change the music's random streams. Patches:
`footstep`, `hit`, `thud`, `creak`, `cry`, `sink`, `lift`, `thunk`, `snap`,
`twang`, `pluck`. Triggers take a gain, a pan, a sample-accurate delay, and
either a MIDI note or a scale degree resolved against the current parameters,
so a pickup chime lands in the music's key.

### Determinism

The same parameters render byte-identical audio **on one CPU architecture**.
The derivation itself is pinned across architectures (its products go through
explicit `float64` conversions, so Go cannot fuse them into FMA on arm64). The
per-sample VM and the pattern builder still contain unfused products, so a
render on arm64 and one on amd64 can differ in the last bit. Compare renders on
the same architecture.

## The CLI

```
go install github.com/mparrett/let-go-undertone/cmd/undertone@latest

undertone -play                            # live, endless
undertone -play -depth 5 -tension 0.8      # deeper and more dangerous
undertone -play -mood overworld -depth 3   # -depth -3 is the same thing
undertone -play -vu compact                # meters: bars | compact | tall
undertone -wav out.wav -depth 4 -lead pluck -dur 30
```

## The let-go binding

Importing `github.com/mparrett/let-go-undertone/letgo` registers the `audio`
namespace from the package's `init`, so lg code can call it with no generated
interop:

| native | |
|---|---|
| `audio/set-parameters` | one map, merged over the current state, applied at the next pattern boundary; unknown keys are ignored |
| `audio/music-start`, `audio/music-stop` | playback |
| `audio/trigger` | a trigger map (`{:shot :hit :gain 0.8 :pan 0.3}`) or a vector of them; fire-and-forget |
| `audio/render-wav` | `(audio/render-wav "out.wav" 20.0)`: offline render, for checking determinism without a sound card |

The surface is write-only: nothing reads audio state back into lg, so game
logic cannot branch on whether a sound played and replays stay deterministic.
Triggers sent before the music starts are dropped silently.

The natives act on a default `letgo.Music`. A Go host that wants to own the
instance creates one with `letgo.NewMusic` and calls `letgo.SetMusic` before
running lg code. Hosts with no filesystem (`GOOS=js`) can load the embedded
`ambience` namespace with `letgo.LoadEmbedded`.

### With lgx

Link the binding into a built `lg` runtime as a Go dep. It registers itself,
so it needs `:go/version` and no `:go/interop`:

```clojure
{:lg-runtime :built
 :lg-version "1.13.0"
 :deps {github.com/mparrett/let-go-undertone/letgo {:go/version "main"}}}
```

`:go/version` takes anything Go accepts: a tag, a commit, a branch. Then, from
lg:

```clojure
(audio/set-parameters {:mood :dungeon :bpm 54.0 :root 38 :drone-gain 0.5})
(audio/music-start)
(audio/trigger {:shot :footstep :gain 0.3 :pan 0.4})
```

The `ambience` namespace that derives full parameter maps from
`(seed, depth, tension)` is at `letgo/lg/ambience.lg`. Put that directory on
your load path, or embed it as above.

### Without lgx

```
go run ./cmd/lghost -e '(do (ambience/score! 7 4 0.6) (audio/music-start) (sleep 60000))'
go run ./cmd/lghost -e '(do (ambience/score! 7 4 0.6) (audio/render-wav "out.wav" 20.0))'
```

`letgo/parity_test.go` renders 144 mood × seed × depth × tension cases
through both derivations, Go and lg, and compares the parameters field by
field plus a hash of the PCM. Two things it caught are worth knowing if you
port similar code to lg. First, lg's `*` raises on overflow, so seed mixing
needs `unchecked-multiply`. Second, lg has no unsigned integer, so an unsigned
remainder needs doing by hand (`ambience/umod`).

## Keep the player reachable

oto attaches a cleanup to every `*oto.Player` that closes it once the handle
is unreachable, and its internal mixer does not keep the handle alive
([ebitengine/oto#293](https://github.com/ebitengine/oto/issues/293)). A player
held only in a local stops at the next GC, silently, with no error. Store the
result of `NewPlayer` in a struct field or a package-level variable, or call
`runtime.KeepAlive` on it for as long as it plays. `letgo.Music` keeps its
player in a field. `cmd/undertone/playerlifetime_test.go` parses every
`NewPlayer` call site in the module and fails on one that does neither.

## Latency

`engine.DeviceBuffer` and `engine.PlayerBuffer` replace oto's
streaming-oriented defaults. On the measuring bench, tap-to-sound dropped from
a median of about 820 ms to about 130 ms. The device buffer is kept at a
power-of-two frame count, because oto's ScriptProcessorNode fallback (browsers
without AudioWorklet) throws otherwise.

## Building

Builds need no cgo. `CGO_ENABLED=0` works for macOS, for Linux (PulseAudio
through a pure-Go client, or ALSA loaded through purego), and for
`GOOS=js GOARCH=wasm`. oto is pinned at v3.5.1.

## History

Extracted on 2026-10-09 from a private workspace, where it began as
`sointu-homage` on 2026-08-14. The commit history is preserved.

## Thanks

To [jasalt](https://github.com/jasalt), whose
[govertone](https://github.com/jasalt/govertone), an experiment pairing Sointu
with let-go shared in the Clojurians Slack `#let-go` channel in July 2026,
introduced Sointu to this project and prompted it. And to
[vsariola](https://github.com/vsariola) for Sointu itself.

## License

MIT; see [LICENSE](LICENSE).
