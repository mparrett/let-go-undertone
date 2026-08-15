// Package engine is undertone's generative-ambience engine: a stack-of-unit-
// opcodes synth VM, a tracker-style sequencer, and the game-state -> music
// parameter derivation, all deterministic from a seed.
//
// The package boundary exists so more than one front end can drive the same
// engine: the CLI in the parent directory, and the let-go embedding host in
// ./lghost, which moves the derivation layer into lg (the audio surface
// sketch's v1 split: lg is the control language, Go is the DSP engine).
package engine

import (
	"encoding/binary"
	"math"
	"os"
	"sync/atomic"
)

const SampleRate = 44100

// ---- unit VM ----

const (
	opEnv   = iota // p: attack s, decay s, sustain lvl, release s   -> push env
	opOsc          // p: transpose semis, shape, sweep semis, sweep decay s -> push sample
	opMul          // pop a, b -> push a*b
	opLpf          // p: cutoff Hz; pop x -> push lowpassed x
	opSend         // p: amount; peek top, add top*amount to delay bus
	opOut          // p: gain, pan 0..1; pop x -> accumulate to L/R
	opPluck        // p: transpose semis, feedback; Karplus-Strong string -> push sample
)

const (
	shpSine = iota
	shpSaw
	shpPulse
	shpNoise
)

type Unit struct {
	op int
	p  [4]float64
}

type Patch []Unit

// per-voice, per-unit mutable state
type ustate struct {
	phase, env, lp, sweep float64
	stage                 int // env: 0 attack, 1 decay, 2 sustain, 3 release
	rng                   uint64
	ks                    []float64 // Karplus-Strong delay line, allocated on first sample
	kpos                  int
}

type Voice struct {
	patch    Patch
	note     int
	gain     float64
	gate     bool
	gateRows int
	units    []ustate
	active   bool
}

func (v *Voice) trigger(patch Patch, note, gateRows int, gain float64, seed uint64) {
	v.patch = patch
	v.note = note
	v.gain = gain
	v.gate = true
	v.gateRows = gateRows
	v.active = true
	v.units = make([]ustate, len(patch))
	for i := range v.units {
		v.units[i].sweep = 1
		v.units[i].rng = seed*2862933555777941757 + uint64(i) + 3037000493
	}
}

// run executes the patch for one sample, accumulating into l/r.
func (v *Voice) run(l, r, delay *float64) {
	var stack [8]float64
	sp := 0
	alive := false
	for i := range v.patch {
		u := &v.patch[i]
		s := &v.units[i]
		switch u.op {
		case opEnv:
			switch s.stage {
			case 0:
				s.env += 1 / (u.p[0]*SampleRate + 1)
				if s.env >= 1 {
					s.env, s.stage = 1, 1
				}
			case 1:
				s.env += (u.p[2] - s.env) / (u.p[1] * SampleRate)
				if s.env-u.p[2] < 1e-3 {
					s.stage = 2
				}
			case 3:
				s.env -= s.env / (u.p[3] * SampleRate)
			}
			if !v.gate && s.stage < 3 {
				s.stage = 3
			}
			if s.stage < 3 || s.env > 1e-4 {
				alive = true
			}
			stack[sp] = s.env
			sp++
		case opOsc:
			semis := float64(v.note) + u.p[0] + u.p[2]*s.sweep
			s.sweep -= s.sweep / (u.p[3]*SampleRate + 1)
			freq := 440 * math.Pow(2, (semis-69)/12)
			s.phase += freq / SampleRate
			s.phase -= math.Floor(s.phase)
			var x float64
			switch int(u.p[1]) {
			case shpSine:
				x = math.Sin(2 * math.Pi * s.phase)
			case shpSaw:
				x = 2*s.phase - 1
			case shpPulse:
				if s.phase < 0.5 {
					x = 1
				} else {
					x = -1
				}
			case shpNoise:
				s.rng = s.rng*6364136223846793005 + 1442695040888963407
				x = float64(int64(s.rng>>11))/(1<<52) - 1
			}
			stack[sp] = x
			sp++
		case opMul:
			sp--
			stack[sp-1] *= stack[sp]
		case opLpf:
			c := 1 - math.Exp(-2*math.Pi*u.p[0]/SampleRate)
			s.lp += c * (stack[sp-1] - s.lp)
			stack[sp-1] = s.lp
		case opPluck:
			// Karplus-Strong: a noise burst circulating in a delay line
			// the length of the period, averaged (lowpassed) each pass.
			if s.ks == nil {
				freq := 440 * math.Pow(2, (float64(v.note)+u.p[0]-69)/12)
				n := int(SampleRate / freq)
				if n < 2 {
					n = 2
				}
				s.ks = make([]float64, n)
				for i := range s.ks {
					s.rng = s.rng*6364136223846793005 + 1442695040888963407
					s.ks[i] = float64(int64(s.rng>>11))/(1<<52) - 1
				}
			}
			x := s.ks[s.kpos]
			next := s.ks[(s.kpos+1)%len(s.ks)]
			s.ks[s.kpos] = (x + next) * 0.5 * u.p[1]
			s.kpos = (s.kpos + 1) % len(s.ks)
			stack[sp] = x
			sp++
		case opSend:
			*delay += stack[sp-1] * u.p[0] * v.gain
		case opOut:
			sp--
			*l += stack[sp] * u.p[0] * (1 - u.p[1]) * v.gain
			*r += stack[sp] * u.p[0] * u.p[1] * v.gain
		}
	}
	v.active = alive
}

// ---- instruments (compact opcode tables, sointu-patch style) ----
// Two env→osc→out chains in one patch = two detuned layers; opOut
// accumulates, so layering needs no add opcode.

var drone = Patch{
	{opEnv, [4]float64{2.5, 2, 1, 4}},
	{opOsc, [4]float64{-0.06, shpSaw, 0, 1}},
	{opMul, [4]float64{}},
	{opLpf, [4]float64{280, 0, 0, 0}},
	{opOut, [4]float64{0.5, 0.42}},
	{opEnv, [4]float64{2.5, 2, 1, 4}},
	{opOsc, [4]float64{0.06, shpSaw, 0, 1}},
	{opMul, [4]float64{}},
	{opLpf, [4]float64{280, 0, 0, 0}},
	{opOut, [4]float64{0.5, 0.58}},
}

var pad = Patch{
	{opEnv, [4]float64{1.2, 1.5, 0.6, 1.8}},
	{opOsc, [4]float64{-0.04, shpSine, 0, 1}},
	{opMul, [4]float64{}},
	{opOut, [4]float64{0.5, 0.35}},
	{opEnv, [4]float64{1.4, 1.5, 0.6, 1.8}},
	{opOsc, [4]float64{0.04, shpSine, 0, 1}},
	{opMul, [4]float64{}},
	{opOut, [4]float64{0.5, 0.65}},
}

var bell = Patch{
	{opEnv, [4]float64{0.003, 1.4, 0, 0.5}},
	{opOsc, [4]float64{0, shpSine, 0, 1}},
	{opMul, [4]float64{}},
	{opSend, [4]float64{0.7, 0, 0, 0}},
	{opOut, [4]float64{0.5, 0.6}},
}

// ---- lead instruments (the bell slot) ----
// Different species, same opcodes: inharmonic partials read as metal,
// ring-mod as clang, filtered pulse as wood/breath.

// chime: three sine partials at non-integer ratios (1, ~2.29, ~3.01)
var chime = Patch{
	{opEnv, [4]float64{0.002, 1.6, 0, 0.6}},
	{opOsc, [4]float64{0, shpSine, 0, 1}},
	{opMul, [4]float64{}},
	{opSend, [4]float64{0.6, 0, 0, 0}},
	{opOut, [4]float64{0.35, 0.6}},
	{opEnv, [4]float64{0.002, 1.1, 0, 0.5}},
	{opOsc, [4]float64{14.35, shpSine, 0, 1}},
	{opMul, [4]float64{}},
	{opOut, [4]float64{0.18, 0.4}},
	{opEnv, [4]float64{0.002, 0.7, 0, 0.4}},
	{opOsc, [4]float64{19.08, shpSine, 0, 1}},
	{opMul, [4]float64{}},
	{opOut, [4]float64{0.1, 0.65}},
}

// clang: ring-modulated sine pair (env x osc x osc) — eerie metal
var clang = Patch{
	{opEnv, [4]float64{0.002, 1.2, 0, 0.5}},
	{opOsc, [4]float64{0, shpSine, 0, 1}},
	{opOsc, [4]float64{16.9, shpSine, 0, 1}},
	{opMul, [4]float64{}},
	{opMul, [4]float64{}},
	{opSend, [4]float64{0.5, 0, 0, 0}},
	{opOut, [4]float64{0.4, 0.55}},
}

// kalimba: struck sine with a brief bright partial — woody
var kalimba = Patch{
	{opEnv, [4]float64{0.001, 0.35, 0, 0.15}},
	{opOsc, [4]float64{0, shpSine, 0, 1}},
	{opMul, [4]float64{}},
	{opSend, [4]float64{0.4, 0, 0, 0}},
	{opOut, [4]float64{0.45, 0.55}},
	{opEnv, [4]float64{0.001, 0.08, 0, 0.05}},
	{opOsc, [4]float64{19, shpSine, 0, 1}},
	{opMul, [4]float64{}},
	{opOut, [4]float64{0.2, 0.45}},
}

// pluck: Karplus-Strong string — harp/lute, the sound the osc set can't fake
var pluck = Patch{
	{opEnv, [4]float64{0.001, 2.5, 0, 0.4}},
	{opPluck, [4]float64{0, 0.996, 0, 0}},
	{opMul, [4]float64{}},
	{opLpf, [4]float64{2600, 0, 0, 0}},
	{opSend, [4]float64{0.45, 0, 0, 0}},
	{opOut, [4]float64{0.55, 0.55}},
}

// reed: filtered pulse, slow attack, sustained — breath, needs a longer gate
var reed = Patch{
	{opEnv, [4]float64{0.15, 0.5, 0.5, 0.25}},
	{opOsc, [4]float64{0, shpPulse, 0, 1}},
	{opMul, [4]float64{}},
	{opLpf, [4]float64{900, 0, 0, 0}},
	{opSend, [4]float64{0.3, 0, 0, 0}},
	{opOut, [4]float64{0.22, 0.5}},
}

// overworld drone: sine pair — a bed, not a weight
var bed = Patch{
	{opEnv, [4]float64{3, 2, 1, 4}},
	{opOsc, [4]float64{-0.05, shpSine, 0, 1}},
	{opMul, [4]float64{}},
	{opOut, [4]float64{0.6, 0.45}},
	{opEnv, [4]float64{3, 2, 1, 4}},
	{opOsc, [4]float64{0.05, shpSine, 0, 1}},
	{opMul, [4]float64{}},
	{opOut, [4]float64{0.6, 0.55}},
}

// overworld air: same shape as air, opened up — wind, not rumble
var breeze = Patch{
	{opEnv, [4]float64{2, 2, 0.5, 3}},
	{opOsc, [4]float64{0, shpNoise, 0, 1}},
	{opMul, [4]float64{}},
	{opLpf, [4]float64{1400, 0, 0, 0}},
	{opLpf, [4]float64{1400, 0, 0, 0}},
	{opOut, [4]float64{0.9, 0.5}},
}

// tension pulse: a soft swept-sine tom, descendant of the techno kick
var pulse = Patch{
	{opEnv, [4]float64{0.002, 0.15, 0, 0.08}},
	{opOsc, [4]float64{0, shpSine, 18, 0.05}},
	{opMul, [4]float64{}},
	{opOut, [4]float64{0.7, 0.5}},
}

var air = Patch{
	{opEnv, [4]float64{2.5, 2.5, 0.4, 3.5}},
	{opOsc, [4]float64{0, shpNoise, 0, 1}},
	{opMul, [4]float64{}},
	// two one-pole stages: a single pole leaks enough top end that the
	// air track *brightened* with depth instead of darkening
	{opLpf, [4]float64{300, 0, 0, 0}},
	{opLpf, [4]float64{300, 0, 0, 0}},
	{opOut, [4]float64{1.1, 0.5}},
}

// ---- parameters: the (seed, depth) -> music map ----
// The kaiju-elements trick: taste lives in hand-tuned option tables,
// the seed just picks. Depth darkens the pick.

type Params struct {
	BPM         float64
	Root        int   // MIDI, chord anchor
	Scale       []int // semitone intervals
	DroneGain   float64
	FifthGain   float64 // second drone at the fifth; thickens with depth
	PadDensity  float64
	BellDensity float64
	AirGain     float64
	PulseGain   float64 // tension pulse track level (0 = silent)
	FifthOffset int     // second drone's interval below the chord; slides dissonant with tension
	BellLift    int     // semitones added to bell notes (overworld sparkle register)
	LeadName    string  // which instrument holds the bell slot
	LeadGate    int     // gate rows for the lead (sustained leads want > 1)
	Palette     Palette // the mood's patch bank
	Seed        uint64
}

// leadDef is one entry in the lead-instrument library.
type leadDef struct {
	name  string
	patch Patch
	gate  int
}

var leadLib = map[string]leadDef{
	"bell":    {"bell", bell, 1},
	"chime":   {"chime", chime, 1},
	"clang":   {"clang", clang, 1},
	"kalimba": {"kalimba", kalimba, 1},
	"reed":    {"reed", reed, 3},
	"pluck":   {"pluck", pluck, 1},
}

// mood-weighted audition lists; the seed picks (kaiju trick applied to
// timbre: two seeds differ in instrument, not just notes)
var dungeonLeads = []string{"bell", "chime", "chime", "clang", "kalimba", "pluck", "pluck"}
var overworldLeads = []string{"bell", "bell", "kalimba", "kalimba", "reed", "pluck", "pluck"}

// pickLead draws from a separate rng stream so the pattern content for a
// given seed is unchanged — only the instrument varies.
func pickLead(p *Params, names []string) {
	r := rng{p.Seed*0x9e3779b97f4a7c15 + 0x1eadf00d | 1}
	ApplyLead(p, names[r.n(len(names))])
}

func ApplyLead(p *Params, name string) {
	if d, ok := leadLib[name]; ok {
		p.LeadName, p.LeadGate, p.Palette.Bell = d.name, d.gate, d.patch
	}
}

// Palette is a mood's patch bank: mood = patches + a point in parameter space.
type Palette struct {
	Drone, Fifth, Pad, Bell, Air, Pulse Patch
}

var dungeonPalette = Palette{Drone: drone, Fifth: pad, Pad: pad, Bell: bell, Air: air, Pulse: pulse}
var overworldPalette = Palette{Drone: bed, Fifth: pad, Pad: pad, Bell: bell, Air: breeze, Pulse: pulse}

var scalesByDarkness = [][]int{
	{0, 2, 3, 5, 7, 9, 10}, // dorian
	{0, 2, 3, 5, 7, 8, 10}, // aeolian
	{0, 1, 3, 5, 7, 8, 10}, // phrygian
}

var scalesByBrightness = [][]int{
	{0, 2, 4, 5, 7, 9, 10}, // mixolydian
	{0, 2, 4, 5, 7, 9, 11}, // major
	{0, 2, 4, 6, 7, 9, 11}, // lydian
}

// tenseOffset slides the second drone's interval toward dissonance: the
// mood's calm interval, then tritone, then minor-ninth territory.
func tenseOffset(calm int, t float64) int {
	switch {
	case t >= 0.75:
		return -11
	case t >= 0.4:
		return -6
	}
	return calm
}

// DeriveDungeon maps game state to music. Depth darkens (where you are);
// tension 0..1 agitates (what's happening): tempo up, pulse in, the second
// drone slides fifth -> tritone -> minor ninth, calm layers thin out.
// tension 0 reproduces the pre-tension output exactly (no extra rng draws).
func DeriveDungeon(seed uint64, depth int, tension float64) Params {
	r := rng{seed ^ uint64(depth)*0x9e3779b97f4a7c15 | 1}
	d := float64(depth)
	t := math.Max(0, math.Min(1, tension))
	darkness := depth / 2
	if darkness >= len(scalesByDarkness) {
		darkness = len(scalesByDarkness) - 1
	}
	p := Params{
		BPM:         math.Max(46, 66-3*d) + float64(r.n(5)) + 30*t,
		Root:        57 - depth + []int{-2, 0, 0, 3}[r.n(4)],
		Scale:       scalesByDarkness[darkness],
		DroneGain:   math.Min(0.9, 0.55+0.06*d),
		FifthGain:   math.Min(0.7, 0.2*d) + 0.25*t,
		PadDensity:  (0.6 - 0.05*d) * (1 - 0.6*t),
		BellDensity: math.Max(0.04, 0.16-0.02*d) * (1 - 0.5*t),
		AirGain:     math.Min(0.6, 0.25+0.05*d) + 0.1*t,
		PulseGain:   t,
		FifthOffset: tenseOffset(-5, t), // perfect fifth above the low drone
		Palette:     dungeonPalette,
		Seed:        seed,
	}
	pickLead(&p, dungeonLeads)
	return p
}

// DeriveOverworld is the bright mirror: elevation raises and opens
// (mixolydian -> major -> lydian, higher root, denser lifted bells,
// breeze instead of rumble); tension works unchanged — brightness
// collapsing into the same tritone/minor-ninth dread.
func DeriveOverworld(seed uint64, elevation int, tension float64) Params {
	r := rng{seed ^ uint64(elevation)*0x9e3779b97f4a7c15 | 1}
	e := float64(elevation)
	t := math.Max(0, math.Min(1, tension))
	brightness := elevation / 2
	if brightness >= len(scalesByBrightness) {
		brightness = len(scalesByBrightness) - 1
	}
	p := Params{
		BPM:         74 + 3*e + float64(r.n(5)) + 30*t,
		Root:        62 + elevation + []int{-2, 0, 0, 3}[r.n(4)],
		Scale:       scalesByBrightness[brightness],
		DroneGain:   0.25 + 0.03*e,
		FifthGain:   0.3 + 0.05*e + 0.25*t,
		PadDensity:  0.7 * (1 - 0.6*t),
		BellDensity: (0.2 + 0.04*e) * (1 - 0.5*t),
		AirGain:     0.15 + 0.02*e + 0.1*t,
		PulseGain:   t,
		FifthOffset: tenseOffset(-8, t), // major third above the low drone: sunshine
		BellLift:    12,
		Palette:     overworldPalette,
		Seed:        seed,
	}
	pickLead(&p, overworldLeads)
	return p
}

// ---- generative sequencer ----

type rng struct{ s uint64 }

func (r *rng) next() uint64 {
	r.s ^= r.s << 13
	r.s ^= r.s >> 7
	r.s ^= r.s << 17
	return r.s
}
func (r *rng) f() float64  { return float64(r.next()>>11) / (1 << 53) }
func (r *rng) n(m int) int { return int(r.next() % uint64(m)) }

const rows = 16
const nTracks = 6 // drone, fifth, pad, bell, air, pulse

type trackRow struct {
	note     int
	gain     float64
	gateRows int
	patch    Patch
}

// buildPatterns fills one pattern (16 rows x nTracks) from Params.
// Chord drifts between tonic, VI, and iv each pattern; bells walk the
// scale — per-pattern reseeding is the gradual-mutation mechanism.
func buildPatterns(p Params, pat int) [nTracks][rows]trackRow {
	var out [nTracks][rows]trackRow
	r := rng{p.Seed ^ uint64(pat)*0x9e3779b97f4a7c15 | 1}
	chord := p.Root + []int{0, 0, p.Scale[5] - 12, p.Scale[3] - 12}[r.n(4)]

	out[0][0] = trackRow{chord - 12, p.DroneGain, rows, p.Palette.Drone}
	if p.FifthGain > 0.01 {
		// pad patch (sine pair), not the saw drone: the fifth should
		// thicken with depth, not brighten
		out[1][0] = trackRow{chord + p.FifthOffset, p.FifthGain, rows, p.Palette.Fifth}
	}
	for _, row := range []int{0, 8} {
		if r.f() < p.PadDensity {
			deg := []int{0, 2, 4}[r.n(3)]
			out[2][row] = trackRow{chord + p.Scale[deg], 0.7, 6, p.Palette.Pad}
		}
	}
	deg := 7 + r.n(7)
	for i := 0; i < rows; i++ {
		if r.f() < p.BellDensity {
			deg += r.n(5) - 2
			deg = max(3, min(deg, 17))
			out[3][i] = trackRow{chord + 12*(deg/7) + p.Scale[deg%7] + p.BellLift, 0.55, p.LeadGate, p.Palette.Bell}
		}
	}
	if pat%2 == 0 {
		out[4][0] = trackRow{45, p.AirGain, 10, p.Palette.Air}
	}
	// tension pulse: heartbeat on 0/8, agitation fills as tension rises.
	// Guarded so tension 0 draws no rng and leaves the stream untouched.
	if p.PulseGain > 0.01 {
		note := chord - 24
		g := 0.35 + 0.45*p.PulseGain
		out[5][0] = trackRow{note, g, 1, p.Palette.Pulse}
		out[5][8] = trackRow{note, g, 1, p.Palette.Pulse}
		for _, row := range []int{4, 12} {
			if r.f() < p.PulseGain {
				out[5][row] = trackRow{note, g * 0.8, 1, p.Palette.Pulse}
			}
		}
		if p.PulseGain > 0.7 {
			for _, row := range []int{6, 14} {
				if r.f() < (p.PulseGain-0.7)*2 {
					out[5][row] = trackRow{note + 12, g * 0.5, 1, p.Palette.Pulse}
				}
			}
		}
	}
	return out
}

// ---- synth: io.Reader producing 16-bit LE stereo ----

type Synth struct {
	params        Params
	next          *Params // staged; applied at the next pattern boundary
	samplesPerRow int
	rowLeft       int
	row, pat      int
	voices        []Voice
	pats          [nTracks][rows]trackRow
	dl, dr        []float64 // delay lines
	dpos          int
	dbus          float64

	// VU meter tap: read-only from the synthesis path's view — nothing
	// here feeds back into the samples, so renders stay byte-identical.
	mlp  [3]float64 // one-pole states at the band edges
	menv [4]float64 // per-band peak envelopes (attack instant, slow decay)
	mout [4]uint64  // envelopes published as float bits for cross-goroutine reads
}

// band-edge one-pole coefficients (120/350/1200 Hz) and ~400ms decay
var mcoef = [3]float64{
	1 - math.Exp(-2*math.Pi*120/SampleRate),
	1 - math.Exp(-2*math.Pi*350/SampleRate),
	1 - math.Exp(-2*math.Pi*1200/SampleRate),
}

const mdecay = 0.99974

func (s *Synth) meter(l, r float64) {
	mono := (l + r) / 2
	for i, c := range mcoef {
		s.mlp[i] += c * (mono - s.mlp[i])
	}
	bands := [4]float64{
		s.mlp[0],            // sub
		s.mlp[1] - s.mlp[0], // low-mid
		s.mlp[2] - s.mlp[1], // mid
		mono - s.mlp[2],     // high
	}
	for i, b := range bands {
		if b < 0 {
			b = -b
		}
		s.menv[i] *= mdecay
		if b > s.menv[i] {
			s.menv[i] = b
		}
		atomic.StoreUint64(&s.mout[i], math.Float64bits(s.menv[i]))
	}
}

// Levels returns the current per-band envelopes (sub, low-mid, mid, high).
func (s *Synth) Levels() [4]float64 {
	var v [4]float64
	for i := range v {
		v[i] = math.Float64frombits(atomic.LoadUint64(&s.mout[i]))
	}
	return v
}

func NewSynth(p Params) *Synth {
	s := &Synth{voices: make([]Voice, nTracks)}
	s.apply(p)
	return s
}

// SetParameters stages a new parameter map; it takes effect at the next
// pattern boundary (the audio/set-parameters contract).
func (s *Synth) SetParameters(p Params) { s.next = &p }

func (s *Synth) apply(p Params) {
	s.params = p
	s.samplesPerRow = int(SampleRate * 60 / (p.BPM * 2)) // 2 rows per beat: ambient rows are long
	if s.dl == nil {
		s.dl = make([]float64, s.samplesPerRow*3)
		s.dr = make([]float64, s.samplesPerRow*3)
	}
}

func (s *Synth) step() {
	if s.row == 0 {
		if s.next != nil {
			s.apply(*s.next)
			s.next = nil
		}
		s.pats = buildPatterns(s.params, s.pat)
	}
	for t := 0; t < nTracks; t++ {
		v := &s.voices[t]
		if tr := s.pats[t][s.row]; tr.note != 0 {
			v.trigger(tr.patch, tr.note, tr.gateRows, tr.gain, s.params.Seed+uint64(s.pat*rows+s.row))
		} else if v.active && v.gate {
			v.gateRows--
			if v.gateRows <= 0 {
				v.gate = false
			}
		}
	}
	s.row++
	if s.row == rows {
		s.row = 0
		s.pat++
	}
}

func (s *Synth) frame() (int16, int16) {
	if s.rowLeft == 0 {
		s.step()
		s.rowLeft = s.samplesPerRow
	}
	s.rowLeft--
	var l, r float64
	s.dbus = 0
	for i := range s.voices {
		if s.voices[i].active {
			s.voices[i].run(&l, &r, &s.dbus)
		}
	}
	// cross-fed stereo delay
	wl, wr := s.dl[s.dpos], s.dr[s.dpos]
	s.dl[s.dpos] = s.dbus + wr*0.5
	s.dr[s.dpos] = s.dbus*0.7 + wl*0.5
	s.dpos = (s.dpos + 1) % len(s.dl)
	l += wl * 0.5
	r += wr * 0.5
	l, r = math.Tanh(l*0.8), math.Tanh(r*0.8)
	s.meter(l, r)
	return int16(l * 32000), int16(r * 32000)
}

func (s *Synth) Read(p []byte) (int, error) {
	n := len(p) / 4 * 4
	for i := 0; i < n; i += 4 {
		l, r := s.frame()
		binary.LittleEndian.PutUint16(p[i:], uint16(l))
		binary.LittleEndian.PutUint16(p[i+2:], uint16(r))
	}
	return n, nil
}

// ---- output ----

func WriteWAV(path string, s *Synth, seconds float64) error {
	data := make([]byte, int(seconds*SampleRate)*4)
	s.Read(data)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := func(v any) { binary.Write(f, binary.LittleEndian, v) }
	f.WriteString("RIFF")
	w(uint32(36 + len(data)))
	f.WriteString("WAVEfmt ")
	w(uint32(16))
	w(uint16(1)) // PCM
	w(uint16(2))
	w(uint32(SampleRate))
	w(uint32(SampleRate * 4))
	w(uint16(4))
	w(uint16(16))
	f.WriteString("data")
	w(uint32(len(data)))
	_, err = f.Write(data)
	return err
}
