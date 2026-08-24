// Package host is the Go side of the audio surface: the music/one-shot state,
// the lg-facing natives, and the loader for the embedded lg sources. It exists
// as a package so more than one front end can embed the same surface — the
// native CLI host in ../lghost, and the browser demo in ../webdemo, which has
// no filesystem and loads the lg lane through core's load-string instead of
// the resolver's load path.
package host

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/ebitengine/oto/v3"
	"github.com/nooga/let-go/pkg/api"
	"github.com/nooga/let-go/pkg/rt"
	"github.com/nooga/let-go/pkg/vm"

	"undertone/engine"
	"undertone/lg"
)

// Music holds the host side of the audio surface. The mutex guards the
// lg-facing methods against the oto callback goroutine reading the synth.
type Music struct {
	mu     sync.Mutex
	params engine.Params
	synth  *engine.Synth
	player *oto.Player
	ctx    *oto.Context

	// starting guards the window between opening the device and the unlock
	// gesture arriving, so a second Start cannot open a second context.
	starting bool
}

// NewMusic starts from base parameters; the lg script is expected to
// overwrite every scalar via audio/set-parameters.
func NewMusic(p engine.Params) *Music {
	return &Music{params: p}
}

// Params returns a snapshot of the current parameter state — the demo trace
// reads BPM and PulseGain off it. Read-only by construction: it is a copy.
func (m *Music) Params() engine.Params {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.params
}

// setParameters merges a partial lg map over current state and stages it.
// Unknown keys are ignored (forward compatibility) and absent keys keep their
// current value, so live-coding is just calling it again with what changed.
func (m *Music) setParameters(v vm.Value) vm.Value {
	lgm, ok := v.(*vm.PersistentMap)
	if !ok {
		return vm.NIL
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	// :mood selects the patch palette, which is the one thing not expressible
	// as a scalar in the map. Deriving a throwaway params for the mood is how
	// the host gets that palette without exporting the patch tables.
	if s, ok := kwStr(lgm, "mood"); ok {
		switch s {
		case "overworld":
			m.params.Palette = engine.DeriveOverworld(1, 0, 0).Palette
		case "dungeon":
			m.params.Palette = engine.DeriveDungeon(1, 0, 0).Palette
		}
	}
	flt(lgm, "bpm", &m.params.BPM)
	integer(lgm, "root", &m.params.Root)
	flt(lgm, "drone-gain", &m.params.DroneGain)
	flt(lgm, "fifth-gain", &m.params.FifthGain)
	flt(lgm, "pad-density", &m.params.PadDensity)
	flt(lgm, "bell-density", &m.params.BellDensity)
	flt(lgm, "air-gain", &m.params.AirGain)
	flt(lgm, "pulse-gain", &m.params.PulseGain)
	integer(lgm, "fifth-offset", &m.params.FifthOffset)
	integer(lgm, "bell-lift", &m.params.BellLift)
	if sc, ok := ints(lgm, "scale"); ok {
		m.params.Scale = sc
	}
	if s, ok := kwStr(lgm, "lead"); ok {
		engine.ApplyLead(&m.params, s)
	}
	// :seed is int64 on the lg side; the engine's rng streams are uint64.
	var seed int
	if integer(lgm, "seed", &seed) {
		m.params.Seed = uint64(seed)
	}

	if m.synth != nil {
		m.synth.SetParameters(m.params)
	}
	return vm.NIL
}

// trigger accepts one trigger map or a vector of them (the shape
// sfx/for-events returns) and stages each as a one-shot. Fire-and-forget by
// contract: with no synth running (music not started, or a -dry demo) the
// triggers drop silently, because the write-only rule means nothing upstream
// may ever learn whether a sound played.
func (m *Music) trigger(v vm.Value) vm.Value {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.synth == nil {
		return vm.NIL
	}
	var maps []*vm.PersistentMap
	switch t := v.(type) {
	case *vm.PersistentMap:
		maps = append(maps, t)
	case vm.ArrayVector:
		for _, e := range t {
			if pm, ok := e.(*vm.PersistentMap); ok {
				maps = append(maps, pm)
			}
		}
	}
	for _, lgm := range maps {
		name, ok := kwStr(lgm, "shot")
		if !ok {
			continue
		}
		sh := engine.Shot{Name: name, Gain: 1, Pan: 0.5}
		flt(lgm, "gain", &sh.Gain)
		flt(lgm, "pan", &sh.Pan)
		flt(lgm, "delay-ms", &sh.DelayMS)
		if integer(lgm, "note", &sh.Note) {
			sh.HasNote = true
		} else if integer(lgm, "degree", &sh.Degree) {
			sh.HasDegree = true
			integer(lgm, "octave", &sh.Octave)
		}
		// Seeds may arrive as an lg int (the prototype's own lg) or as a
		// decimal string (xsofy's, which has to survive a JSON hop). Accept
		// both, so this host can drive either vocabulary unchanged.
		var seed int
		if integer(lgm, "seed", &seed) {
			sh.Seed = uint64(seed)
		} else if s, ok := kwStr(lgm, "seed"); ok {
			if v, err := strconv.ParseInt(s, 10, 64); err == nil {
				sh.Seed = uint64(v)
			}
		}
		m.synth.TriggerShot(sh)
	}
	return vm.NIL
}

// Start opens the audio device and begins streaming. Exported because the
// demo drivers call it directly — in the browser it must run inside a user
// gesture (autoplay policy), not from lg.
func (m *Music) Start() vm.Value {
	m.mu.Lock()
	if m.player != nil || m.starting {
		m.mu.Unlock()
		return vm.NIL
	}
	m.starting = true
	p := m.params
	m.mu.Unlock()

	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   engine.SampleRate,
		ChannelCount: 2,
		Format:       oto.FormatSignedInt16LE,
		BufferSize:   engine.DeviceBuffer,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "audio/music-start:", err)
		m.mu.Lock()
		m.starting = false
		m.mu.Unlock()
		return vm.NIL
	}
	// Deliberately NOT holding the mutex here. In a browser this channel does
	// not close until the user makes an unlocking gesture, which can be never
	// — and holding the lock across that wait deadlocks every other method,
	// including Started(), which callers poll precisely to find out whether
	// this finished. (Cost a real debugging session: it presented as the wasm
	// runtime killing the module for deadlock, not as a lock-ordering bug.)
	<-ready

	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx = ctx
	m.synth = engine.NewSynth(p)
	// Parameters may have moved while we waited for the gesture; the synth is
	// built from the snapshot, so catch it up to whatever is current.
	m.synth.SetParameters(m.params)
	m.player = ctx.NewPlayer(io.Reader(m.synth))
	// Before Play: oto's default read-ahead is half a second, which a
	// one-shot has to wait out (see engine.PlayerBuffer).
	m.player.SetBufferSize(engine.PlayerBufferBytes())
	m.player.Play()
	m.starting = false
	return vm.NIL
}

// Started reports whether playback is live. Go-side only, deliberately not a
// native: a guest-visible (audio/playing?) would let game logic branch on audio
// state, which is exactly what the write-only rule forbids.
func (m *Music) Started() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.player != nil
}

func (m *Music) Stop() vm.Value {
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
func (m *Music) renderWAV(path string, seconds float64) vm.Value {
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

// current is the Music instance the audio natives act on. It is indirect
// because namespace registration must happen exactly once per process: a
// second vm.NewNamespace("audio") does replace the registry entry, but lg code
// loaded against the first one keeps referring to the first one's Vars, so a
// re-register silently leaves earlier callers writing to the abandoned
// instance. Swapping the target instead keeps every caller pointed at the
// live one.
var (
	current     atomic.Pointer[Music]
	installOnce sync.Once
)

// Install points the audio namespace at m, registering it on first call. An
// embedder can do this with no changes to let-go itself, which is what makes
// this a self-contained spike: vm.NewNamespace + vm.MustBox + rt.RegisterNS
// are all exported.
func Install(m *Music) {
	current.Store(m)
	installOnce.Do(func() {
		ns := vm.NewNamespace("audio")
		ns.Def("set-parameters", vm.MustBox(func(v vm.Value) vm.Value {
			return current.Load().setParameters(v)
		}))
		ns.Def("music-start", vm.MustBox(func() vm.Value { return current.Load().Start() }))
		ns.Def("music-stop", vm.MustBox(func() vm.Value { return current.Load().Stop() }))
		ns.Def("trigger", vm.MustBox(func(v vm.Value) vm.Value {
			return current.Load().trigger(v)
		}))
		// spike-only
		ns.Def("render-wav", vm.MustBox(func(path string, secs float64) vm.Value {
			return current.Load().renderWAV(path, secs)
		}))
		rt.RegisterNS(ns)
	})
}

// LoadEmbedded loads the embedded lg sources (undertone/lg, dependency order)
// through core's load-string — the lane for hosts with no filesystem. Each
// source is handed over as a Def'd var rather than spliced into the expression
// string, so no escaping of the lg text is ever needed.
func LoadEmbedded(c *api.LetGo) error {
	for _, s := range lg.Sources {
		if err := c.Def("undertone-embedded-src", s.Text); err != nil {
			return fmt.Errorf("%s: %w", s.Name, err)
		}
		if _, err := c.Run("(load-string undertone-embedded-src)"); err != nil {
			return fmt.Errorf("%s: %w", s.Name, err)
		}
	}
	return nil
}

// LoadSource loads one lg namespace from source text through core's
// load-string — the same lane as LoadEmbedded, but for sources the caller
// supplies at runtime. The tuner uses it to load xsofy's OWN music.lg and
// sfx.lg, fetched over HTTP, so what it plays is the shipping vocabulary
// rather than a copy that can drift.
func LoadSource(c *api.LetGo, name, text string) error {
	if err := c.Def("undertone-loaded-src", text); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if _, err := c.Run("(load-string undertone-loaded-src)"); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// SetParams replaces the parameter state outright and stages it. Distinct
// from the lg-facing partial merge: a caller that owns the whole derivation
// (the tuner's depth/tension sliders) wants replacement, not merge, or
// muting the bed would leave the previous gains behind.
func (m *Music) SetParams(p engine.Params) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.params = p
	if m.synth != nil {
		m.synth.SetParameters(p)
	}
}
