// sointu-homage: a demo-sized generative-audio homage to vsariola/sointu
// (the 4k-intro softsynth govertone embeds). Not a reimplementation — it
// keeps the *shape*: instruments are stacks of unit opcodes run by a tiny
// per-sample VM, a tracker-style sequencer drives them, and the whole track
// is derived deterministically from a seed.
//
//	go run . -play              # live, endless, via oto
//	go run . -wav out.wav       # deterministic offline render
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math"
	"os"

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
	gate     bool
	gateRows int
	units    []ustate
	active   bool
}

func (v *Voice) trigger(patch Patch, note, gateRows int, seed uint64) {
	v.patch = patch
	v.note = note
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
			*delay += stack[sp-1] * u.p[0]
		case opOut:
			sp--
			*l += stack[sp] * u.p[0] * (1 - u.p[1])
			*r += stack[sp] * u.p[0] * u.p[1]
		}
	}
	v.active = alive
}

// ---- instruments (compact opcode tables, sointu-patch style) ----

var kick = Patch{
	{opEnv, [4]float64{0.001, 0.09, 0, 0.06}},
	{opOsc, [4]float64{0, shpSine, 38, 0.04}}, // pitch-swept sine
	{opMul, [4]float64{}},
	{opOut, [4]float64{0.9, 0.5}},
}

var bass = Patch{
	{opEnv, [4]float64{0.004, 0.25, 0.4, 0.08}},
	{opOsc, [4]float64{0, shpSaw, 0, 1}},
	{opMul, [4]float64{}},
	{opLpf, [4]float64{700, 0, 0, 0}},
	{opOut, [4]float64{0.55, 0.5}},
}

var lead = Patch{
	{opEnv, [4]float64{0.01, 0.3, 0.25, 0.12}},
	{opOsc, [4]float64{0, shpPulse, 0, 1}},
	{opMul, [4]float64{}},
	{opLpf, [4]float64{2200, 0, 0, 0}},
	{opSend, [4]float64{0.5, 0, 0, 0}},
	{opOut, [4]float64{0.28, 0.42}},
}

var hat = Patch{
	{opEnv, [4]float64{0.001, 0.035, 0, 0.02}},
	{opOsc, [4]float64{0, shpNoise, 0, 1}},
	{opMul, [4]float64{}},
	{opOut, [4]float64{0.18, 0.62}},
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

// A-minor pentatonic; chord roots A F C G — enough harmony to sound
// intentional, few enough rules to stay demo-sized.
var penta = []int{0, 3, 5, 7, 10}
var roots = []int{57, 53, 60, 55}

type trackCfg struct {
	patch    Patch
	gateRows int
	gen      func(r *rng, pat int) [rows]int
}

var tracks = []trackCfg{
	{kick, 1, func(r *rng, pat int) (p [rows]int) {
		p[0], p[8] = 33, 33
		if r.f() < 0.3 {
			p[10+2*r.n(3)] = 33
		}
		return
	}},
	{bass, 2, func(r *rng, pat int) (p [rows]int) {
		root := roots[pat%4] - 24
		for i := 0; i < rows; i += 4 {
			p[i] = root
		}
		for i := 2; i < rows; i += 4 {
			if r.f() < 0.4 {
				p[i] = root + []int{0, 7, 12}[r.n(3)]
			}
		}
		return
	}},
	{lead, 1, func(r *rng, pat int) (p [rows]int) {
		deg := r.n(len(penta)) + len(penta) // start mid-range
		density := 0.35
		if pat%4 == 3 {
			density = 0.6 // fills on the turnaround pattern
		}
		for i := 0; i < rows; i++ {
			if r.f() > density {
				continue
			}
			deg += r.n(5) - 2
			if deg < 0 {
				deg = 0
			}
			if deg >= 2*len(penta) {
				deg = 2*len(penta) - 1
			}
			p[i] = roots[pat%4] + 12*(deg/len(penta)) + penta[deg%len(penta)]
		}
		return
	}},
	{hat, 1, func(r *rng, pat int) (p [rows]int) {
		for i := 2; i < rows; i += 4 {
			p[i] = 80
		}
		for i := 0; i < rows; i += 2 {
			if p[i] == 0 && r.f() < 0.15 {
				p[i] = 80
			}
		}
		return
	}},
}

// ---- synth: io.Reader producing 16-bit LE stereo ----

type Synth struct {
	seed          uint64
	samplesPerRow int
	rowLeft       int
	row, pat      int
	voices        []Voice
	pats          [][rows]int // current pattern per track
	dl, dr        []float64   // delay lines
	dpos          int
	dbus          float64
}

func NewSynth(seed uint64, bpm float64) *Synth {
	spr := int(sampleRate * 60 / (bpm * 4)) // 4 rows per beat
	return &Synth{
		seed:          seed,
		samplesPerRow: spr,
		voices:        make([]Voice, len(tracks)),
		pats:          make([][rows]int, len(tracks)),
		dl:            make([]float64, spr*3), // dotted eighth
		dr:            make([]float64, spr*3),
	}
}

func (s *Synth) step() {
	if s.row == 0 {
		for t := range tracks {
			r := rng{s.seed ^ uint64(s.pat)*0x9e3779b97f4a7c15 ^ uint64(t)<<32 | 1}
			s.pats[t] = tracks[t].gen(&r, s.pat)
		}
	}
	for t := range tracks {
		v := &s.voices[t]
		if n := s.pats[t][s.row]; n != 0 {
			v.trigger(tracks[t].patch, n, tracks[t].gateRows, s.seed+uint64(s.pat*rows+s.row))
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
	s.dl[s.dpos] = s.dbus + wr*0.45
	s.dr[s.dpos] = s.dbus*0.7 + wl*0.45
	s.dpos = (s.dpos + 1) % len(s.dl)
	l += wl * 0.4
	r += wr * 0.4
	l, r = math.Tanh(l*0.8), math.Tanh(r*0.8)
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
	dur := flag.Float64("dur", 32, "WAV duration seconds")
	seed := flag.Uint64("seed", 1, "generative seed")
	bpm := flag.Float64("bpm", 112, "tempo")
	flag.Parse()

	s := NewSynth(*seed, *bpm)
	switch {
	case *wav != "":
		if err := writeWAV(*wav, s, *dur); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("rendered %.0fs seed=%d bpm=%.0f -> %s\n", *dur, *seed, *bpm, *wav)
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
		p := ctx.NewPlayer(io.Reader(s))
		p.Play()
		fmt.Printf("playing seed=%d bpm=%.0f — ctrl-c to stop\n", *seed, *bpm)
		select {}
	default:
		flag.Usage()
	}
}
