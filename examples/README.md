# Examples

Three ways in. Browser pages need HTTPS or `localhost`, since Safari only
offers AudioWorklet in a secure context; tap to start audio.

| Example | What it shows | Size (wasm) |
|---|---|---|
| [`grid/`](grid) | A step sequencer on the engine: lead, bass and pulse lanes over the generated bed. A link carries the pattern. | ~5 MB |
| [`player/`](player) | The engine alone, generative, with a four-band meter and query-string controls (`?depth=4&tension=0.6&seed=7&lead=pluck`). | ~3 MB |
| [`descent/`](descent) | let-go driving the engine: lg code derives music parameters and sound-effect triggers from a scripted descent-and-fight, natively (`cli/`) and in the browser (`web/`). | ~29 MB |

The descent demo's size is let-go's runtime; the engine is a few MB of it.
Served compressed it is about 4.4 MB with brotli.

## Run

From the repository root:

```sh
# grid
GOOS=js GOARCH=wasm go build -o examples/grid/main.wasm ./examples/grid
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" examples/grid/
python3 examples/serve.py 8900 grid

# player: the page runs cmd/undertone
GOOS=js GOARCH=wasm go build -o examples/player/main.wasm ./cmd/undertone
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" examples/player/
python3 examples/serve.py 8899 player

# descent, native
go run ./examples/descent/cli -demo -dry   # the tension trace, no sound
go run ./examples/descent/cli -demo        # with sound

# descent, browser
GOOS=js GOARCH=wasm go build -o examples/descent/web/main.wasm ./examples/descent/web
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" examples/descent/web/
python3 examples/serve.py 8901 descent/web
```

`serve.py` binds all interfaces and sends `Cache-Control: no-cache`, so a
phone on the LAN won't run a stale wasm. A phone needs HTTPS to get
AudioWorklet, though; `tailscale serve` is one way to give it that.

## The descent demo's lg

`descent/lg/music.lg` and `descent/lg/sfx.lg` are frozen prototypes of a game's
music and sound vocabulary; `demo.lg` is written against them. Their
maintained successors live in the game that grew out of them, in a different
shape. Don't tune these copies.
