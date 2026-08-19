// lghost embeds let-go and hands it the undertone engine through the audio
// surface sketch's v1 split: lg is the control language, Go is the DSP engine.
//
// Only control-rate data crosses the boundary — one parameter map per pattern
// (every few seconds), never per-sample PCM. That is the whole point: the
// per-sample unit VM runs millions of ops/second, so putting it in lg would
// walk straight into the boxed variadic call ABI (advisory #722). The layer
// that genuinely wants to be lg is the one above it: deriving music parameters
// from game state, which happens at human rates and is where a game's taste
// lives.
//
// The natives registered here are the sketch's proposed surface, no more:
//
//	audio/music-start            begin streaming
//	audio/music-stop             stop
//	audio/set-parameters         one map, merged over current state, applied
//	                             at the next pattern boundary
//
// audio/render-wav is a fourth, spike-only: it is how determinism gets checked
// without a sound card, not part of the proposed surface.
//
//	go run ./lghost -lg lg/ambience.lg -play
//	go run ./lghost -lg lg/ambience.lg -wav out.wav -dur 20
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/nooga/let-go/pkg/api"
	"github.com/nooga/let-go/pkg/rt"
	"github.com/nooga/let-go/pkg/vm"

	"undertone/engine"
)

// music holds the host side of the audio surface. The mutex guards the
// lg-facing methods against the oto callback goroutine reading the synth.
type music struct {
	mu     sync.Mutex
	params engine.Params
	synth  *engine.Synth
	player *oto.Player
	ctx    *oto.Context
}

// setParameters merges a partial lg map over current state and stages it.
// Unknown keys are ignored (forward compatibility) and absent keys keep their
// current value, so live-coding is just calling it again with what changed.
func (m *music) setParameters(v vm.Value) vm.Value {
	lg, ok := v.(*vm.PersistentMap)
	if !ok {
		return vm.NIL
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	// :mood selects the patch palette, which is the one thing not expressible
	// as a scalar in the map. Deriving a throwaway params for the mood is how
	// the host gets that palette without exporting the patch tables.
	if s, ok := kwStr(lg, "mood"); ok {
		switch s {
		case "overworld":
			m.params.Palette = engine.DeriveOverworld(1, 0, 0).Palette
		case "dungeon":
			m.params.Palette = engine.DeriveDungeon(1, 0, 0).Palette
		}
	}
	flt(lg, "bpm", &m.params.BPM)
	integer(lg, "root", &m.params.Root)
	flt(lg, "drone-gain", &m.params.DroneGain)
	flt(lg, "fifth-gain", &m.params.FifthGain)
	flt(lg, "pad-density", &m.params.PadDensity)
	flt(lg, "bell-density", &m.params.BellDensity)
	flt(lg, "air-gain", &m.params.AirGain)
	flt(lg, "pulse-gain", &m.params.PulseGain)
	integer(lg, "fifth-offset", &m.params.FifthOffset)
	integer(lg, "bell-lift", &m.params.BellLift)
	if sc, ok := ints(lg, "scale"); ok {
		m.params.Scale = sc
	}
	if s, ok := kwStr(lg, "lead"); ok {
		engine.ApplyLead(&m.params, s)
	}
	// :seed is int64 on the lg side; the engine's rng streams are uint64.
	var seed int
	if integer(lg, "seed", &seed) {
		m.params.Seed = uint64(seed)
	}

	if m.synth != nil {
		m.synth.SetParameters(m.params)
	}
	return vm.NIL
}

func (m *music) start() vm.Value {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.player != nil {
		return vm.NIL
	}
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   engine.SampleRate,
		ChannelCount: 2,
		Format:       oto.FormatSignedInt16LE,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "audio/music-start:", err)
		return vm.NIL
	}
	<-ready
	m.ctx = ctx
	m.synth = engine.NewSynth(m.params)
	m.player = ctx.NewPlayer(io.Reader(m.synth))
	m.player.Play()
	return vm.NIL
}

// started reports whether playback is live. Go-side only, deliberately not a
// native: a guest-visible (audio/playing?) would let game logic branch on audio
// state, which is exactly what the write-only rule forbids.
func (m *music) started() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.player != nil
}

func (m *music) stop() vm.Value {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.player != nil {
		m.player.Close()
		m.player = nil
	}
	return vm.NIL
}

// renderWAV is the spike-only offline lane: it makes "same parameters ->
// byte-identical audio" checkable in a test rather than by ear.
func (m *music) renderWAV(path string, seconds float64) vm.Value {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := engine.NewSynth(m.params)
	if err := engine.WriteWAV(path, s, seconds); err != nil {
		fmt.Fprintln(os.Stderr, "audio/render-wav:", err)
	}
	return vm.NIL
}

// ---- lg map readers ----
// Absent keys leave the target untouched; that is the partial-merge contract.

func at(m *vm.PersistentMap, k string) vm.Value {
	return m.ValueAtOr(vm.Keyword(k), vm.NIL)
}

// flt accepts both lg ints and floats, so a derivation may write :bpm 60.
func flt(m *vm.PersistentMap, k string, dst *float64) bool {
	switch v := at(m, k).(type) {
	case vm.Float:
		*dst = float64(v)
	case vm.Int:
		*dst = float64(v)
	default:
		return false
	}
	return true
}

func integer(m *vm.PersistentMap, k string, dst *int) bool {
	v, ok := at(m, k).(vm.Int)
	if !ok {
		return false
	}
	*dst = int(v)
	return true
}

// kwStr reads a key that may be written as either :chime or "chime".
func kwStr(m *vm.PersistentMap, k string) (string, bool) {
	switch v := at(m, k).(type) {
	case vm.Keyword:
		return string(v), true
	case vm.String:
		return string(v), true
	}
	return "", false
}

func ints(m *vm.PersistentMap, k string) ([]int, bool) {
	vec, ok := at(m, k).(vm.ArrayVector)
	if !ok {
		return nil, false
	}
	out := make([]int, 0, len(vec))
	for _, e := range vec {
		n, ok := e.(vm.Int)
		if !ok {
			return nil, false
		}
		out = append(out, int(n))
	}
	return out, true
}

// current is the music instance the audio natives act on. It is indirect
// because namespace registration must happen exactly once per process: a
// second vm.NewNamespace("audio") does replace the registry entry, but lg code
// loaded against the first one keeps referring to the first one's Vars, so a
// re-register silently leaves earlier callers writing to the abandoned
// instance. Swapping the target instead keeps every caller pointed at the
// live one.
var (
	current     atomic.Pointer[music]
	installOnce sync.Once
)

// install points the audio namespace at m, registering it on first call. An
// embedder can do this with no changes to let-go itself, which is what makes
// this a self-contained spike: vm.NewNamespace + vm.MustBox + rt.RegisterNS
// are all exported.
func install(m *music) {
	current.Store(m)
	installOnce.Do(func() {
		ns := vm.NewNamespace("audio")
		ns.Def("set-parameters", vm.MustBox(func(v vm.Value) vm.Value {
			return current.Load().setParameters(v)
		}))
		ns.Def("music-start", vm.MustBox(func() vm.Value { return current.Load().start() }))
		ns.Def("music-stop", vm.MustBox(func() vm.Value { return current.Load().stop() }))
		// spike-only
		ns.Def("render-wav", vm.MustBox(func(path string, secs float64) vm.Value {
			return current.Load().renderWAV(path, secs)
		}))
		rt.RegisterNS(ns)
	})
}

func main() {
	loadPath := flag.String("load-path", "lg", "directory holding the lg namespaces")
	nsName := flag.String("ns", "ambience", "lg namespace to require")
	expr := flag.String("e", "", "lg expression to run after the namespace loads")
	demo := flag.Bool("demo", false, "walk demo.lg's scripted descent-and-fight arc")
	dry := flag.Bool("dry", false, "with -demo: print the tension trace, make no sound")
	turnMS := flag.Int("turn-ms", 1500, "with -demo: real milliseconds per game turn")
	flag.Parse()

	// The base params supply the patch palette and a lead default; the script
	// is expected to overwrite every scalar via audio/set-parameters.
	m := &music{params: engine.DeriveDungeon(1, 0, 0)}
	install(m)

	c, err := api.NewLetGo("undertone.host")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// api.Run compiles a single form, so the whole-file lane is require, not a
	// read-and-eval loop — which is also how xsofy loads its own namespaces.
	c.SetLoadPath([]string{*loadPath})
	if _, err := c.Run("(require '" + *nsName + ")"); err != nil {
		fmt.Fprintln(os.Stderr, "lg:", err)
		os.Exit(1)
	}
	if *demo {
		if err := runDemo(c, m, *dry, time.Duration(*turnMS)*time.Millisecond); err != nil {
			fmt.Fprintln(os.Stderr, "demo:", err)
			os.Exit(1)
		}
		return
	}
	if *expr != "" {
		v, err := c.Run(*expr)
		if err != nil {
			fmt.Fprintln(os.Stderr, "lg:", err)
			os.Exit(1)
		}
		if v != vm.NIL {
			fmt.Println(v)
		}
	}
}
