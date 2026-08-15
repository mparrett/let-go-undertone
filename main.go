// undertone: a tiny generative-ambience engine (the glplat audio spike's
// engine seed). Architecturally a demo-sized homage to vsariola/sointu —
// no sointu code, just its shape: instruments are stacks of unit opcodes
// run by a tiny per-sample VM, a tracker-style sequencer drives them, and
// the whole track is derived deterministically from a seed.
//
// Register: roguelike ambience. Parameters derive from (seed, depth) —
// deeper is darker, slower, thicker — and SetParameters applies a new map
// at the next pattern boundary, the same transactional semantics the
// glplat audio surface sketch proposes for audio/set-parameters.
//
//	go run . -play                       # live, endless, via oto
//	go run . -wav out.wav -depth 4       # deterministic offline render
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"sync/atomic"

	"github.com/ebitengine/oto/v3"
)

const sampleRate = 44100

// ---- unit VM ----

const (
	opEnv  = iota // p: attack s, decay s, sustain lvl, release s   -> push env
	opOsc         // p: transpose semis, shape, sweep semis, sweep decay s -> push sample
	opMul         // pop a, b -> push a*b
	opLpf         // p: cutoff Hz; pop x -> push lowpassed x
	opSend        // p: amount; peek top, add top*amount to delay bus
	opOut         // p: gain, pan 0..1; pop x -> accumulate to L/R
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
				s.env += 1 / (u.p[0]*sampleRate + 1)
				if s.env >= 1 {
					s.env, s.stage = 1, 1
				}
			case 1:
				s.env += (u.p[2] - s.env) / (u.p[1] * sampleRate)
				if s.env-u.p[2] < 1e-3 {
					s.stage = 2
				}
			case 3:
				s.env -= s.env / (u.p[3] * sampleRate)
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
			s.sweep -= s.sweep / (u.p[3]*sampleRate + 1)
			freq := 440 * math.Pow(2, (semis-69)/12)
			s.phase += freq / sampleRate
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
			c := 1 - math.Exp(-2*math.Pi*u.p[0]/sampleRate)
			s.lp += c * (stack[sp-1] - s.lp)
			stack[sp-1] = s.lp
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
	Seed        uint64
}

var scalesByDarkness = [][]int{
	{0, 2, 3, 5, 7, 9, 10}, // dorian
	{0, 2, 3, 5, 7, 8, 10}, // aeolian
	{0, 1, 3, 5, 7, 8, 10}, // phrygian
}

func DeriveParams(seed uint64, depth int) Params {
	r := rng{seed ^ uint64(depth)*0x9e3779b97f4a7c15 | 1}
	d := float64(depth)
	darkness := depth / 2
	if darkness >= len(scalesByDarkness) {
		darkness = len(scalesByDarkness) - 1
	}
	return Params{
		BPM:         math.Max(46, 66-3*d) + float64(r.n(5)),
		Root:        57 - depth + []int{-2, 0, 0, 3}[r.n(4)],
		Scale:       scalesByDarkness[darkness],
		DroneGain:   math.Min(0.9, 0.55+0.06*d),
		FifthGain:   math.Min(0.7, 0.2*d),
		PadDensity:  0.6 - 0.05*d,
		BellDensity: math.Max(0.04, 0.16-0.02*d),
		AirGain:     math.Min(0.6, 0.25+0.05*d),
		Seed:        seed,
	}
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
const nTracks = 5 // drone, fifth, pad, bell, air

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

	out[0][0] = trackRow{chord - 12, p.DroneGain, rows, drone}
	if p.FifthGain > 0.01 {
		// pad patch (sine pair), not the saw drone: the fifth should
		// thicken with depth, not brighten
		out[1][0] = trackRow{chord - 5, p.FifthGain, rows, pad}
	}
	for _, row := range []int{0, 8} {
		if r.f() < p.PadDensity {
			deg := []int{0, 2, 4}[r.n(3)]
			out[2][row] = trackRow{chord + p.Scale[deg], 0.7, 6, pad}
		}
	}
	deg := 7 + r.n(7)
	for i := 0; i < rows; i++ {
		if r.f() < p.BellDensity {
			deg += r.n(5) - 2
			deg = max(3, min(deg, 17))
			out[3][i] = trackRow{chord + 12*(deg/7) + p.Scale[deg%7], 0.55, 1, bell}
		}
	}
	if pat%2 == 0 {
		out[4][0] = trackRow{45, p.AirGain, 10, air}
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
	1 - math.Exp(-2*math.Pi*120/sampleRate),
	1 - math.Exp(-2*math.Pi*350/sampleRate),
	1 - math.Exp(-2*math.Pi*1200/sampleRate),
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
	s.samplesPerRow = int(sampleRate * 60 / (p.BPM * 2)) // 2 rows per beat: ambient rows are long
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

func writeWAV(path string, s *Synth, seconds float64) error {
	data := make([]byte, int(seconds*sampleRate)*4)
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
	w(uint32(sampleRate))
	w(uint32(sampleRate * 4))
	w(uint16(4))
	w(uint16(16))
	f.WriteString("data")
	w(uint32(len(data)))
	_, err = f.Write(data)
	return err
}

func main() {
	play := flag.Bool("play", false, "play live (endless) via oto")
	wav := flag.String("wav", "", "render to WAV file")
	dur := flag.Float64("dur", 48, "WAV duration seconds")
	seed := flag.Uint64("seed", 1, "level seed")
	depth := flag.Int("depth", 0, "dungeon depth: deeper = darker, slower, thicker")
	vu := flag.String("vu", "bars", "meter style: bars | compact (one cell per band) | tall (two cells, 2x range)")
	flag.Parse()

	p := DeriveParams(*seed, *depth)
	s := NewSynth(p)
	desc := fmt.Sprintf("seed=%d depth=%d bpm=%.0f root=%d", *seed, *depth, p.BPM, p.Root)
	switch {
	case *wav != "":
		if err := writeWAV(*wav, s, *dur); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("rendered %.0fs %s -> %s\n", *dur, desc, *wav)
	case *play:
		ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
			SampleRate:   sampleRate,
			ChannelCount: 2,
			Format:       oto.FormatSignedInt16LE,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		<-ready
		pl := ctx.NewPlayer(io.Reader(s))
		pl.Play()
		runMeterUI(s, desc, *vu)
	default:
		flag.Usage()
	}
}
