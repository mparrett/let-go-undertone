//go:build js

// xsofymod is xsofy's audio consumer: a sibling wasm module that listens for
// the game's `xsofy/audio` frames and renders them with the undertone engine.
//
// Sibling, not nested — the same conclusion the Doom investigation reached for
// graphics. The game is a let-go wasm module; this is a separate, much smaller
// Go wasm module carrying only the engine (no let-go runtime), and the two
// communicate through data. It self-wires: ui-bridge's js/emit already
// delivers each frame to `window` as a CustomEvent, correctly relayed across
// the worker boundary, so this module needs no shell API — only to be on the
// page. That also means it can be added and removed without touching the
// shipping shell.
//
// It lives on the MAIN thread because AudioContext does not exist in worker
// scope, and xsofy's production boot is worker mode (the SAB input ring needs
// COI). The worker sends control data; this thread makes sound.
//
// The tension ENVELOPE lives here, which is the whole point of the design:
// the memory belongs to the thing making the sound, so the game needs neither
// a world field nor a mutable global for a value only the speakers read.
// Frames arrive once per turn, so decay is in game-clock units.
//
//	GOOS=js GOARCH=wasm go build -o dist/xsofy-audio.wasm ./xsofymod
package main

import (
	"fmt"
	"io"
	"strconv"
	"sync"
	"syscall/js"

	"github.com/ebitengine/oto/v3"

	"undertone/engine"
)

// defaultDecay: ~7-turn half-life. Instant attack, exponential release —
// danger lands immediately, relief is earned. The single taste knob.
const defaultDecay = 0.9

type module struct {
	mu      sync.Mutex
	synth   *engine.Synth
	params  engine.Params
	tension float64
	started bool
	pending []engine.Params // frames that arrived before audio unlocked
}

var mod = &module{params: engine.DeriveDungeon(1, 1, 0)}

// frame applies one audio frame: derive, envelope, stage parameters, fire the
// turn's one-shots. Fire-and-forget — every failure path is silence, never an
// exception into the game's emit call.
func (m *module) frame(detail js.Value) {
	if detail.IsUndefined() || detail.IsNull() {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	// Seeds cross as decimal strings: the frame is JSON and JS numbers are
	// f64, so an int64 seed would silently round past 2^53.
	seed := uint64(1)
	if s := detail.Get("seed"); s.Type() == js.TypeString {
		if v, err := strconv.ParseInt(s.String(), 10, 64); err == nil {
			seed = uint64(v)
		}
	}
	depth := 1
	if d := detail.Get("depth"); d.Type() == js.TypeNumber {
		depth = d.Int()
	}
	target := 0.0
	if t := detail.Get("tension-target"); t.Type() == js.TypeNumber {
		target = t.Float()
	}

	// The envelope: instant attack, exponential release. One frame per turn,
	// so this decays per turn — idling does not relieve dread, taking turns
	// does. Without it the ~8s pattern boundary samples an instantaneous
	// signal and aliases (kill a mob one turn early, the whole next phrase
	// goes calm).
	m.tension = max(target, m.tension*defaultDecay)

	mood := "dungeon"
	if s := detail.Get("mood"); s.Type() == js.TypeString {
		mood = s.String()
	}
	if mood == "overworld" {
		m.params = engine.DeriveOverworld(seed, depth, m.tension)
	} else {
		m.params = engine.DeriveDungeon(seed, depth, m.tension)
	}
	if m.synth != nil {
		m.synth.SetParameters(m.params)
	}

	m.fireTriggers(detail.Get("triggers"))
}

// fireTriggers stages the frame's one-shots. Notes arrive one of two ways:
// :note for sounds with no musical context (impacts, steps), or
// :degree + :octave for sounds that must land in key — resolved here against
// the SAME parameters that pitched the music, so a pickup chime is in the
// floor's scale by construction.
func (m *module) fireTriggers(trs js.Value) {
	if m.synth == nil || trs.Type() != js.TypeObject {
		return
	}
	for i := 0; i < trs.Length(); i++ {
		t := trs.Index(i)
		shot := t.Get("shot")
		if shot.Type() != js.TypeString {
			continue
		}
		note := 60
		if n := t.Get("note"); n.Type() == js.TypeNumber {
			note = n.Int()
		} else if d := t.Get("degree"); d.Type() == js.TypeNumber {
			note = m.resolveDegree(d.Int(), numOr(t.Get("octave"), 0))
		}
		var seed uint64
		if s := t.Get("seed"); s.Type() == js.TypeString {
			if v, err := strconv.ParseInt(s.String(), 10, 64); err == nil {
				seed = uint64(v)
			}
		}
		m.synth.Trigger(shot.String(), note,
			floatOr(t.Get("gain"), 1), floatOr(t.Get("pan"), 0.5),
			floatOr(t.Get("delay-ms"), 0), seed)
	}
}

// resolveDegree maps a scale degree in the current parameters' scale to an
// absolute note, wrapping octaves past the top of the scale.
func (m *module) resolveDegree(degree, octave int) int {
	sc := m.params.Scale
	if len(sc) == 0 {
		return m.params.Root + 12*octave
	}
	if degree < 0 {
		degree = 0
	}
	return m.params.Root + 12*(octave+degree/len(sc)) + sc[degree%len(sc)]
}

func numOr(v js.Value, d int) int {
	if v.Type() == js.TypeNumber {
		return v.Int()
	}
	return d
}

func floatOr(v js.Value, d float64) float64 {
	if v.Type() == js.TypeNumber {
		return v.Float()
	}
	return d
}

// start opens the audio device. It blocks until oto sees an unlocking
// gesture, so it runs on its own goroutine from init — NOT from the gesture
// handler. oto's ready channel only closes on a touchend/keyup/mouseup that
// arrives AFTER the AudioContext exists, so creating the context inside the
// first tap's handler waits forever for a gesture that already happened.
func (m *module) start() {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	p := m.params
	m.mu.Unlock()

	synth := engine.NewSynth(p)
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   engine.SampleRate,
		ChannelCount: 2,
		Format:       oto.FormatSignedInt16LE,
	})
	if err != nil {
		fmt.Println("xsofy-audio: no audio device:", err)
		return
	}
	<-ready
	ctx.NewPlayer(io.Reader(synth)).Play()

	m.mu.Lock()
	m.synth = synth
	synth.SetParameters(m.params) // catch up to whatever arrived while waiting
	m.mu.Unlock()
	fmt.Println("xsofy-audio: playing")
}

func main() {
	go mod.start()

	js.Global().Call("addEventListener", "xsofy/audio",
		js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				mod.frame(args[0].Get("detail"))
			}
			return nil
		}))
	// Announce readiness so a shell (or a probe) can tell the module loaded
	// without reaching into it.
	js.Global().Set("XsofyAudioReady", js.ValueOf(true))
	fmt.Println("xsofy-audio: listening for xsofy/audio frames")

	select {} // keep the module alive for the listener
}
